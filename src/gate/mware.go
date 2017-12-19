package main

import (
	"gopkg.in/mgo.v2/bson"
	"strings"
	"fmt"

	"../common"
	"../common/crypto"
)

type MwareDesc struct {
	// These objects are kept in Mongo, which requires the below
	// field to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`

	SwoId				`bson:",inline"`
	Cookie		string		`bson:"cookie"`
	MwareType	string		`bson:"mwaretype"`	// Middleware type
	Client		string		`bson:"client"`		// Middleware client (db user)
	Secret		string		`bson:"secret"`		// Client secret (e.g. password)
	Namespace	string		`bson:"namespace"`	// Client namespace (e.g. dbname, mq domain)
	State		int		`bson:"state"`		// Mware state
}

type MwareOps struct {
	Init	func(conf *YAMLConfMw, mwd *MwareDesc) (error)
	Fini	func(conf *YAMLConfMw, mwd *MwareDesc) (error)
	Event	func(conf *YAMLConfMw, source *FnEventDesc, mwd *MwareDesc, on bool) (error)
	GetEnv	func(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string)
	Devel	bool
}

func mkEnv(mwd *MwareDesc, envName, value string) [2]string {
	return [2]string{"MWARE_" + strings.ToUpper(mwd.Name) + "_" + envName, value}
}

func mwGenUserPassEnvs(mwd *MwareDesc, mwaddr string) ([][2]string) {
	return [][2]string{
		mkEnv(mwd, "ADDR", mwaddr),
		mkEnv(mwd, "USER", mwd.Client),
		mkEnv(mwd, "PASS", mwd.Secret),
	}
}

func mwareGetCookie(id SwoId, name string) (string, error) {
	id.Name = name
	mw, err := dbMwareGetItem(&id)
	if err != nil {
		return "", fmt.Errorf("No such mware: %s", id.Str())
	}

	return mw.Cookie, nil
}

func mwareGenerateUserPassClient(mwd *MwareDesc) (error) {
	var err error

	mwd.Client, err = swy.GenRandId(32)
	if err != nil {
		return err
	}

	mwd.Secret, err = swy.GenRandId(64)
	if err != nil {
		return err
	}

	return nil
}

var mwareHandlers = map[string]*MwareOps {
	"maria":	&MwareMariaDB,
	"postgres":	&MwarePostgres,
	"rabbit":	&MwareRabbitMQ,
	"mongo":	&MwareMongo,
	"s3":		&MwareS3,
}

func forgetMware(conf *YAMLConf, handler *MwareOps, desc *MwareDesc) error {
	err := handler.Fini(&conf.Mware, desc)
	if err != nil {
		log.Errorf("Failed cleanup for mware %s: %s", desc.SwoId.Str(), err.Error())
		return err
	}

	err = swk8sMwSecretRemove(desc.Cookie)
	if err != nil {
		log.Errorf("Failed secret cleanup for mware %s: %s", desc.SwoId.Str(), err.Error())
		return err
	}

	err = dbMwareRemove(desc)
	if err != nil {
		log.Errorf("Can't remove mware %s: %s", desc.SwoId.Str(), err.Error())
		return err
	}

	return nil
}

func mwareRemove(conf *YAMLConf, id *SwoId) error {
	item, err := dbMwareGetReady(id)
	if err != nil {
		log.Errorf("Can't find mware %s", id.Str())
		return err
	}

	handler, ok := mwareHandlers[item.MwareType]
	if !ok {
		return fmt.Errorf("no handler for %s", id.Str())
	}

	err = forgetMware(conf, handler, &item)
	if err != nil {
		return err
	}

	return nil
}

func getMwareDesc(id *SwoId, mwType string) *MwareDesc {
	ret := &MwareDesc {
		SwoId: SwoId {
			Tennant:	id.Tennant,
			Project:	id.Project,
			Name:		id.Name,
		},
		MwareType:	mwType,
		State:		swy.DBMwareStateBsy,
	}

	ret.Cookie = ret.SwoId.Cookie()
	return ret
}

func mwareSetup(conf *YAMLConf, id *SwoId, mwType string) error {
	var handler *MwareOps
	var ok bool

	mwd := getMwareDesc(id, mwType)
	log.Debugf("set up wmare %s:%s", mwd.SwoId.Str(), mwType)

	err := dbMwareAdd(mwd)
	if err != nil {
		goto out
	}

	handler, ok = mwareHandlers[mwType]
	if !ok {
		err = fmt.Errorf("no handler for %s:%s", id.Str(), mwType)
		goto outdb
	}

	if handler.Devel && !SwyModeDevel {
		err = fmt.Errorf("middleware %s not enabled", mwType)
		goto outdb
	}

	err = handler.Init(&conf.Mware, mwd)
	if err != nil {
		err = fmt.Errorf("mware init error: %s", err.Error())
		goto outdb
	}

	err = swk8sMwSecretAdd(mwd.Cookie, handler.GetEnv(&conf.Mware, mwd))
	if err != nil {
		err = fmt.Errorf("mware secret add error: %s", err.Error())
		goto outh
	}

	mwd.Secret, err = swycrypt.EncryptString(gateSecPas, mwd.Secret)
	if err != nil {
		goto outs
	}

	mwd.State = swy.DBMwareStateRdy
	err = dbMwareUpdateAdded(mwd)
	if err != nil {
		goto outs
	}

	return nil

outs:
	swk8sMwSecretRemove(mwd.Cookie)
outh:
	handler.Fini(&conf.Mware, mwd)
outdb:
	dbMwareRemove(mwd)
out:
	log.Errorf("mwareSetup: %s", err.Error())
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
	if ok && (iface.Event != nil) {
		return iface.Event(&conf.Mware, &fn.Event, &item, on)
	}

	return fmt.Errorf("No mware for event")
}
