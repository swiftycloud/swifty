package main

import (
	"gopkg.in/mgo.v2/bson"
	"strings"
	"net/url"
	"fmt"
	"context"
	"errors"

	"../apis"
	"../common"
	"../common/crypto"
	"../common/xrest"
)

const (
	DBMwareStatePrp	int = 1		// Preparing
	DBMwareStateRdy	int = 2		// Ready
	DBMwareStateTrm	int = 3		// Terminating
	DBMwareStateStl	int = 4		// Stalled (while terminating or cleaning up)

	DBMwareStateNo	int = -1	// Doesn't exists :)
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
	DBMwareStatePrp:	"preparing",
	DBMwareStateRdy:	"ready",
	DBMwareStateTrm:	"terminating",
	DBMwareStateStl:	"stalled",
	DBMwareStateNo:	"dead",
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
	if mw.State != DBMwareStateRdy {
		return "", errors.New("Mware not ready")
	}

	return mw.Cookie, nil
}

func mwareGenerateUserPassClient(ctx context.Context, mwd *MwareDesc) (error) {
	var err error

	mwd.Client, err = xh.GenRandId(32)
	if err != nil {
		ctxlog(ctx).Errorf("Can't generate clnt for %s: %s", mwd.SwoId.Str(), err.Error())
		return err
	}

	mwd.Secret, err = xh.GenRandId(64)
	if err != nil {
		ctxlog(ctx).Errorf("Can't generate secret for %s: %s", mwd.SwoId.Str(), err.Error())
		return err
	}

	return nil
}

func listMwares(ctx context.Context, project, name, mtype string, labels []string) ([]*MwareDesc, *xrest.ReqErr) {
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

func mwareRemoveId(ctx context.Context, id *SwoId) *xrest.ReqErr {
	var item MwareDesc

	err := dbFind(ctx, id.dbReq(), &item)
	if err != nil {
		return GateErrD(err)
	}

	return item.Del(ctx)
}

func (item *MwareDesc)Del(ctx context.Context) *xrest.ReqErr {
	handler, ok := mwareHandlers[item.MwareType]
	if !ok {
		return GateErrC(swyapi.GateGenErr) /* Shouldn't happen */
	}

	err := item.ToState(ctx, DBMwareStateTrm, item.State)
	if err != nil {
		ctxlog(ctx).Errorf("Can't terminate mware %s", item.SwoId.Str())
		return GateErrM(swyapi.GateGenErr, "Cannot terminate mware")
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
	item.ToState(ctx, DBMwareStateStl, -1)
	return GateErrE(swyapi.GateGenErr, err)
}

func (item *MwareDesc)toFnInfo(ctx context.Context) *swyapi.MwareInfo {
	return &swyapi.MwareInfo {
		ID: item.ObjID.Hex(),
		Name: item.SwoId.Name,
		Type: item.MwareType,
	}
}

type Mwares struct {}

func (_ Mwares)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	mwtype := q.Get("type")
	mname := q.Get("name")

	mws, cerr := listMwares(ctx, project, mname, mwtype, q["label"])
	if cerr != nil {
		return cerr
	}

	for _, mw := range mws {
		cerr = cb(ctx, mw)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func (_ Mwares)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.MwareAdd)
	id := ctxSwoId(ctx, params.Project, params.Name)
	return getMwareDesc(id, params)
}

func (mw *MwareDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return mw.toInfo(ctx, details)
}

func (mw *MwareDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrM(swyapi.GateGenErr, "Not updatable")
}

func (item *MwareDesc)toInfo(ctx context.Context, details bool) (*swyapi.MwareInfo, *xrest.ReqErr) {
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
			return nil, GateErrC(swyapi.GateGenErr) /* Shouldn't happen */
		}

		if handler.Info != nil {
			err := handler.Info(ctx, item, resp)
			if err != nil {
				return nil, GateErrE(swyapi.GateGenErr, err)
			}
		}
	}

	return resp, nil
}

func getMwareStats(ctx context.Context, ten string) (map[string]*swyapi.TenantStatsMware, *xrest.ReqErr) {
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
				return nil, GateErrE(swyapi.GateGenErr, err)
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

func getMwareDesc(id *SwoId, params *swyapi.MwareAdd) (*MwareDesc, *xrest.ReqErr) {
	if !id.NameOK() {
		return nil, GateErrM(swyapi.GateBadRequest, "Bad function name")
	}

	ret := &MwareDesc {
		SwoId:		*id,
		MwareType:	params.Type,
		State:		DBMwareStatePrp,
		UserData:	params.UserData,
	}

	ret.Cookie = ret.SwoId.Cookie()
	return ret, nil
}

func (mwd *MwareDesc)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
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

	mwd.Secret, err = xcrypt.EncryptString(gateSecPas, mwd.Secret)
	if err != nil {
		ctxlog(ctx).Errorf("Mw secret encrypt error: %s", err.Error())
		err = errors.New("Encrypt error")
		goto outs
	}

	mwd.State = DBMwareStateRdy
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
	return GateErrE(swyapi.GateGenErr, err)

stalled:
	mwd.ToState(ctx, DBMwareStateStl, -1)
	goto out
}
