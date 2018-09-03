package main

import (
	"fmt"
	"flag"
	"time"
	"strings"
	"strconv"
	"net/http"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"../common"
	"../apis"
	"../gate/mgo"
)

const (
	DBName		= "swifty"
	ColTenStats	= "TenantStats"
	ColTenStatsA	= "TenantStatsArch"
	ColLogs		= "Logs"
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
	gateDB		*swy.XCreds
	Admd		string			`yaml:"admd"`
	admd		*swy.XCreds

	SA		YAMLConfSA		`yaml:"starch"`
	Logs		YAMLConfLogs		`yaml:"logs"`
}

var conf YAMLConf

var uis map[string]*swyapi.UserInfo

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

func timePassed(since *time.Time, now time.Time, period string) bool {
	if now.Before(*since) {
		return false
	}

	return nextPeriod(since, period).Before(now)
}

func getUserInfo(ten string) (*swyapi.UserInfo, error) {
	if uis == nil {
		cln := swyapi.MakeClient(conf.admd.User, conf.admd.Pass, conf.admd.Host, conf.admd.Port)
		cln.Admd("", conf.admd.Port)
		cln.ToAdmd(true)
		if conf.admd.Domn == "direct" {
			cln.NoTLS()
			cln.Direct()
		}

		err := cln.Login()
		if err != nil {
			fmt.Printf("  cannot login to admd: %s\n", err.Error())
			return nil, err
		}

		var ifs []*swyapi.UserInfo
		err = cln.Req1("GET", "users", http.StatusOK, nil, &ifs)
		if err != nil {
			fmt.Printf("  error getting users: %s\n", err.Error())
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
		fmt.Printf("\t\tNo archive - requesting creation time\n")
		ui, err := getUserInfo(ten)
		if err != nil {
			return nil
		}
		if ui == nil {
			fmt.Printf("\t\tERR: user unregistered\n")
			return nil
		}
		if ui.Created == "" {
			fmt.Printf("\t\tERR: created time missing\n")
			return nil
		}
		ct, err := time.Parse(time.RFC1123Z, ui.Created)
		if err != nil {
			fmt.Printf("\t\tERR: created time %s parse error %s\n",
				ui.Created, err.Error())
			return nil
		}
		fmt.Printf("\t\tCreated %s\n", last)
		last = &ct
	} else {
		fmt.Printf("\t\tERR: arch query error: %s", err.Error())
		return nil
	}

	return last
}

func doArchPass(now time.Time, s *mgo.Session) {
	defer s.Close()
	defer func() { uis = nil } ()

	curr := s.DB(DBName).C(ColTenStats)
	arch := s.DB(DBName).C(ColTenStatsA)

	var st TenStats
	iter := curr.Find(nil).Iter()
	for iter.Next(&st) {
		fmt.Printf("\tFound stats for %s, checking archive\n", st.Tenant)
		last := getLastStats(arch, st.Tenant)
		if last == nil {
			continue
		}

		fmt.Printf("\tLast archive at %s\n", last)
		if !timePassed(last, now, conf.SA.Period) {
			fmt.Printf("\t\tFresh archive, skipping\n")
			continue
		}

		fmt.Printf("\tArchive %s stats (%s)\n", st.Tenant, now)
		st.Till = &now
		err := arch.Insert(&st)
		if err != nil {
			fmt.Printf("\t\tERR: error archiving: %s\n", err.Error())
		}
	}

	err := iter.Close()
	if err != nil {
		fmt.Printf("ERR:  error requesting stats: %s", err.Error())
	}
}

func main() {
	var config_path string

	flag.StringVar(&config_path, "conf", "", "path to a config file")
	flag.Parse()

	if config_path == "" {
		fmt.Printf("Specify config path\n")
		return
	}

	err := swy.ReadYamlConfig(config_path, &conf)
	if err != nil {
		fmt.Printf("Bad config: %s\n", err.Error())
		return
	}

	conf.gateDB = swy.ParseXCreds(conf.GateDB)
	conf.gateDB.Resolve()

	conf.admd = swy.ParseXCreds(conf.Admd)
	conf.admd.Resolve()

	info := mgo.DialInfo{
		Addrs:		[]string{conf.gateDB.Addr()},
		Database:	DBName,
		Timeout:	60 * time.Second,
		Username:	conf.gateDB.User,
		Password:	conf.gateDB.Pass,
	}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		fmt.Printf("dbConnect: Can't dial (%s)\n", err.Error())
		return
	}

	if conf.Logs.Keep > 0 {
		fmt.Printf("Start logs cleaner (%d days old)", conf.Logs.Keep)
		go func() {
			for {
				time.Sleep(LogsCleanPeriod)

				s := session.Copy()
				logs := s.DB(DBName).C(ColLogs)
				dur := time.Now().AddDate(0, 0, -conf.Logs.Keep)
				logs.RemoveAll(bson.M{"ts": bson.M{"$lt": dur }})
				fmt.Printf("Cleaned logs < %s", dur.String())
				s.Close()
			}
		}()
	}

	for {
		now := time.Now()
		fmt.Printf("%s: Check stats\n", now.Format("Mon Jan 2 15:04:05 2006"))

		doArchPass(now, session.Copy())

		fmt.Printf("-----------8<--------------------------------\n")

		slp := nextPeriod(&now, conf.SA.Check).Sub(now)
		<-time.After(slp)
	}
}
