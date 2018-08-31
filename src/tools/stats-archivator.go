package main

import (
	"time"
	"fmt"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"./src/common"
	"./src/apis"
)

const (
	ColTenStats	= "TenantStats"
	ColTenStatsA	= "TenantStatsArch"
)

type YAMLConf struct {
	GateDB		string			`yaml:"gate-db"`
	gateDB		*swy.XCreds
	Admd		string			`yaml:"admd"`
	admd		*swy.XCreds

	Check		int			`yaml:"check"`
	Period		int			`yaml:"period"`
}

var conf YAMLConf

var uis map[string]*swyapi.UserInfo

func getUserInfo(ten string) (*swyapi.UserInfo, error) {
	if uis == nil {
		cln := swyapi.MakeClient(conf.admd.User, conf.admd.Pass, conf.admd.Host, conf.admd.Port)
		cln.Admd("", conf.admd.Port)
		cln.ToAdmd()
		err := cln.Login()
		if err != nil {
			fmt.Printf("  cannot login to admd: %s", err.Error())
			return nil, err
		}

		var ifs []*swyapi.UserInfo
		err = cln.Req1("GET", "users", http.StatusOK, nil, &ifs)
		if err != nil {
			fmt.Printf("  error getting users: %s", err.Error())
			return nil, err
		}

		uis = make(map[string]*swyapi.UserInfo)
		for _, ui := range(ifs) {
			uis[ui.UId] = ui
		}
	}

	return uis[ten], nil
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
		Database:	"swifty",
		Timeout:	60 * time.Second,
		Username:	conf.gateDB.User,
		Password:	conf.gateDB.Pass,
	}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		fmt.Printf("dbConnect: Can't dial (%s)\n", err.Error())
		return
	}

	check := parseDuration(c.Check)
	period := parseDuration(c.Period)

	for {
		s := session.Copy()
		curr := s.DB("swifty").C(ColTenStats)
		arch := s.DB("swifty").C(ColTenStatsA)

		now := time.Now()
		fmt.Printf("%s: Check stats\n", now)

		var st TenStats
		iter := curr.Find(nil).Iter()
		for iter.Next(&st) {
			var ast TenStats
			var last *time.Time
			err = arch.Find(bson.M{"tenant": st.Tentant}).Sort("-till").Limit(1).One(&ast)
			if err == nil {
				last = ast.Till
			} else if err == mgo.ErrNotFound {
				fmt.Printf("  %s has no arch stats, requesting creation time", st.Tentant)
				ui, err := getUserInfo(st.Tenant)
				if err != nil {
					continue
				}
				if ui == nil {
					fmt.Printf("  %s user unregistered\n", st.Tenant)
					continue
				}
				if ui.Created == "" {
					fmt.Printf("  %s created time missing\n", st.Tenant)
					continue
				}
				ct, err := time.Parse(time.RFC1123Z, ui.Created)
				if err != nil {
					fmt.Printf("  %s created time %s parse error %s\n",
						st.Tenant, ui.Created, err.Error())
					continue
				}
				last = &st
			} else {
				fmt.Printf("  %s arch query error: %s", st.Tentant, err.Error())
				continue
			}

			if last.Till.Add(period).After(now) {
				continue
			}

			fmt.Printf("-> archive %s stats\n", st.Tentant)
			ast.Till = now
			err = arch.Insert(ast)
			if err != nil {
				fmt.Printf(" error archiving: %s\n", err.Error())
			}
		}

		err = iter.Close()
		if err != nil {
			fmt.Printf("  error requesting stats: %s", err.Error())
		}

		uis = nil

		<-time.After(check)
	}

//	c.Insert(bson.M{"id": 1, "ts": time.Now()})
//	c.Insert(bson.M{"id": 2, "ts": time.Now().Add(2 * time.Second)})
}
