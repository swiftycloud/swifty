package main

import (
	"time"
	"fmt"
	"context"
	"reflect"
	"net/http"
	"github.com/gorilla/mux"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"swifty/gate/mgo"
	"swifty/common"
	"swifty/apis"
	"swifty/common/xrest"
)

const (
	DBStateDB	= "swifty"
	DBTenantDB	= "swifty-tenant"
	DBColFunc	= "Function"
	DBColMware	= "Mware"
	DBColLogs	= "Logs"
	DBColFnStats	= "FnStats"
	DBColFnStatsA	= "FnStatsArch"
	DBColTenStats	= "TenantStats"
	DBColTenStatsA	= "TenantStatsArch"
	DBColBal	= "BalancerRS"
	DBColDeploy	= "Deploy"
	DBColLimits	= "Limits"
	DBColEvents	= "Events"
	DBColRepos	= "Repos"
	DBColAccounts	= "Accounts"
	DBColRouters	= "Routers"
)

var dbColMap map[reflect.Type]string

func init() {
	dbColMap = make(map[reflect.Type]string)
	dbColMap[reflect.TypeOf(MwareDesc{})] = DBColMware
	dbColMap[reflect.TypeOf(&MwareDesc{})] = DBColMware
	dbColMap[reflect.TypeOf([]*MwareDesc{})] = DBColMware
	dbColMap[reflect.TypeOf(&[]*MwareDesc{})] = DBColMware
	dbColMap[reflect.TypeOf(FunctionDesc{})] = DBColFunc
	dbColMap[reflect.TypeOf(&FunctionDesc{})] = DBColFunc
	dbColMap[reflect.TypeOf([]*FunctionDesc{})] = DBColFunc
	dbColMap[reflect.TypeOf(&[]*FunctionDesc{})] = DBColFunc
	dbColMap[reflect.TypeOf(DeployDesc{})] = DBColDeploy
	dbColMap[reflect.TypeOf(&DeployDesc{})] = DBColDeploy
	dbColMap[reflect.TypeOf([]*DeployDesc{})] = DBColDeploy
	dbColMap[reflect.TypeOf(&[]*DeployDesc{})] = DBColDeploy
	dbColMap[reflect.TypeOf(FnEventDesc{})] = DBColEvents
	dbColMap[reflect.TypeOf(&FnEventDesc{})] = DBColEvents
	dbColMap[reflect.TypeOf([]*FnEventDesc{})] = DBColEvents
	dbColMap[reflect.TypeOf(&[]*FnEventDesc{})] = DBColEvents
	dbColMap[reflect.TypeOf(RepoDesc{})] = DBColRepos
	dbColMap[reflect.TypeOf(&RepoDesc{})] = DBColRepos
	dbColMap[reflect.TypeOf([]*RepoDesc{})] = DBColRepos
	dbColMap[reflect.TypeOf(&[]*RepoDesc{})] = DBColRepos
	dbColMap[reflect.TypeOf(AccDesc{})] = DBColAccounts
	dbColMap[reflect.TypeOf(&AccDesc{})] = DBColAccounts
	dbColMap[reflect.TypeOf([]*AccDesc{})] = DBColAccounts
	dbColMap[reflect.TypeOf(&[]*AccDesc{})] = DBColAccounts
	dbColMap[reflect.TypeOf(RouterDesc{})] = DBColRouters
	dbColMap[reflect.TypeOf(&RouterDesc{})] = DBColRouters
	dbColMap[reflect.TypeOf([]*RouterDesc{})] = DBColRouters
	dbColMap[reflect.TypeOf(&[]*RouterDesc{})] = DBColRouters
}

func dbCol(ctx context.Context, col string) *mgo.Collection {
	return gctx(ctx).S.DB(DBStateDB).C(col)
}

func dbColSlow(ctx context.Context, object interface{}) *mgo.Collection {
	typ := reflect.TypeOf(object)
	if name, ok := dbColMap[typ]; ok {
		return dbCol(ctx, name)
	}
	glog.Fatalf("Unmapped object %s", typ.String())
	return nil
}

func objcni(o interface{}) (string, bson.ObjectId) {
	switch o := o.(type) {
	case *AccDesc:
		return DBColAccounts, o.ObjID
	case *RepoDesc:
		return DBColRepos, o.ObjID
	case *MwareDesc:
		return DBColMware, o.ObjID
	case *DeployDesc:
		return DBColDeploy, o.ObjID
	case *FunctionDesc:
		return DBColFunc, o.ObjID
	case *FnEventDesc:
		return DBColEvents, o.ObjID
	case *RouterDesc:
		return DBColRouters, o.ObjID
	default:
		glog.Fatalf("Unmapped object %s", reflect.TypeOf(o).String())
		return "", ""
	}
}

func objq(ctx context.Context, o interface{}) (*mgo.Collection, bson.M) {
	col, id := objcni(o)
	return dbCol(ctx, col), bson.M{"_id": id}
}

func dbRemove(ctx context.Context, o interface{}) error {
	col, q := objq(ctx, o)
	return col.Remove(q)
}

func dbInsert(ctx context.Context, o interface{}) error {
	col, _ := objq(ctx, o)
	return col.Insert(o)
}

func dbFindAll(ctx context.Context, q interface{}, o interface{}) error {
	return dbColSlow(ctx, o).Find(q).All(o)
}

func dbIterAll(ctx context.Context, q interface{}, o interface{}) *mgo.Iter {
	col, _ := objq(ctx, o)
	return col.Find(q).Iter()
}

func dbFind(ctx context.Context, q bson.M, o interface{}) error {
	return dbColSlow(ctx, o).Find(q).One(o)
}

func dbUpdatePart2(ctx context.Context, o interface{}, q2 bson.M, u bson.M) error {
	col, q := objq(ctx, o)
	for k, v := range q2 {
		q[k] = v
	}
	return col.Update(q, bson.M{"$set": u})
}

func dbUpdatePart(ctx context.Context, o interface{}, u bson.M) error {
	col, q := objq(ctx, o)
	return col.Update(q, bson.M{"$set": u})
}

func dbUpdateAll(ctx context.Context, o interface{}) error {
	col, q := objq(ctx, o)
	return col.Update(q, o)
}

func (id *SwoId)dbReq() bson.M {
	return bson.M{"cookie": id.Cookie()}
}

func listReq(ctx context.Context, project string, labels []string) bson.D {
	q := bson.D{{"tennant", gctx(ctx).Tenant}, {"project", project}}
	for _, l := range labels {
		q = append(q, bson.DocElem{"labels", l})
	}
	return q
}

func cookieReq(ctx context.Context, project, name string) bson.M {
	return ctxSwoId(ctx, project, name).dbReq()
}

func idReq(ctx context.Context, id string, q bson.M) bson.M {
	if q == nil {
		q = bson.M{}
	}

	q["tennant"] = gctx(ctx).Tenant
	q["_id"] = bson.ObjectIdHex(id)

	return q
}

type DBLogRec struct {
	FnId		string		`bson:"fnid"`
	Event		string		`bson:"event"`
	Time		time.Time	`bson:"ts"`
	Text		string		`bson:"text"`
}

func objFindId(ctx context.Context, id string, out interface{}, q bson.M) *xrest.ReqErr {
	if !bson.IsObjectIdHex(id) {
		return GateErrM(swyapi.GateBadRequest, "Bad ID value")
	}

	err := dbFind(ctx, idReq(ctx, id, q), out)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func objFindForReq2(ctx context.Context, r *http.Request, n string, out interface{}, q bson.M) *xrest.ReqErr {
	return objFindId(ctx, mux.Vars(r)[n], out, q)
}

func objFindForReq(ctx context.Context, r *http.Request, n string, out interface{}) *xrest.ReqErr {
	return objFindForReq2(ctx, r, n, out, nil)
}

func repoFindForReq(ctx context.Context, r *http.Request, user_action bool) (*RepoDesc, *xrest.ReqErr) {
	rid := mux.Vars(r)["rid"]
	if !bson.IsObjectIdHex(rid) {
		return nil, GateErrM(swyapi.GateBadRequest, "Bad repo ID value")
	}

	var rd RepoDesc

	err := dbFind(ctx, ctxRepoId(ctx, rid), &rd)
	if err != nil {
		return nil, GateErrD(err)
	}

	if !user_action {
		gx := gctx(ctx)
		if !gx.Admin && rd.SwoId.Tennant != gx.Tenant {
			return nil, GateErrM(swyapi.GateNotAvail, "Shared repo")
		}
	}

	return &rd, nil
}

var session *mgo.Session

func dbNF(err error) bool {
	return err == mgo.ErrNotFound
}

func maybe(err error) error {
	if err == mgo.ErrNotFound {
		return nil
	} else {
		return err
	}
}

func dbTenantGetLimits(ctx context.Context, tenant string) (*swyapi.UserLimits, error) {
	c := gctx(ctx).S.DB(DBTenantDB).C(DBColLimits)
	var v swyapi.UserLimits
	err := maybe(c.Find(bson.M{"uid":tenant}).One(&v))
	return &v, err
}

func dbMwareCount(ctx context.Context) (map[string]int, error) {
	var counts []struct {
		Id	string	`bson:"_id"`
		Count	int	`bson:"count"`
	}

	err := dbCol(ctx, DBColMware).Pipe([]bson.M{
			bson.M{"$group": bson.M{
				"_id":"$mwaretype",
				"count":bson.M{"$sum": 1},
			},
		}}).All(&counts)
	if err != nil {
		return nil, err
	}

	ret := map[string]int{}
	for _, cnt := range counts {
		ret[cnt.Id] = cnt.Count
	}

	return ret, nil
}

func dbFuncCount(ctx context.Context) (int, error) {
	return dbCol(ctx, DBColFunc).Count()
}

func dbFuncCountProj(ctx context.Context, id *SwoId) (int, error) {
	return dbCol(ctx, DBColFunc).Find(bson.M{"tenant": id.Tennant, "project": id.Project}).Count()
}

func dbFuncUpdate(ctx context.Context, q, ch bson.M) (error) {
	return dbCol(ctx, DBColFunc).Update(q, ch)
}

func dbTenStatsGet(ctx context.Context, tenant string, st *TenStats) error {
	return maybe(dbCol(ctx, DBColTenStats).Find(bson.M{"tenant": tenant}).One(st))
}

func dbTenStatsGetArch(ctx context.Context, tenant string, nr int) ([]TenStats, error) {
	var ret []TenStats
	err := maybe(dbCol(ctx, DBColTenStatsA).Find(bson.M{"tenant": tenant}).Sort("-till").Limit(nr).All(&ret))
	return ret, err
}

func dbTenStatsGetLatestArch(ctx context.Context, tenant string) (*TenStats, error) {
	var ret TenStats
	a, err := dbTenStatsGetArch(ctx, tenant, 1)
	if len(a) != 0 {
		ret = a[0]
	}
	return &ret, err
}

func dbTenStatsUpdate(ctx context.Context, tenant string, delta *gmgo.TenStatValues) error {
	_, err := dbCol(ctx, DBColTenStats).Upsert(bson.M{"tenant": tenant}, bson.M{
			"$set": bson.M{"tenant": tenant},
			"$inc": bson.M{
				"called":	delta.Called,
				"runcost":	delta.RunCost,
				"bytesin":	delta.BytesIn,
				"bytesout":	delta.BytesOut,
			},
		})
	return err
}

func dbFnStatsGet(ctx context.Context, cookie string, st *FnStats) error {
	return maybe(dbCol(ctx, DBColFnStats).Find(bson.M{"cookie": cookie}).One(st))
}

func dbFnStatsGetArch(ctx context.Context, id string, nr int) ([]FnStats, error) {
	var ret []FnStats
	err := maybe(dbCol(ctx, DBColFnStatsA).Find(bson.M{"cookie": id}).Sort("-till").Limit(nr).All(&ret))
	return ret, err
}

func dbFnStatsUpdate(ctx context.Context, cookie string, delta *gmgo.FnStatValues, lastCall time.Time) error {
	_, err := dbCol(ctx, DBColFnStats).Upsert(bson.M{"cookie": cookie}, bson.M{
			"$set": bson.M{"cookie": cookie},
			"$inc": bson.M{
				"called":	delta.Called,
				"timeouts":	delta.Timeouts,
				"errors":	delta.Errors,
				"rtime":	delta.RunTime,
				"bytesin":	delta.BytesIn,
				"bytesout":	delta.BytesOut,
				"runcost":	delta.RunCost,
			},
			"$max": bson.M{"lastcall": lastCall},
		})
	return err
}

func dbFnStatsDrop(ctx context.Context, cookie string, st *FnStats) error {
	if st.Called != 0 {
		n := time.Now()
		st.Dropped = &n
		st.Till = &n

		err := dbCol(ctx, DBColFnStatsA).Insert(st)
		if err != nil {
			return err
		}
	}

	return maybe(dbCol(ctx, DBColFnStats).Remove(bson.M{"cookie": cookie}))
}

func logSaveResult(ctx context.Context, fnCookie, event, stdout, stderr string) {
	c := dbCol(ctx, DBColLogs)
	tm := time.Now()

	if stdout != "" {
		c.Insert(DBLogRec{
			FnId:		fnCookie,
			Event:		"stdout." + event,
			Time:		tm,
			Text:		stdout,
		})
	}

	if stderr != "" {
		c.Insert(DBLogRec{
			FnId:		fnCookie,
			Event:		"stderr." + event,
			Time:		tm,
			Text:		stderr,
		})
	}
}

func logSaveEvent(ctx context.Context, fnid, text string) {
	dbCol(ctx, DBColLogs).Insert(DBLogRec{
		FnId:		fnid,
		Event:		"event",
		Time:		time.Now(),
		Text:		text,
	})
}

func logGetFor(ctx context.Context, id *SwoId, since *time.Time) ([]DBLogRec, error) {
	var logs []DBLogRec
	q := bson.M{"fnid": id.Cookie()}
	if since != nil {
		q["ts"] = bson.M{"$gt": since}
	}
	err := dbCol(ctx, DBColLogs).Find(q).Sort("ts").All(&logs)
	return logs, err
}

func logRemove(ctx context.Context, fn *FunctionDesc) error {
	_, err := dbCol(ctx, DBColLogs).RemoveAll(bson.M{"fnid": fn.Cookie})
	return maybe(err)
}

func dbBalancerPodAdd(ctx context.Context, pod *k8sPod) error {
	err := dbCol(ctx, DBColBal).Insert(bson.M{
			"uid":		pod.UID,
			"wdogaddr":	pod.WdogAddr,
			"wdogport":	pod.WdogPort,
			"host":		pod.Host,
		})
	if err != nil {
		return fmt.Errorf("add: %s", err.Error())
	}

	return nil
}

func dbBalancerPodUpd(ctx context.Context, fnId string, pod *k8sPod) error {
	err := dbCol(ctx, DBColBal).Update(bson.M{"uid": pod.UID}, bson.M{"$set": bson.M {
			"fnid":		fnId,
			"fnversion":	pod.Version,
		}})
	if err != nil && err != mgo.ErrNotFound {
		return fmt.Errorf("add: %s", err.Error())
	}

	return nil
}

func dbBalancerPodDel(ctx context.Context, pod *k8sPod) (error) {
	err := maybe(dbCol(ctx, DBColBal).Remove(bson.M{ "uid": pod.UID, }))
	if err != nil {
		return fmt.Errorf("del: %s", err.Error())
	}

	return nil
}

func dbBalancerPodDelStuck(ctx context.Context) (error) {
	_, err := dbCol(ctx, DBColBal).RemoveAll(bson.M{ "fnid": bson.M{"$exists": false}})
	return maybe(err)
}

func dbBalancerPodDelAll(ctx context.Context, fnid string) (error) {
	_, err := dbCol(ctx, DBColBal).RemoveAll(bson.M{ "fnid": fnid })
	return maybe(err)
}

type balancerEntry struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	FnId		string		`bson:"fnid"`
	UID		string		`bson:"uid"`
	WdogAddr	string		`bson:"wdogaddr"`
	WdogPort	string		`bson:"wdogport"`
	Host		string		`bson:"host"`
	Version		string		`bson:"fnversion"`
}

func (be *balancerEntry)conn() *podConn {
	return &podConn {
		Addr: be.WdogAddr,
		Port: be.WdogPort,
		Host: be.Host,
		Cookie: be.FnId,
	}
}

func dbBalancerListVersions(ctx context.Context, cookie string) ([]string, error) {
	var fv []string
	err := dbCol(ctx, DBColBal).Find(bson.M{"fnid": cookie }).Distinct("fnversion", &fv)
	return fv, err
}

func dbBalancerGetConnsByCookie(ctx context.Context, cookie string) ([]*podConn, error) {
	var v []*balancerEntry

	err := dbCol(ctx, DBColBal).Find(bson.M{ "fnid": cookie }).All(&v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	var ret []*podConn
	for _, b := range(v) {
		ret = append(ret, b.conn())
	}

	return ret, nil
}

func dbBalancerGetConnExact(ctx context.Context, fnid, version string) (*podConn, error) {
	var v balancerEntry

	err := dbCol(ctx, DBColBal).Find(bson.M{
			"fnid":		fnid,
			"fnversion":	version,
		}).One(&v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	return v.conn(), nil
}

func dbProjectListAll(ctx context.Context, ten string) (fn []string, mw []string, err error) {
	err = dbCol(ctx, DBColFunc).Find(bson.M{"tennant": ten}).Distinct("project", &fn)
	if err != nil {
		return
	}

	err = dbCol(ctx, DBColMware).Find(bson.M{"tennant": ten}).Distinct("project", &mw)
	return
}

func dbConnect() error {
	var err error

	dbc := xh.ParseXCreds(conf.DB)

	info := mgo.DialInfo{
		Addrs:		[]string{dbc.Addr()},
		Database:	DBStateDB,
		Timeout:	60 * time.Second,
		Username:	dbc.User,
		Password:	gateSecrets[dbc.Pass]}

	session, err = mgo.DialWithInfo(&info);
	if err != nil {
		glog.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB, DBStateDB, err.Error())
		return err
	}

	session.SetMode(mgo.Monotonic, true)

	dbs := session.Copy()
	defer dbs.Close()

	// Make sure the indices are present
	index := mgo.Index{
			Unique:		true,
			DropDups:	true,
			Background:	true,
			Sparse:		true}

	index.Key = []string{"cookie"}
	err = dbs.DB(DBStateDB).C(DBColFunc).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for functions: %s", err.Error())
	}
	err = dbs.DB(DBStateDB).C(DBColMware).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for mware: %s", err.Error())
	}
	err = dbs.DB(DBStateDB).C(DBColAccounts).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for mware: %s", err.Error())
	}
	err = dbs.DB(DBStateDB).C(DBColRouters).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for mware: %s", err.Error())
	}

	index.Key = []string{"uid"}
	err = dbs.DB(DBStateDB).C(DBColBal).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No uid index for balancerrs: %s", err.Error())
	}

	index.Unique = false
	index.DropDups = false
	index.Key = []string{"src.repo"}
	err = dbs.DB(DBStateDB).C(DBColFunc).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No src.repo index for functions: %s", err.Error())
	}

	index.Key = []string{"name"}
	err = dbs.DB(DBStateDB).C(DBColRepos).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No name index for repos: %s", err.Error())
	}

	return nil

}

func dbDisconnect() {
	session.Close()
	session = nil
}
