/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"log"
	"flag"
	"time"
	"strings"
	"strconv"
	"net/http"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"swifty/common"
	"swifty/apis"
	"swifty/s3/mgo"
)

const (
	DBName		= "swifty-s3"
	ColS3Stats	= "S3Stats"
	ColS3StatsArch	= "S3StatsA"
)

type YAMLConfSA struct {
	Check		string			`yaml:"check"`
	Period		string			`yaml:"period"`
}

type YAMLConf struct {
	GateDB		string			`yaml:"gate-db"`
	gateDB		*xh.XCreds
	Admd		string			`yaml:"admd"`
	admd		*xh.XCreds

	SA		*YAMLConfSA		`yaml:"starch,omitempty"`
}

var conf YAMLConf

var created map[string]*time.Time

func nextPeriod(since *time.Time, period string) time.Time {
	/* Common sane types */
	switch period {
	case "hourly":
		return since.Add(time.Hour)
	case "daily":
		return since.AddDate(0, 0, 1)
	case "weekly":
		return since.AddDate(0, 0, 7)
	case "monthly":
		return since.AddDate(0, 1, 0)
	}

	/* For debugging mostly */
	var mult time.Duration
	var dur string
	if strings.HasSuffix(period, "s") {
		dur = strings.TrimSuffix(period, "s")
		mult = time.Second
	} else if strings.HasSuffix(period, "m") {
		dur = strings.TrimSuffix(period, "s")
		mult = time.Minute
	}

	if mult != 0 {
		i, err := strconv.Atoi(dur)
		if err != nil {
			goto out
		}
		return since.Add(time.Duration(i) * mult)
	}
out:
	panic("Bad period value: " + period)
}

func s3NamespaceId(tenant string) string {
	/* See gate's S3Namespace and s3's acct.NamespaceID */
	s3ns := xh.Cookify(tenant + "/default")
	nsid := xh.Sha256sum([]byte(s3ns))
	return nsid
}

func timePassed(since *time.Time, now time.Time, period string) bool {
	if now.Before(*since) {
		return false
	}

	return nextPeriod(since, period).Before(now)
}

func getByUserCreationTime(nsid string) (*time.Time, error) {
	if created == nil {
		cln := swyapi.MakeClient(conf.admd.User, conf.admd.Pass, conf.admd.Host, conf.admd.Port)
		cln.Admd("", conf.admd.Port)
		cln.ToAdmd(true)
		if conf.admd.Domn == "direct" {
			cln.NoTLS()
			cln.Direct()
		}

		err := cln.Login()
		if err != nil {
			log.Printf("  cannot login to admd: %s\n", err.Error())
			return nil, err
		}

		var ifs []*swyapi.UserInfo
		err = cln.Req1("GET", "users", http.StatusOK, nil, &ifs)
		if err != nil {
			log.Printf("  error getting users: %s\n", err.Error())
			return nil, err
		}

		created = make(map[string]*time.Time)
		for _, ui := range(ifs) {
			if ui.Created == "" {
				log.Printf("\t\tERR: created time missing for %s\n", ui.UId)
				continue
			}
			ct, err := time.Parse(time.RFC1123Z, ui.Created)
			if err != nil {
				log.Printf("\t\tERR: created time %s parse error %s\n", ui.Created, err.Error())
				continue
			}

			nsid := s3NamespaceId(ui.UId)
			created[nsid] = &ct
		}
	}

	return created[nsid], nil
}

func getLastStats(arch *mgo.Collection, nsid string) *time.Time {
	var ast s3mgo.AcctStats
	var last *time.Time

	err := arch.Find(bson.M{"nsid": nsid}).Sort("-till").Limit(1).One(&ast)
	if err == nil {
		last = ast.Till
	} else if err == mgo.ErrNotFound {
		log.Printf("\t\tNo archive - requesting creation time\n")
		ct, err := getByUserCreationTime(nsid)
		if err != nil {
			return nil
		}

		last = ct
		log.Printf("\t\tCreated %s\n", last)
	} else {
		log.Printf("\t\tERR: arch query error: %s", err.Error())
		return nil
	}

	return last
}

func doArchPass(now time.Time, s *mgo.Session) {
	defer s.Close()
	defer func() { created = nil } ()

	curr := s.DB(DBName).C(ColS3Stats)
	arch := s.DB(DBName).C(ColS3StatsArch)

	var st s3mgo.AcctStats

	iter := curr.Find(nil).Iter()
	for iter.Next(&st) {
		log.Printf("\tFound stats for %s, checking archive\n", st.NamespaceID)
		last := getLastStats(arch, st.NamespaceID)
		if last == nil {
			continue
		}

		log.Printf("\tLast archive at %s\n", last)
		if !timePassed(last, now, conf.SA.Period) {
			log.Printf("\t\tFresh archive, skipping\n")
			continue
		}

		log.Printf("\tArchive %s stats (%s)\n", st.NamespaceID, now)
		oid := st.ObjID
		st.ObjID = bson.NewObjectId()
		st.Till = &now
		st.Lim = nil
		err := arch.Insert(&st)
		if err != nil {
			log.Printf("\t\tERR: error archiving: %s\n", err.Error())
		}

		upd := bson.M{"out-bytes-tot-off": st.OutBytes + st.OutBytesWeb}
		err = curr.Update(bson.M{"_id": oid}, bson.M{"$set": upd})
		if err != nil {
			log.Printf("\t\tERR: error updating orig stats: %s\n", err.Error())
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

	conf.admd = xh.ParseXCreds(conf.Admd)

	info := mgo.DialInfo{
		Addrs:		[]string{conf.gateDB.Addr()},
		Database:	DBName,
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

				slp := nextPeriod(&now, conf.SA.Check).Sub(now)
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
