package main

import (
	"gopkg.in/mgo.v2/bson"
	"strings"
	"fmt"

	"../apis/apps"
	"../common"
)

type MwareDesc struct {
	// These objects are kept in Mongo, which requires the below
	// field to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`

	SwoId				`bson:",inline"`
	Cookie		string		`bson:"cookie"`
	MwareType	string		`bson:"mwaretype"`	// Middleware type
	Client		string		`bson:"client"`		// Middleware client
	Pass		string		`bson:"pass"`		// Client password
	Counter		int		`bson:"counter"`	// Middleware instance counter
	State		int		`bson:"state"`		// Mware state

	JSettings	string		`bson:"jsettings"`	// Middleware settings in json format
}

type MwareOps struct {
	Init	func(conf *YAMLConf, mwd *MwareDesc, mware *swyapi.MwareItem) ([]byte, error)
	Fini	func(conf *YAMLConf, mwd *MwareDesc) (error)
	Event	func(conf *YAMLConf, source *FnEventDesc, mwd *MwareDesc, on bool) (error)
	GetEnv	func(conf *YAMLConf, mwd *MwareDesc) ([]string)
}

func mkEnv(mwd *MwareDesc, envName, value string) string {
	return "MWARE_" + strings.ToUpper(mwd.Name) + "_" + envName + "=" + value
}

func mwGenEnvs(mwd *MwareDesc, mwaddr string) ([]string) {
	return []string{
		mkEnv(mwd, "ADDR", mwaddr),
		mkEnv(mwd, "USER", mwd.Client),
		mkEnv(mwd, "PASS", mwd.Pass),
	}
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

var mwareHandlers = map[string]MwareOps {
	"sql":		MwareMariaDB,
	"mq":		MwareRabbitMQ,
	"mongo":	MwareMongo,
}

func mwareGetEnv(conf *YAMLConf, id *SwoId) ([]string, error) {
	// No mware lock needed here since it's a pure
	// read with mware counter increased already so
	// can't disappear
	item, err := dbMwareGetItem(id)
	if err != nil {
		return nil, fmt.Errorf("Can't fetch settings for mware %s", id.Str())
	}

	handler, ok := mwareHandlers[item.MwareType]
	if !ok {
		return nil, fmt.Errorf("No handler for %s mware", id.Str())
	}

	return handler.GetEnv(conf, &item), nil
}

func mwareGetFnEnv(conf *YAMLConf, fn *FunctionDesc) ([]string, error) {
	var envs []string

	for _, mwId := range fn.Mware {
		env, err := mwareGetEnv(conf, makeSwoId(fn.Tennant, fn.Project, mwId))
		if err != nil {
			return nil, err
		}

		envs = append(envs, env...)
	}

	return envs, nil
}

func mwareRemove(conf *YAMLConf, id SwoId, mwIds []string) error {
	var ret error = nil

	for _, mwId := range mwIds {
		id.Name = mwId

		log.Debugf("remove wmare %s", id.Str())
		if dbMwareLock(&id) != nil {
			log.Errorf("Can't lock mware %s", id.Str())
			continue
		}

		removed, item, _ := dbMwareDecRefLocked(&id)
		if removed == false {
			dbMwareUnlock(&id)
			continue
		}

		handler, ok := mwareHandlers[item.MwareType]
		if !ok {
			log.Errorf("no handler for %s", id.Str())
			continue
		}

		err := handler.Fini(conf, &item)
		if err != nil {
			ret = err
			dbMwareUnlock(&id)
			log.Errorf("Failed cleanup for mware %s", id.Str())
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

func mwareSetup(conf *YAMLConf, id SwoId, mwares []swyapi.MwareItem, fn *FunctionDesc) error {
	var mwares_complete []string
	var jsettings []byte
	var found bool
	var err error

	for _, mware := range mwares {
		var mwd MwareDesc

		id.Name = mware.ID

		log.Debugf("set up wmare %s:%s", id.Str(), mware.Type)
		found, mwd, err = dbMwareAddRefOrInsertLocked(&id)
		if err == nil && found == false {
			if mware.Type == "" {
				err = fmt.Errorf("no type for new mware %s", id.Str())
				goto out
			}

			handler, ok := mwareHandlers[mware.Type]
			if !ok {
				err = fmt.Errorf("no handler for %s:%s", id.Str(), mware.Type)
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
			err = fmt.Errorf("mwareSetup: Can't setup mwareid %s: %s", id.Str(), err.Error())
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
	mwareRemove(conf, id, mwares_complete)
	return err
}

func mwareEventSetup(conf *YAMLConf, fn *FunctionDesc, on bool) error {
	item, err := dbMwareGetItem(makeSwoId(fn.Tennant, fn.Project, fn.Event.MwareId))
	if err != nil {
		log.Errorf("Can't find mware %s for event", fn.Event.MwareId)
		return err
	}

	log.Debugf("set up event for %s.%s mware", fn.Event.MwareId, item.MwareType)

	iface, ok := mwareHandlers[item.MwareType]
	if ok {
		return iface.Event(conf, &fn.Event, &item, on)
	}

	return fmt.Errorf("No mware for event")
}
