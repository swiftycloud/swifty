package main

import (
	"gopkg.in/mgo.v2/bson"
	"strings"
	"net/url"
	"net/http"
	"fmt"
	"context"
	"errors"

	"swifty/apis"
	"swifty/common"
	"swifty/common/crypto"
	"swifty/common/xrest"
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
	HDat		map[string]string	`bson:"hdat",omitempty"`
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
	Setup	func(mwd *MwareDesc, p *swyapi.MwareAdd)
	Init	func(ctx context.Context, mwd *MwareDesc) (error)
	Fini	func(ctx context.Context, mwd *MwareDesc) (error)
	GetEnv	func(ctx context.Context, mwd *MwareDesc) (map[string][]byte)
	Info	func(ctx context.Context, mwd *MwareDesc, ifo *swyapi.MwareInfo) (error)
	Devel	bool
	LiteOK	bool
}

func mkEnvName(typ, name, env string) string {
	return "MWARE_" + strings.ToUpper(typ) + strings.Replace(strings.ToUpper(name), ".", "", -1) + "_" + env
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

func mwSecEnv(ctx context.Context, h *MwareOps, mw *MwareDesc) *secEnvs {
	return &secEnvs{
		id: "mw-" + mw.Cookie,
		envs: h.GetEnv(ctx, mw),
	}
}

func mwareGetEnvData(ctx context.Context, id SwoId, name string) (*secEnvs, error) {
	var mw MwareDesc

	id.Name = name
	err := dbFind(ctx, id.dbReq(), &mw)
	if err != nil {
		return nil, fmt.Errorf("No such mware: %s", id.Str())
	}
	if mw.State != DBMwareStateRdy {
		return nil, errors.New("Mware not ready")
	}

	handler := mwareHandlers[mw.MwareType]
	return mwSecEnv(ctx, handler, &mw), nil
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

var mwareHandlers = map[string]*MwareOps {
	"maria":	&MwareMariaDB,
	"postgres":	&MwarePostgres,
	"rabbit":	&MwareRabbitMQ,
	"mongo":	&MwareMongo,
	"authjwt":	&MwareAuthJWT,
	"websocket":	&MwareWebSocket,
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

	err = k8sSecretRemove(ctx, "mw-" + item.Cookie)
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
		Id: item.ObjID.Hex(),
		Name: item.SwoId.Name,
		Type: item.MwareType,
	}
}

type Mwares struct { }

func (_ Mwares)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var mw MwareDesc

	cerr := objFindForReq(ctx, r, "mid", &mw)
	if cerr != nil {
		return nil, cerr
	}

	return &mw, nil
}

func (_ Mwares)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	var mw MwareDesc

	mname := q.Get("name")
	if mname != "" {

		err := dbFind(ctx, cookieReq(ctx, project, mname), &mw)
		if err != nil {
			return GateErrD(err)
		}

		return cb(ctx, &mw)
	}

	mwtype := q.Get("type")

	dbq := listReq(ctx, project, q["label"])
	if mwtype != "" {
		dbq = append(dbq, bson.DocElem{"mwaretype", mwtype})
	}

	iter := dbIterAll(ctx, dbq, &mw)
	defer iter.Close()

	for iter.Next(&mw) {
		cerr := cb(ctx, &mw)
		if cerr != nil {
			return cerr
		}
	}

	err := iter.Err()
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (_ Mwares)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.MwareAdd)
	id := ctxSwoId(ctx, params.Project, params.Name)
	return getMwareDesc(id, params)
}

type FnMwares struct {
	Fn	*FunctionDesc
}

func (fm FnMwares)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var mw MwareDesc

	cerr := objFindForReq2(ctx, r, "mid", &mw, bson.M{"project": fm.Fn.SwoId.Project})
	if cerr != nil {
		return nil, cerr
	}

	return &FnMware{Fn:fm.Fn, Mw:&mw}, nil
}

func (fm FnMwares)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	var mw MwareDesc

	cerr := objFindId(ctx, *p.(*string), &mw, bson.M{"project": fm.Fn.SwoId.Project})
	if cerr != nil {
		return nil, cerr
	}

	return &FnMware{Fn:fm.Fn, Mw:&mw}, nil
}

func (fm FnMwares)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	for _, mwn := range fm.Fn.Mware {
		id := fm.Fn.SwoId
		id.Name = mwn

		var mw MwareDesc

		fmw := FnMware{Fn: fm.Fn}

		err := dbFind(ctx, id.dbReq(), &mw)
		if err == nil {
			fmw.Mw = &mw
		} else {
			fmw.Name = mwn
		}

		cerr := cb(ctx, &fmw)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

type FnMware struct {
	Fn	*FunctionDesc
	Mw	*MwareDesc
	Name	string
}

func (fmw *FnMware)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	return fmw.Fn.addMware(ctx, fmw.Mw)
}

func (fmw *FnMware)Del(ctx context.Context) *xrest.ReqErr {
	return fmw.Fn.delMware(ctx, fmw.Mw)
}

func (fmw *FnMware)Upd(ctx context.Context, _ interface{}) *xrest.ReqErr {
	return GateErrM(swyapi.GateGenErr, "Not updatable")
}

func (fmw *FnMware)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	if fmw.Mw != nil {
		return fmw.Mw.toInfo(ctx, details)
	} else {
		return &swyapi.MwareInfo{Name: fmw.Name}, nil
	}
}

func (mw *MwareDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return mw.toInfo(ctx, details)
}

func (mw *MwareDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrM(swyapi.GateGenErr, "Not updatable")
}

func (item *MwareDesc)toInfo(ctx context.Context, details bool) (*swyapi.MwareInfo, *xrest.ReqErr) {
	resp := &swyapi.MwareInfo{
		Id:		item.ObjID.Hex(),
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

func getMwareStats(ctx context.Context) (map[string]*swyapi.TenantStatsMware, *xrest.ReqErr) {
	var mw MwareDesc

	ten := gctx(ctx).Tenant

	iter := dbIterAll(ctx, bson.M{"tennant": ten}, &mw)
	defer iter.Close()

	mst := make(map[string]*swyapi.TenantStatsMware)
	for iter.Next(&mw) {
		st, ok := mst[mw.MwareType]
		if !ok {
			st = &swyapi.TenantStatsMware{}
			mst[mw.MwareType] = st
		}

		st.Count++

		h := mwareHandlers[mw.MwareType]
		if h.Info != nil {
			var ifo swyapi.MwareInfo

			err := h.Info(ctx, &mw, &ifo)
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

	err := iter.Err()
	if err != nil {
		return nil, GateErrD(err)
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

	handler, ok := mwareHandlers[params.Type]
	if !ok {
		return nil, GateErrM(swyapi.GateBadRequest, "Not such type")
	}

	if handler.Setup != nil {
		handler.Setup(ret, params)
	}

	ret.Cookie = ret.SwoId.Cookie()
	return ret, nil
}

func (mwd *MwareDesc)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	var handler *MwareOps
	var err, erc error

	mwd.ObjID = bson.NewObjectId()
	err = dbInsert(ctx, mwd)
	if err != nil {
		ctxlog(ctx).Errorf("Can't add mware %s: %s", mwd.SwoId.Str(), err.Error())
		return GateErrD(err)
	}

	gateMwares.WithLabelValues(mwd.MwareType).Inc()

	handler, _ = mwareHandlers[mwd.MwareType]

	if handler.Devel && !ModeDevel {
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

	err = k8sSecretAdd(ctx, mwSecEnv(ctx, handler, mwd))
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
	erc = k8sSecretRemove(ctx, "mw-" + mwd.Cookie)
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
