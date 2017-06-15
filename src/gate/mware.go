package main

import (
	"encoding/json"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/michaelklishin/rabbit-hole"

	"gopkg.in/mgo.v2/bson"

	"net/http"
	"strings"
	"fmt"

	"../apis/apps"
	"../common"
)

type MwareDesc struct {
	// These objects are kept in Mongo, which requires the below
	// field to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`

	Project		string		`bson:"project"`	// Project name
	MwareID		string		`bson:"mwareid"`	// Middleware ID
	MwareType	string		`bson:"mwaretype"`	// Middleware type
	Client		string		`bson:"client"`		// Middleware client
	Pass		string		`bson:"pass"`		// Client password
	Counter		int		`bson:"counter"`	// Middleware instance counter
	State		int		`bson:"state"`		// Mware state

	JSettings	string		`bson:"jsettings"`	// Middleware settings in json format
}

type MwareOpsIface interface {
	Init(conf *YAMLConf, mwd *MwareDesc, mware *swyapi.MwareItem) ([]byte, error)
	Fini(conf *YAMLConf, mwd *MwareDesc) (error)
	Event(conf *YAMLConf, source *FnEventDesc, mwd *MwareDesc, on bool) (error)
	GetEnv(conf *YAMLConf, mwd *MwareDesc) ([]string)
}

func mkEnv(mwd *MwareDesc, envName, value string) string {
	return "MWARE_" + strings.ToUpper(mwd.MwareID) + "_" + envName + "=" + value
}

func genEnvs(mwd *MwareDesc, mwaddr string) ([]string) {
	return []string{
		mkEnv(mwd, "ADDR", mwaddr),
		mkEnv(mwd, "USER", mwd.Client),
		mkEnv(mwd, "PASS", mwd.Pass),
	}
}

type DBSettings struct {
	DBName		string			`json:"dbname"`
}

type MQSettings struct {
	Vhost		string			`json:"vhost"`
}

type MariaDBSettings struct {
}

type RabbitMQSettings struct {
}

func stripName(name string) string {
	if len(name) > 64 {
		return name[:64]
	}
	return name
}

func mwareGenerateClient(mwd *MwareDesc) (error) {
	var err error

	mwd.Client, err = swy.GenRandId(32)
	if err != nil {
		return err
	}

	mwd.Pass, err = swy.GenRandId(64)
	if err != nil {
		return err
	}

	return nil
}

func mariaConn(conf *YAMLConf) (*sql.DB, error) {
	return sql.Open("mysql",
			fmt.Sprintf("%s:%s@tcp(%s)/?charset=utf8",
				conf.Mware.SQL.User,
				conf.Mware.SQL.Pass,
				conf.Mware.SQL.Addr))
}

func mariaReq(db *sql.DB, req string) error {
	_, err := db.Exec(req)
	if err != nil {
		return fmt.Errorf("DB: cannot execure %s req: %s", req, err.Error())
	}

	return nil
}

// SELECT User FROM mysql.user;
// SHOW DATABASES;
// DROP USER IF EXISTS '8257fbff9618952fbd2b83b4794eb694'@'%';
// DROP DATABASE IF EXISTS 8257fbff9618952fbd2b83b4794eb694;

func (m MariaDBSettings) Init(conf *YAMLConf, mwd *MwareDesc, mware *swyapi.MwareItem) ([]byte, error) {
	dbs := DBSettings{ }

	err := mwareGenerateClient(mwd)
	if err != nil {
		return nil, err
	}

	dbs.DBName = mwd.Client

	db, err := mariaConn(conf)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	err = mariaReq(db, "CREATE USER '" + mwd.Client + "'@'%' IDENTIFIED BY '" + mwd.Pass + "';")
	if err != nil {
		return nil, err
	}

	err = mariaReq(db, "CREATE DATABASE " + dbs.DBName + " CHARACTER SET utf8 COLLATE utf8_general_ci;")
	if err != nil {
		return nil, err
	}

	err = mariaReq(db, "GRANT ALL PRIVILEGES ON " + dbs.DBName + ".* TO '" + mwd.Client + "'@'%' IDENTIFIED BY '" + mwd.Pass + "';")
	if err != nil {
		return nil, err
	}

	return json.Marshal(&dbs)
}

func (m MariaDBSettings) Fini(conf *YAMLConf, mwd *MwareDesc) error {
	var dbs DBSettings

	err := json.Unmarshal([]byte(mwd.JSettings), &dbs)
	if err != nil {
		return fmt.Errorf("MariaDBSettings.Fini: Can't unmarshal data %s: %s",
					mwd.JSettings, err.Error())
	}

	db, err := mariaConn(conf)
	if err != nil {
		return err
	}
	defer db.Close()

	err = mariaReq(db, "DROP USER IF EXISTS '" + mwd.Client + "'@'%';")
	if err != nil {
		log.Errorf("maria: can't drop user %s: %s", mwd.Client, err.Error())
	}

	err = mariaReq(db, "DROP DATABASE IF EXISTS " + dbs.DBName + ";")
	if err != nil {
		log.Errorf("maria: can't drop database %s: %s", dbs.DBName, err.Error())
	}

	return nil
}

func (m MariaDBSettings) Event(conf *YAMLConf, source *FnEventDesc, mwd *MwareDesc, on bool) (error) {
	return fmt.Errorf("No events for mariadb")
}

func (m MariaDBSettings) GetEnv(conf *YAMLConf, mwd *MwareDesc) ([]string) {
	var dbs DBSettings
	var envs []string
	var err error

	err = json.Unmarshal([]byte(mwd.JSettings), &dbs)
	if err == nil {
		envs = append(genEnvs(mwd, conf.Mware.SQL.Addr), mkEnv(mwd, "DBNAME", dbs.DBName))
	} else {
		log.Fatal("rabbit: Can't unmarshal DB entry %s", mwd.JSettings)
	}

	return envs
}

func rabbitConn(conf *YAMLConf) (*rabbithole.Client, error) {
	addr := strings.Split(conf.Mware.MQ.Addr, ":")[0] + ":" + conf.Mware.MQ.AdminPort
	return rabbithole.NewClient("http://" + addr, conf.Mware.MQ.User, conf.Mware.MQ.Pass)
}

func rabbitErr(resp *http.Response, err error) error {
	if err != nil {
		return err
	} else if resp.StatusCode != http.StatusCreated &&
			resp.StatusCode != http.StatusNoContent &&
			resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	} else {
		return nil
	}
}

func (m RabbitMQSettings) Init(conf *YAMLConf, mwd *MwareDesc, mware *swyapi.MwareItem) ([]byte, error) {
	rmq := MQSettings{ }

	err := mwareGenerateClient(mwd)
	if err != nil {
		return nil, err
	}

	rmq.Vhost = mwd.Client

	rmqc, err := rabbitConn(conf)
	if err != nil {
		return nil, err
	}

	err = rabbitErr(rmqc.PutUser(mwd.Client, rabbithole.UserSettings{Password: mwd.Pass}))
	if err != nil {
		return nil, fmt.Errorf("Can't create user %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.PutVhost(rmq.Vhost, rabbithole.VhostSettings{Tracing: false}))
	if err != nil {
		return nil, fmt.Errorf("Can't create vhost %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.UpdatePermissionsIn(rmq.Vhost, mwd.Client,
			rabbithole.Permissions{Configure: ".*", Write: ".*", Read: ".*"}))
	if err != nil {
		return nil, fmt.Errorf("Can't set permissions %s: %s", mwd.Client, err.Error())
	}

	/* Add permissions for us as well, just in case event listening is required */
	err = rabbitErr(rmqc.UpdatePermissionsIn(rmq.Vhost, conf.Mware.MQ.User,
			rabbithole.Permissions{Configure: ".*", Write: ".*", Read: ".*"}))
	if err != nil {
		return nil, fmt.Errorf("Can't set permissions %s: %s", mwd.Client, err.Error())
	}

	return json.Marshal(&rmq)
}

func (m RabbitMQSettings) Fini(conf *YAMLConf, mwd *MwareDesc) error {
	var rmq MQSettings

	err := json.Unmarshal([]byte(mwd.JSettings), &rmq)
	if err != nil {
		return fmt.Errorf("RabbitMQSettings.Fini: Can't unmarshal data %s: %s",
					mwd.JSettings, err.Error())
	}

	rmqc, err := rabbitConn(conf)
	if err != nil {
		return err
	}

	err = rabbitErr(rmqc.DeleteVhost(rmq.Vhost))
	if err != nil {
		log.Errorf("rabbit: can't delete vhost %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.DeleteUser(mwd.Client))
	if err != nil {
		log.Errorf("rabbit: can't delete user %s: %s", mwd.Client, err.Error())
	}

	return nil
}

func (m RabbitMQSettings) Event(conf *YAMLConf, source *FnEventDesc, mwd *MwareDesc, on bool) (error) {
	var rmq MQSettings

	_ = json.Unmarshal([]byte(mwd.JSettings), &rmq)
	if on {
		return mqStartListener(conf, rmq.Vhost, source.MQueue)
	} else {
		mqStopListener(rmq.Vhost, source.MQueue)
		return nil
	}
}

func (m RabbitMQSettings) GetEnv(conf *YAMLConf, mwd *MwareDesc) ([]string) {
	var rmq MQSettings
	var envs []string
	var err error

	err = json.Unmarshal([]byte(mwd.JSettings), &rmq)
	if err == nil {
		envs = append(genEnvs(mwd, conf.Mware.MQ.Addr), mkEnv(mwd, "VHOST", rmq.Vhost))
	} else {
		log.Fatal("rabbit: Can't unmarshal DB entry %s", mwd.JSettings)
	}

	return envs
}

type MwareSettings struct {
	MariaDB		MariaDBSettings		`json:"mariadb"`
	RabbitMQ	RabbitMQSettings	`json:"rabbitmq"`
}

var settings MwareSettings

var mwareHandlers = map[string]MwareOpsIface {
	"sql":	settings.MariaDB,
	"mq":	settings.RabbitMQ,
}

func mwareGetEnv(conf *YAMLConf, project, mwid string) ([]string, error) {
	// No mware lock needed here since it's a pure
	// read with mware counter increased already so
	// can't disappear
	item, err := dbMwareGetItem(project, mwid)
	if err != nil {
		return nil, fmt.Errorf("Can't fetch settings for mware %s", mwid)
	}

	handler, ok := mwareHandlers[item.MwareType]
	if !ok {
		return nil, fmt.Errorf("No handler for %s mware", mwid)
	}

	return handler.GetEnv(conf, &item), nil
}

func mwareGetFnEnv(conf *YAMLConf, fn *FunctionDesc) ([]string, error) {
	var envs []string

	for _, mwId := range fn.Mware {
		env, err := mwareGetEnv(conf, fn.Project, mwId)
		if err != nil {
			return nil, err
		}

		envs = append(envs, env...)
	}

	return envs, nil
}

func mwareRemove(conf *YAMLConf, project string, mwIds []string) error {
	var ret error = nil

	for _, mwId := range mwIds {
		log.Debugf("remove wmare %s", mwId)
		if dbMwareLock(project, mwId) != nil {
			log.Errorf("Can't lock mware %s", mwId)
			continue
		}

		removed, item, _ := dbMwareDecRefLocked(project, mwId)
		if removed == false {
			dbMwareUnlock(project, mwId)
			continue
		}

		handler, ok := mwareHandlers[item.MwareType]
		if !ok {
			log.Errorf("no handler for %s", mwId)
			continue
		}

		err := handler.Fini(conf, &item)
		if err != nil {
			ret = err
			dbMwareUnlock(project, mwId)
			log.Errorf("Failed cleanup for mware %s", mwId)
			continue
		}

		err = dbMwareRemoveLocked(item)
		if err != nil {
			ret = err
			log.Errorf("Can't remove mware %s", err.Error())
			continue
		}
	}

	if ret != nil {
		return fmt.Errorf("%s", ret.Error())
	}

	return nil
}

func mwareSetup(conf *YAMLConf, project string, mwares []swyapi.MwareItem, fn *FunctionDesc) error {
	var mwares_complete []string
	var jsettings []byte
	var found bool
	var err error

	for _, mware := range mwares {
		var mwd MwareDesc

		log.Debugf("set up wmare %s:%s", mware.ID, mware.Type)
		found, mwd, err = dbMwareAddRefOrInsertLocked(project, mware.ID)
		if err == nil && found == false {
			if mware.Type == "" {
				err = fmt.Errorf("no type for new mware %s", mware.ID)
				goto out
			}

			handler, ok := mwareHandlers[mware.Type]
			if !ok {
				err = fmt.Errorf("no handler for %s:%s", mware.ID, mware.Type)
				dbMwareRemove(mwd)
				goto out
			}

			//
			// If mware not found either plain mware IDs are
			// provided so we had to fetch it or need to
			// setup new ones.
			jsettings, err = handler.Init(conf, &mwd, &mware)
			if err == nil {
				mwd.MwareType = mware.Type
				err = dbMwareAddSettingsUnlock(mwd, jsettings)
			}
		}

		if err != nil {
			err = fmt.Errorf("mwareSetup: Can't setup mwareid %s: %s", mware.ID, err.Error())
			goto out
		}

		mwares_complete = append(mwares_complete, mware.ID)
	}

	if fn != nil {
		fn.Mware = mwares_complete
	}

	return nil

out:
	log.Errorf("mwareSetup: %s", err.Error())
	mwareRemove(conf, project, mwares_complete)
	return err
}

func mwareEventSetup(conf *YAMLConf, fn *FunctionDesc, on bool) error {
	item, err := dbMwareGetItem(fn.Project, fn.Event.MwareId)
	if err != nil {
		log.Errorf("Can't find mware %s for event", fn.Event.MwareId)
		return err
	}

	log.Debugf("set up event for %s.%s mware", fn.Event.MwareId, item.MwareType)

	iface := mwareHandlers[item.MwareType]
	if iface != nil {
		return iface.Event(conf, &fn.Event, &item, on)
	}

	return fmt.Errorf("No mware for event")
}
