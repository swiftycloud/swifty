/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
        "os"
	"log"
	"flag"
	"time"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"swifty/common"
	"swifty/apis"
	"swifty/gate/mgo"
	"swifty/scrapers"
)

const (
	LogsCleanPeriod = 30 * 60 * time.Second
)

type TenStats struct {
	gmgo.TenStatValues			`bson:",inline"`
	Tenant			string		`bson:"tenant"`
	Till			*time.Time	`bson:"till,omitempty"`
}

type YAMLConfSA struct {
	Check		string			`yaml:"check"`
	Period		string			`yaml:"period"`
}

type YAMLConfLogs struct {
	Keep		int			`yaml:"keep"`
}

type YAMLConf struct {
	GateDB		string			`yaml:"gate-db"`
	gateDB		*xh.XCreds
	Admd		string			`yaml:"admd"`
	admd		*xh.XCreds

	SA		*YAMLConfSA		`yaml:"starch,omitempty"`
	Logs		*YAMLConfLogs		`yaml:"logs,omitempty"`
}

var conf YAMLConf

var uis map[string]*swyapi.UserInfo

func timePassed(since *time.Time, now time.Time, period string) bool {
	if now.Before(*since) {
		return false
	}

	return dbscr.NextPeriod(since, period).Before(now)
}

func getUserInfo(ten string) (*swyapi.UserInfo, error) {
	if uis == nil {
		ifs, err := dbscr.GetTenants(conf.admd)
		if err != nil {
			log.Printf("Cannot get tenants: %s\n", err.Error())
			return nil, err
		}

		uis = make(map[string]*swyapi.UserInfo)
		for _, ui := range(ifs) {
			uis[ui.UId] = ui
		}
	}

	return uis[ten], nil
}

func getLastStats(arch *mgo.Collection, ten string) *time.Time {
	var ast TenStats
	var last *time.Time

	err := arch.Find(bson.M{"tenant": ten}).Sort("-till").Limit(1).One(&ast)
	if err == nil {
		last = ast.Till
	} else if err == mgo.ErrNotFound {
		log.Printf("\t\tNo archive - requesting creation time\n")
		ui, err := getUserInfo(ten)
		if err != nil {
			return nil
		}
		if ui == nil {
			log.Printf("\t\tERR: user unregistered\n")
			return nil
		}
		if ui.Created == "" {
			log.Printf("\t\tERR: created time missing\n")
			return nil
		}
		ct, err := time.Parse(time.RFC1123Z, ui.Created)
		if err != nil {
			log.Printf("\t\tERR: created time %s parse error %s\n",
				ui.Created, err.Error())
			return nil
		}
		last = &ct
		log.Printf("\t\tCreated %s\n", last)
	} else {
		log.Printf("\t\tERR: arch query error: %s", err.Error())
		return nil
	}

	return last
}

func doArchPass(now time.Time, s *mgo.Session) {
	defer s.Close()
	defer func() { uis = nil } ()

	curr := s.DB(gmgo.DBStateDB).C(gmgo.DBColTenStats)
	arch := s.DB(gmgo.DBStateDB).C(gmgo.DBColTenStatsA)

	var st TenStats
	iter := curr.Find(nil).Iter()
	for iter.Next(&st) {
		log.Printf("\tFound stats for %s, checking archive\n", st.Tenant)
		last := getLastStats(arch, st.Tenant)
		if last == nil {
			continue
		}

		log.Printf("\tLast archive at %s\n", last)
		if !timePassed(last, now, conf.SA.Period) {
			log.Printf("\t\tFresh archive, skipping\n")
			continue
		}

		log.Printf("\tArchive %s stats (%s)\n", st.Tenant, now)
		st.Till = &now
		err := arch.Insert(&st)
		if err != nil {
			log.Printf("\t\tERR: error archiving: %s\n", err.Error())
		}
	}

	err := iter.Close()
	if err != nil {
		log.Printf("ERR:  error requesting stats: %s", err.Error())
	}
}

func main() {
	var config_path string

	flag.StringVar(&config_path, "conf", "", "path to a config file")
	flag.Parse()

	if config_path == "" {
		log.Printf("Specify config path\n")
		return
	}

	err := xh.ReadYamlConfig(config_path, &conf)
	if err != nil {
		log.Printf("Bad config: %s\n", err.Error())
		return
	}

	conf.gateDB = xh.ParseXCreds(conf.GateDB)
	conf.gateDB.Resolve()
	p := os.Getenv("SCRDBPASS")
	if p != "" {
		conf.gateDB.Pass = p
	}

	conf.admd = xh.ParseXCreds(conf.Admd)
	p = os.Getenv("SCRADPASS")
	if p != "" {
		conf.admd.Pass = p
	}

	info := mgo.DialInfo{
		Addrs:		[]string{conf.gateDB.Addr()},
		Database:	gmgo.DBStateDB,
		Timeout:	60 * time.Second,
		Username:	conf.gateDB.User,
		Password:	conf.gateDB.Pass,
	}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		log.Printf("dbConnect: Can't dial (%s)\n", err.Error())
		return
	}

	lock := make(chan chan bool)

	if conf.Logs != nil {
		log.Printf("Start logs cleaner (%d days old)\n", conf.Logs.Keep)
		go func() {
			for {
				done := <-lock

				log.Printf("Cleaner logs ...\n", conf.Logs.Keep)
				s := session.Copy()
				logs := s.DB(gmgo.DBStateDB).C(gmgo.DBColLogs)
				dur := time.Now().AddDate(0, 0, -conf.Logs.Keep)
				logs.RemoveAll(bson.M{"ts": bson.M{"$lt": dur }})
				log.Printf("`- ... cleaned < %s\n", dur.String())
				s.Close()

				done <-true

				time.Sleep(LogsCleanPeriod)
			}
		}()
	}

	if conf.SA != nil {
		log.Printf("Start stats archiver (%s)\n", conf.SA.Period)
		go func() {
			for {
				done := <-lock

				now := time.Now()
				log.Printf("%s: Check stats\n", now.Format("Mon Jan 2 15:04:05 2006"))
				doArchPass(now, session.Copy())
				log.Printf("-----------8<--------------------------------\n")

				done <-true

				slp := dbscr.NextPeriod(&now, conf.SA.Check).Sub(now)
				<-time.After(slp)
			}
		}()
	}

	done := make(chan bool)
	for {
		lock <-done
		<-done
	}
}
