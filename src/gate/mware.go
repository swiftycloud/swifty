package main

import (
	"gopkg.in/mgo.v2/bson"
	"strings"
	"net/http"
	"fmt"
	"context"
	"errors"

	"../apis"
	"../common"
	"../common/crypto"
)

type MwareDesc struct {
	// These objects are kept in Mongo, which requires the below
	// field to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`

	SwoId				`bson:",inline"`
	Labels		[]string	`bson:"labels"`
	Cookie		string		`bson:"cookie"`
	MwareType	string		`bson:"mwaretype"`	// Middleware type
	Client		string		`bson:"client,omitempty"`		// Middleware client (db user)
	Secret		string		`bson:"secret"`		// Client secret (e.g. password)
	Namespace	string		`bson:"namespace,omitempty"`	// Client namespace (e.g. dbname, mq domain)
	State		int		`bson:"state"`		// Mware state
	UserData	string		`bson:"userdata,omitempty"`
}

var mwStates = map[int]string {
	swy.DBMwareStatePrp:	"preparing",
	swy.DBMwareStateRdy:	"ready",
	swy.DBMwareStateTrm:	"terminating",
	swy.DBMwareStateStl:	"stalled",
	swy.DBMwareStateNo:	"dead",
}

func (mw *MwareDesc)ToState(ctx context.Context, st, from int) error {
	q := bson.M{}
	if from != -1 {
		q["state"] = from
	}

	err := dbUpdatePart2(ctx, mw, q, bson.M{"state": st})
	if err == nil {
		mw.State = st
	}

	return err
}

type MwareOps struct {
	Init	func(ctx context.Context, mwd *MwareDesc) (error)
	Fini	func(ctx context.Context, mwd *MwareDesc) (error)
	GetEnv	func(ctx context.Context, mwd *MwareDesc) (map[string][]byte)
	Info	func(ctx context.Context, mwd *MwareDesc, ifo *swyapi.MwareInfo) (error)
	Devel	bool
	LiteOK	bool
}

func mkEnvName(typ, name, env string) string {
	return "MWARE_" + strings.ToUpper(typ) + strings.ToUpper(name) + "_" + env
}

func (mw *MwareDesc)envName(envName string) string {
	return mkEnvName(mw.MwareType, mw.Name, envName)
}

func (mwd *MwareDesc)stdEnvs(mwaddr string) map[string][]byte {
	return map[string][]byte {
		mwd.envName("ADDR"): []byte(mwaddr),
		mwd.envName("USER"): []byte(mwd.Client),
		mwd.envName("PASS"): []byte(mwd.Secret),
	}
}

func mwareGetCookie(ctx context.Context, id SwoId, name string) (string, error) {
	var mw MwareDesc

	id.Name = name
	err := dbFind(ctx, id.dbReq(), &mw)
	if err != nil {
		return "", fmt.Errorf("No such mware: %s", id.Str())
	}
	if mw.State != swy.DBMwareStateRdy {
		return "", errors.New("Mware not ready")
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

func listMwares(ctx context.Context, project, name, mtype string, labels []string) ([]*MwareDesc, *swyapi.GateErr) {
	var mws []*MwareDesc

	if name == "" {
		q := listReq(ctx, project, labels)
		if mtype != "" {
			q = append(q, bson.DocElem{"mwaretype", mtype})
		}
		err := dbFindAll(ctx, q, &mws)
		if err != nil {
			return nil, GateErrD(err)
		}
	} else {
		var mw MwareDesc

		err := dbFind(ctx, cookieReq(ctx, project, name), &mw)
		if err != nil {
			return nil, GateErrD(err)
		}
		mws = append(mws, &mw)
	}

	return mws, nil
}

var mwareHandlers = map[string]*MwareOps {
	"maria":	&MwareMariaDB,
	"postgres":	&MwarePostgres,
	"rabbit":	&MwareRabbitMQ,
	"mongo":	&MwareMongo,
	"authjwt":	&MwareAuthJWT,
}

func mwareRemoveId(ctx context.Context, id *SwoId) *swyapi.GateErr {
	var item MwareDesc

	err := dbFind(ctx, id.dbReq(), &item)
	if err != nil {
		return GateErrD(err)
	}

	return item.Remove(ctx)
}

func (item *MwareDesc)Remove(ctx context.Context) *swyapi.GateErr {
	handler, ok := mwareHandlers[item.MwareType]
	if !ok {
		return GateErrC(swy.GateGenErr) /* Shouldn't happen */
	}

	err := item.ToState(ctx, swy.DBMwareStateTrm, item.State)
	if err != nil {
		ctxlog(ctx).Errorf("Can't terminate mware %s", item.SwoId.Str())
		return GateErrM(swy.GateGenErr, "Cannot terminate mware")
	}

	err = handler.Fini(ctx, item)
	if err != nil {
		ctxlog(ctx).Errorf("Failed cleanup for mware %s: %s", item.SwoId.Str(), err.Error())
		goto stalled
	}

	err = swk8sMwSecretRemove(ctx, item.Cookie)
	if err != nil {
		ctxlog(ctx).Errorf("Failed secret cleanup for mware %s: %s", item.SwoId.Str(), err.Error())
		goto stalled
	}

	err = dbRemove(ctx, item)
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove mware %s: %s", item.SwoId.Str(), err.Error())
		goto stalled
	}
	gateMwares.WithLabelValues(item.MwareType).Dec()

	return nil

stalled:
	item.ToState(ctx, swy.DBMwareStateStl, -1)
	return GateErrE(swy.GateGenErr, err)
}

func (item *MwareDesc)toFnInfo(ctx context.Context) *swyapi.MwareInfo {
	return &swyapi.MwareInfo {
		ID: item.ObjID.Hex(),
		Name: item.SwoId.Name,
		Type: item.MwareType,
	}
}

type Mwares struct {}

func (_ Mwares)create(ctx context.Context, r *http.Request, p interface{}) (Obj, *swyapi.GateErr) {
	params := p.(*swyapi.MwareAdd)
	id := ctxSwoId(ctx, params.Project, params.Name)
	return getMwareDesc(id, params), nil
}

func (mw *MwareDesc)add(ctx context.Context, params interface{}) *swyapi.GateErr {
	return mw.Setup(ctx)
}

func (mw *MwareDesc)info(ctx context.Context, r *http.Request, details bool) (interface{}, *swyapi.GateErr) {
	return mw.toInfo(ctx, details)
}

func (mw *MwareDesc)upd(ctx context.Context, upd interface{}) *swyapi.GateErr {
	return GateErrM(swy.GateGenErr, "Not updatable")
}

func (mw *MwareDesc)del(ctx context.Context) *swyapi.GateErr {
	return mw.Remove(ctx)
}

func (item *MwareDesc)toInfo(ctx context.Context, details bool) (*swyapi.MwareInfo, *swyapi.GateErr) {
	resp := &swyapi.MwareInfo{
		ID:		item.ObjID.Hex(),
		Name:		item.SwoId.Name,
		Project:	item.SwoId.Project,
		Type:		item.MwareType,
		Labels:		item.Labels,
	}

	if details {
		resp.UserData = item.UserData

		handler, ok := mwareHandlers[item.MwareType]
		if !ok {
			return nil, GateErrC(swy.GateGenErr) /* Shouldn't happen */
		}

		if handler.Info != nil {
			err := handler.Info(ctx, item, resp)
			if err != nil {
				return nil, GateErrE(swy.GateGenErr, err)
			}
		}
	}

	return resp, nil
}

func getMwareStats(ctx context.Context, ten string) (map[string]*swyapi.TenantStatsMware, *swyapi.GateErr) {
	var mws []*MwareDesc

	err := dbFindAll(ctx, bson.M{"tennant": ten}, &mws)
	if err != nil {
		return nil, GateErrD(err)
	}

	mst := make(map[string]*swyapi.TenantStatsMware)
	for _, mw := range mws {
		st, ok := mst[mw.MwareType]
		if !ok {
			st = &swyapi.TenantStatsMware{}
			mst[mw.MwareType] = st
		}

		st.Count++

		h := mwareHandlers[mw.MwareType]
		if h.Info != nil {
			var ifo swyapi.MwareInfo

			err := h.Info(ctx, mw, &ifo)
			if err != nil {
				return nil, GateErrE(swy.GateGenErr, err)
			}

			if ifo.DU != nil {
				if st.DU == nil {
					var du uint64
					st.DU = &du
				}
				*st.DU += *ifo.DU
			}
		}
	}

	return mst, nil
}

func getMwareDesc(id *SwoId, params *swyapi.MwareAdd) *MwareDesc {
	ret := &MwareDesc {
		SwoId:		*id,
		MwareType:	params.Type,
		State:		swy.DBMwareStatePrp,
		UserData:	params.UserData,
	}

	ret.Cookie = ret.SwoId.Cookie()
	return ret
}

func (mwd *MwareDesc)Setup(ctx context.Context) *swyapi.GateErr {
	var handler *MwareOps
	var ok bool
	var err, erc error

	ctxlog(ctx).Debugf("set up wmare %s:%s", mwd.SwoId.Str(), mwd.MwareType)

	mwd.ObjID = bson.NewObjectId()
	err = dbInsert(ctx, mwd)
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

	if isLite() && !handler.LiteOK {
		err = fmt.Errorf("Bad mware type %s", mwd.MwareType)
		goto outdb
	}

	err = handler.Init(ctx, mwd)
	if err != nil {
		err = fmt.Errorf("mware init error: %s", err.Error())
		goto outdb
	}

	err = swk8sMwSecretAdd(ctx, mwd.Cookie, handler.GetEnv(ctx, mwd))
	if err != nil {
		goto outh
	}

	mwd.Secret, err = swycrypt.EncryptString(gateSecPas, mwd.Secret)
	if err != nil {
		ctxlog(ctx).Errorf("Mw secret encrypt error: %s", err.Error())
		err = errors.New("Encrypt error")
		goto outs
	}

	mwd.State = swy.DBMwareStateRdy
	err = dbUpdatePart(ctx, mwd, bson.M {
				"client":	mwd.Client,
				"secret":	mwd.Secret,
				"namespace":	mwd.Namespace,
				"state":	mwd.State })
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
	erc = handler.Fini(ctx, mwd)
	if erc != nil {
		goto stalled
	}
outdb:
	erc = dbRemove(ctx, mwd)
	if erc != nil {
		goto stalled
	}
	gateMwares.WithLabelValues(mwd.MwareType).Dec()
out:
	ctxlog(ctx).Errorf("mwareSetup: %s", err.Error())
	return GateErrE(swy.GateGenErr, err)

stalled:
	mwd.ToState(ctx, swy.DBMwareStateStl, -1)
	goto out
}
