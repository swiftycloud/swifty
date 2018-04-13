package main

import (
	"gopkg.in/mgo.v2/bson"
	"strings"
	"fmt"
	"context"
	"errors"

	"../apis/apps"
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
	Client		string		`bson:"client,omitempty"`		// Middleware client (db user)
	Secret		string		`bson:"secret"`		// Client secret (e.g. password)
	Namespace	string		`bson:"namespace,omitempty"`	// Client namespace (e.g. dbname, mq domain)
	State		int		`bson:"state"`		// Mware state
	UserData	string		`bson:"userdata,omitempty"`
}

type MwareOps struct {
	Init	func(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error)
	Fini	func(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error)
	Event	func(ctx context.Context, conf *YAMLConfMw, source *FnEventDesc, mwd *MwareDesc, on bool) (error)
	GenSec	func(ctx context.Context, conf *YAMLConfMw, fid *SwoId, id string)([][2]string, error)
	GetEnv	func(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string)
	Info	func(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc, ifo *swyapi.MwareInfo) (error)
	Devel	bool
}

func mkEnvId(name, mwType, envName, value string) [2]string {
	return [2]string{"MWARE_" + strings.ToUpper(mwType) + strings.ToUpper(name) + "_" + envName, value}
}

func mkEnv(mwd *MwareDesc, envName, value string) [2]string {
	return mkEnvId(mwd.Name, mwd.MwareType, envName, value)
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
	mw, err := dbMwareGetReady(&id)
	if err != nil {
		return "", fmt.Errorf("No such mware: %s", id.Str())
	}

	return mw.Cookie, nil
}

func mwareGenerateUserPassClient(ctx context.Context, mwd *MwareDesc) (error) {
	var err error

	mwd.Client, err = swy.GenRandId(32)
	if err != nil {
		ctxlog(ctx).Errorf("Can't generate clnt for %s: %s", mwd.SwoId.Str(), err.Error())
		return err
	}

	mwd.Secret, err = swy.GenRandId(64)
	if err != nil {
		ctxlog(ctx).Errorf("Can't generate secret for %s: %s", mwd.SwoId.Str(), err.Error())
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
	"authjwt":	&MwareAuthJWT,
}

func mwareGenerateSecret(ctx context.Context, fid *SwoId, typ, id string) ([][2]string, error) {
	handler, ok := mwareHandlers[typ]
	if !ok {
		return nil, fmt.Errorf("No handler for %s mware", typ)
	}

	if handler.GenSec == nil {
		return nil, fmt.Errorf("No secrets generator for %s", typ)
	}

	return handler.GenSec(ctx, &conf.Mware, fid, id)
}

func mwareRemove(ctx context.Context, conf *YAMLConfMw, id *SwoId) *swyapi.GateErr {
	item, err := dbMwareGetItem(id)
	if err != nil {
		return GateErrD(err)
	}

	handler, ok := mwareHandlers[item.MwareType]
	if !ok {
		return GateErrC(swy.GateGenErr) /* Shouldn't happen */
	}

	err = dbMwareTerminate(&item)
	if err != nil {
		ctxlog(ctx).Errorf("Can't terminate mware %s", id.Str())
		return GateErrM(swy.GateGenErr, "Cannot terminate mware")
	}

	err = handler.Fini(ctx, conf, &item)
	if err != nil {
		ctxlog(ctx).Errorf("Failed cleanup for mware %s: %s", item.SwoId.Str(), err.Error())
		goto stalled
	}

	err = swk8sMwSecretRemove(ctx, item.Cookie)
	if err != nil {
		ctxlog(ctx).Errorf("Failed secret cleanup for mware %s: %s", item.SwoId.Str(), err.Error())
		goto stalled
	}

	err = dbMwareRemove(&item)
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove mware %s: %s", item.SwoId.Str(), err.Error())
		goto stalled
	}
	gateMwares.WithLabelValues(item.MwareType).Dec()

	return nil

stalled:
	dbMwareSetStalled(&item)
	return GateErrE(swy.GateGenErr, err)
}

func mwareInfo(ctx context.Context, conf *YAMLConfMw, id *SwoId) (*swyapi.MwareInfo, *swyapi.GateErr) {
	var item MwareDesc
	var err error

	if item, err = dbMwareGetItem(id); err != nil {
		return nil, GateErrD(err)
	}

	handler, ok := mwareHandlers[item.MwareType]
	if !ok {
		return nil, GateErrC(swy.GateGenErr) /* Shouldn't happen */
	}

	resp := &swyapi.MwareInfo{}
	resp.Type = item.MwareType
	resp.UserData = item.UserData

	if handler.Info != nil {
		err := handler.Info(ctx, conf, &item, resp)
		if err != nil {
			return nil, GateErrE(swy.GateGenErr, err)
		}
	}

	return resp, nil
}

func getMwareDesc(id *SwoId, params *swyapi.MwareAdd) *MwareDesc {
	ret := &MwareDesc {
		SwoId: SwoId {
			Tennant:	id.Tennant,
			Project:	id.Project,
			Name:		id.Name,
		},
		MwareType:	params.Type,
		State:		swy.DBMwareStatePrp,
		UserData:	params.UserData,
	}

	ret.Cookie = ret.SwoId.Cookie()
	return ret
}

func mwareSetup(ctx context.Context, conf *YAMLConfMw, id *SwoId, params *swyapi.MwareAdd) *swyapi.GateErr {
	var handler *MwareOps
	var ok bool
	var err, erc error

	mwd := getMwareDesc(id, params)
	ctxlog(ctx).Debugf("set up wmare %s:%s", mwd.SwoId.Str(), mwd.MwareType)

	err = dbMwareAdd(mwd)
	if err != nil {
		ctxlog(ctx).Errorf("Can't add mware %s: %s", mwd.SwoId.Str(), err.Error())
		return GateErrD(err)
	}

	gateMwares.WithLabelValues(mwd.MwareType).Inc()

	handler, ok = mwareHandlers[mwd.MwareType]
	if !ok {
		err = fmt.Errorf("Bad mware type %s", mwd.MwareType)
		goto outdb
	}

	if handler.Devel && !SwyModeDevel {
		err = fmt.Errorf("Bad mware type %s", mwd.MwareType)
		goto outdb
	}

	err = handler.Init(ctx, conf, mwd)
	if err != nil {
		err = fmt.Errorf("mware init error: %s", err.Error())
		goto outdb
	}

	err = swk8sMwSecretAdd(ctx, mwd.Cookie, handler.GetEnv(conf, mwd))
	if err != nil {
		goto outh
	}

	mwd.Secret, err = swycrypt.EncryptString(gateSecPas, mwd.Secret)
	if err != nil {
		ctxlog(ctx).Errorf("Mw secret encrypt error: %s", err.Error())
		err = errors.New("Encrypt error")
		goto outs
	}

	err = dbMwareUpdateAdded(mwd)
	if err != nil {
		ctxlog(ctx).Errorf("Can't update added %s: %s", mwd.SwoId.Str(), err.Error())
		err = errors.New("DB error")
		goto outs
	}

	return nil

outs:
	erc = swk8sMwSecretRemove(ctx, mwd.Cookie)
	if erc != nil {
		goto stalled
	}
outh:
	erc = handler.Fini(ctx, conf, mwd)
	if erc != nil {
		goto stalled
	}
outdb:
	erc = dbMwareRemove(mwd)
	if erc != nil {
		goto stalled
	}
	gateMwares.WithLabelValues(mwd.MwareType).Dec()
out:
	ctxlog(ctx).Errorf("mwareSetup: %s", err.Error())
	return GateErrE(swy.GateGenErr, err)

stalled:
	dbMwareSetStalled(mwd)
	goto out
}

func mwareEventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	item, err := dbMwareGetReady(makeSwoId(fn.Tennant, fn.Project, fn.Event.MwareId))
	if err != nil {
		return errors.New("No mware for event")
	}

	ctxlog(ctx).Debugf("set up event for %s.%s mware", fn.Event.MwareId, item.MwareType)

	iface, ok := mwareHandlers[item.MwareType]
	if ok && (iface.Event != nil) {
		return iface.Event(ctx, &conf.Mware, &fn.Event, &item, on)
	}

	ctxlog(ctx).Errorf("Can't find mware handler for %s.%s event", item.SwoId.Str(), item.MwareType)
	return errors.New("Bad mware for event")
}
