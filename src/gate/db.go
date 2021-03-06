/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"time"
	"fmt"
	"errors"
	"context"
	"reflect"
	"net/http"
	"runtime/debug"
	"github.com/gorilla/mux"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"swifty/gate/mgo"
	"swifty/common"
	"swifty/apis"
	"swifty/common/xrest"
	"swifty/common/xrest/sysctl"
	"swifty/common/ratelimit"
)

var dbColMap map[reflect.Type]string
var dbNotAllowed = errors.New("Not allowed")
var dbStrict bool = true
var dbwrl *xrl.RL

func init() {
	sysctl.AddBoolSysctl("db_strict_access", &dbStrict)
	dbwrl = xrl.MakeRL(1, 1)
}

func dbMayModify(ctx context.Context) bool {
	gx := gctx(ctx)
	if gx.Role != swyapi.NobodyRole {
		return true
	}

	dbAccViolations.Inc()
	if dbwrl.Get() {
		glog.Errorf("!!! ALERT: Unauthorized DB modification attempt (strict:%v)\n" +
				"ctx desc:%s ten:%s role:%s id:%d\n" +
				"%s\n===============8<====================\n",
				dbStrict, gx.Desc, gx.Tenant, gx.Role, gx.ReqId,
				string(debug.Stack()))
	}

	return !dbStrict
}

func dbMayInsert(ctx context.Context) bool {
	return dbMayModify(ctx)
}

func dbMayRemove(ctx context.Context) bool {
	return dbMayModify(ctx)
}

func dbMayUpdate(ctx context.Context) bool {
	return dbMayModify(ctx)
}

func init() {
	dbColMap = make(map[reflect.Type]string)
	dbColMap[reflect.TypeOf(MwareDesc{})] = gmgo.DBColMware
	dbColMap[reflect.TypeOf(&MwareDesc{})] = gmgo.DBColMware
	dbColMap[reflect.TypeOf([]*MwareDesc{})] = gmgo.DBColMware
	dbColMap[reflect.TypeOf(&[]*MwareDesc{})] = gmgo.DBColMware
	dbColMap[reflect.TypeOf(FunctionDesc{})] = gmgo.DBColFunc
	dbColMap[reflect.TypeOf(&FunctionDesc{})] = gmgo.DBColFunc
	dbColMap[reflect.TypeOf([]*FunctionDesc{})] = gmgo.DBColFunc
	dbColMap[reflect.TypeOf(&[]*FunctionDesc{})] = gmgo.DBColFunc
	dbColMap[reflect.TypeOf(DeployDesc{})] = gmgo.DBColDeploy
	dbColMap[reflect.TypeOf(&DeployDesc{})] = gmgo.DBColDeploy
	dbColMap[reflect.TypeOf([]*DeployDesc{})] = gmgo.DBColDeploy
	dbColMap[reflect.TypeOf(&[]*DeployDesc{})] = gmgo.DBColDeploy
	dbColMap[reflect.TypeOf(FnEventDesc{})] = gmgo.DBColEvents
	dbColMap[reflect.TypeOf(&FnEventDesc{})] = gmgo.DBColEvents
	dbColMap[reflect.TypeOf([]*FnEventDesc{})] = gmgo.DBColEvents
	dbColMap[reflect.TypeOf(&[]*FnEventDesc{})] = gmgo.DBColEvents
	dbColMap[reflect.TypeOf(RepoDesc{})] = gmgo.DBColRepos
	dbColMap[reflect.TypeOf(&RepoDesc{})] = gmgo.DBColRepos
	dbColMap[reflect.TypeOf([]*RepoDesc{})] = gmgo.DBColRepos
	dbColMap[reflect.TypeOf(&[]*RepoDesc{})] = gmgo.DBColRepos
	dbColMap[reflect.TypeOf(AccDesc{})] = gmgo.DBColAccounts
	dbColMap[reflect.TypeOf(&AccDesc{})] = gmgo.DBColAccounts
	dbColMap[reflect.TypeOf([]*AccDesc{})] = gmgo.DBColAccounts
	dbColMap[reflect.TypeOf(&[]*AccDesc{})] = gmgo.DBColAccounts
	dbColMap[reflect.TypeOf(RouterDesc{})] = gmgo.DBColRouters
	dbColMap[reflect.TypeOf(&RouterDesc{})] = gmgo.DBColRouters
	dbColMap[reflect.TypeOf([]*RouterDesc{})] = gmgo.DBColRouters
	dbColMap[reflect.TypeOf(&[]*RouterDesc{})] = gmgo.DBColRouters
}

func dbCol(ctx context.Context, col string) *mgo.Collection {
	return gctx(ctx).S.DB(gmgo.DBStateDB).C(col)
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
		return gmgo.DBColAccounts, o.ObjID
	case *RepoDesc:
		return gmgo.DBColRepos, o.ObjID
	case *MwareDesc:
		return gmgo.DBColMware, o.ObjID
	case *DeployDesc:
		return gmgo.DBColDeploy, o.ObjID
	case *FunctionDesc:
		return gmgo.DBColFunc, o.ObjID
	case *FnEventDesc:
		return gmgo.DBColEvents, o.ObjID
	case *RouterDesc:
		return gmgo.DBColRouters, o.ObjID
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
	if !dbMayRemove(ctx) {
		return dbNotAllowed
	}

	col, q := objq(ctx, o)
	return col.Remove(q)
}

func dbInsert(ctx context.Context, o interface{}) error {
	if !dbMayInsert(ctx) {
		return dbNotAllowed
	}

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
	if !dbMayUpdate(ctx) {
		return dbNotAllowed
	}

	col, q := objq(ctx, o)
	for k, v := range q2 {
		q[k] = v
	}
	return col.Update(q, bson.M{"$set": u})
}

func dbUpdatePart(ctx context.Context, o interface{}, u bson.M) error {
	if !dbMayUpdate(ctx) {
		return dbNotAllowed
	}

	col, q := objq(ctx, o)
	return col.Update(q, bson.M{"$set": u})
}

func dbUpdateAll(ctx context.Context, o interface{}) error {
	if !dbMayUpdate(ctx) {
		return dbNotAllowed
	}

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
	Cookie		string		`bson:"cookie"`
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
		if !gx.Admin() && rd.SwoId.Tennant != gx.Tenant {
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
	c := gctx(ctx).S.DB(gmgo.DBTenantDB).C(gmgo.DBColLimits)
	var v swyapi.UserLimits
	err := maybe(c.Find(bson.M{"uid":tenant}).One(&v))
	return &v, err
}

func dbMwareCount(ctx context.Context) (map[string]int, error) {
	var counts []struct {
		Id	string	`bson:"_id"`
		Count	int	`bson:"count"`
	}

	err := dbCol(ctx, gmgo.DBColMware).Pipe([]bson.M{
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

func dbAccCount(ctx context.Context) (map[string]int, error) {
	var counts []struct {
		Id	string	`bson:"_id"`
		Count	int	`bson:"count"`
	}

	err := dbCol(ctx, gmgo.DBColAccounts).Pipe([]bson.M{
			bson.M{"$group": bson.M{
				"_id":"$type",
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
	return dbCol(ctx, gmgo.DBColFunc).Count()
}

func dbFuncCountTen(ctx context.Context) (int, error) {
	return dbCol(ctx, gmgo.DBColFunc).Find(bson.M{"tenant": gctx(ctx).Tenant}).Count()
}

func dbMwareCountTen(ctx context.Context, mt string) (int, error) {
	return dbCol(ctx, gmgo.DBColFunc).Find(bson.M{"tenant": gctx(ctx).Tenant, "mwaretype": mt}).Count()
}

func dbFuncUpdate(ctx context.Context, q, ch bson.M) (error) {
	if !dbMayUpdate(ctx) {
		return dbNotAllowed
	}

	return dbCol(ctx, gmgo.DBColFunc).Update(q, ch)
}

func dbRouterCount(ctx context.Context) (int, error) {
	return dbCol(ctx, gmgo.DBColRouters).Count()
}

func dbRepoCount(ctx context.Context) (int, error) {
	return dbCol(ctx, gmgo.DBColRepos).Count()
}

func dbRepoCountTen(ctx context.Context) (int, error) {
	return dbCol(ctx, gmgo.DBColRepos).Find(bson.M{"tenant": gctx(ctx).Tenant}).Count()
}


func dbDeployCount(ctx context.Context) (int, error) {
	return dbCol(ctx, gmgo.DBColDeploy).Count()
}

func dbTenStatsGet(ctx context.Context, tenant string, st *TenStats) error {
	return maybe(dbCol(ctx, gmgo.DBColTenStats).Find(bson.M{"tenant": tenant}).One(st))
}

func dbTenStatsGetArch(ctx context.Context, tenant string, nr int) ([]TenStats, error) {
	var ret []TenStats
	err := maybe(dbCol(ctx, gmgo.DBColTenStatsA).Find(bson.M{"tenant": tenant}).Sort("-till").Limit(nr).All(&ret))
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
	if !dbMayUpdate(ctx) {
		return dbNotAllowed
	}

	_, err := dbCol(ctx, gmgo.DBColTenStats).Upsert(bson.M{"tenant": tenant}, bson.M{
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

func dbTCacheFind(ctx context.Context) (*TenantCache, error) {
	cookie := xh.Cookify(gctx(ctx).Tenant)
	var pc TenantCache
	err := dbCol(ctx, gmgo.DBColTCache).Find(bson.M{"cookie": cookie}).One(&pc)
	if err != nil {
		return nil, err
	}

	return &pc, nil
}

func dbTCacheUpdatePackages(ctx context.Context, lang string, pkl []*swyapi.Package) {
	if !dbMayUpdate(ctx) {
		return
	}

	ten := gctx(ctx).Tenant
	cookie := xh.Cookify(ten)
	dbCol(ctx, gmgo.DBColTCache).Upsert(bson.M{"cookie": cookie},
			bson.M{"$set": bson.M{
				"tenant": ten,
				"packages." + lang: pkl,
			}})
}

func dbTCacheUpdatePkgDU(ctx context.Context, ten, lang string, du uint64) error {
	if !dbMayUpdate(ctx) {
		return dbNotAllowed
	}

	cookie := xh.Cookify(ten)
	_, err := dbCol(ctx, gmgo.DBColTCache).Upsert(bson.M{"cookie": cookie},
			bson.M{"$set": bson.M{
				"tenant": ten,
				"pkg_stats." + lang + ".du": du,
			}})
	return err
}

func dbTCacheFlushList(ctx context.Context, lang string) {
	if !dbMayUpdate(ctx) {
		return
	}

	ten := gctx(ctx).Tenant
	cookie := xh.Cookify(ten)
	dbCol(ctx, gmgo.DBColTCache).Update(bson.M{"cookie": cookie}, bson.M{"$unset": bson.M{"packages." + lang: ""}})
}

func dbTCacheFlushAll(ctx context.Context) {
	if !dbMayRemove(ctx) {
		return
	}

	dbCol(ctx, gmgo.DBColTCache).RemoveAll(bson.M{})
}

func dbFnStatsGet(ctx context.Context, cookie string, st *FnStats) error {
	return maybe(dbCol(ctx, gmgo.DBColFnStats).Find(bson.M{"cookie": cookie}).One(st))
}

func dbFnStatsGetArch(ctx context.Context, id string, nr int) ([]FnStats, error) {
	var ret []FnStats
	err := maybe(dbCol(ctx, gmgo.DBColFnStatsA).Find(bson.M{"cookie": id}).Sort("-till").Limit(nr).All(&ret))
	return ret, err
}

func dbFnStatsUpdate(ctx context.Context, cookie string, delta *gmgo.FnStatValues, lastCall time.Time) error {
	if !dbMayUpdate(ctx) {
		return dbNotAllowed
	}

	_, err := dbCol(ctx, gmgo.DBColFnStats).Upsert(bson.M{"cookie": cookie}, bson.M{
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
	if !dbMayRemove(ctx) {
		return dbNotAllowed
	}

	if st.Called != 0 {
		n := time.Now()
		st.Dropped = &n
		st.Till = &n

		err := dbCol(ctx, gmgo.DBColFnStatsA).Insert(st)
		if err != nil {
			return err
		}
	}

	return maybe(dbCol(ctx, gmgo.DBColFnStats).Remove(bson.M{"cookie": cookie}))
}

func logSaveResult(ctx context.Context, cookie, event, stdout, stderr string) {
	c := dbCol(ctx, gmgo.DBColLogs)
	tm := time.Now()

	if stdout != "" {
		c.Insert(DBLogRec{
			Cookie:		cookie,
			Event:		"stdout." + event,
			Time:		tm,
			Text:		stdout,
		})
	}

	if stderr != "" {
		c.Insert(DBLogRec{
			Cookie:		cookie,
			Event:		"stderr." + event,
			Time:		tm,
			Text:		stderr,
		})
	}
}

func logSaveEvent(ctx context.Context, cookie, text string) {
	if !dbMayUpdate(ctx) {
		return
	}

	dbCol(ctx, gmgo.DBColLogs).Insert(DBLogRec{
		Cookie:		cookie,
		Event:		"event",
		Time:		time.Now(),
		Text:		text,
	})
}

func logGetFor(ctx context.Context, cookie string, since *time.Time) ([]DBLogRec, error) {
	var logs []DBLogRec
	q := bson.M{"cookie": cookie}
	if since != nil {
		q["ts"] = bson.M{"$gt": since}
	}
	err := dbCol(ctx, gmgo.DBColLogs).Find(q).Sort("ts").All(&logs)
	return logs, err
}

func logRemove(ctx context.Context, fn *FunctionDesc) error {
	if !dbMayRemove(ctx) {
		return dbNotAllowed
	}

	_, err := dbCol(ctx, gmgo.DBColLogs).RemoveAll(bson.M{"cookie": fn.Cookie})
	return maybe(err)
}

func dbProjectListAll(ctx context.Context, ten string) (fn []string, mw []string, err error) {
	err = dbCol(ctx, gmgo.DBColFunc).Find(bson.M{"tennant": ten}).Distinct("project", &fn)
	if err != nil {
		return
	}

	err = dbCol(ctx, gmgo.DBColMware).Find(bson.M{"tennant": ten}).Distinct("project", &mw)
	return
}

func dbConnect() error {
	var err error

	dbc := xh.ParseXCreds(conf.DB)
	pwd, err := gateSecrets.Get(dbc.Pass)
	if err != nil {
		glog.Errorf("No DB password found in secrets")
		return err
	}

	info := mgo.DialInfo{
		Addrs:		[]string{dbc.Addr()},
		Database:	gmgo.DBStateDB,
		Timeout:	60 * time.Second,
		Username:	dbc.User,
		Password:	pwd,
	}

	session, err = mgo.DialWithInfo(&info);
	if err != nil {
		glog.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB, gmgo.DBStateDB, err.Error())
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
	err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColFunc).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for functions: %s", err.Error())
	}
	err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColMware).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for mware: %s", err.Error())
	}
	err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColAccounts).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for mware: %s", err.Error())
	}
	err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColRouters).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for mware: %s", err.Error())
	}
	err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColTCache).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for ten cache: %s", err.Error())
	}

	index.Unique = false
	index.DropDups = false

	index.Key = []string{"src.repo"}
	err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColFunc).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No src.repo index for functions: %s", err.Error())
	}

	index.Key = []string{"name"}
	err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColRepos).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No name index for repos: %s", err.Error())
	}

	index.Key = []string{"key"}
	err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColEvents).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No name index for repos: %s", err.Error())
	}

	_, err = dbs.DB(gmgo.DBStateDB).C(gmgo.DBColLogs).UpdateAll(bson.M{}, bson.M{"$rename":bson.M{"fnid":"cookie"}})
	if err != nil {
		return fmt.Errorf("Cannot update logs field fnid to cookie")
	}

	return nil

}

func dbDisconnect() {
	session.Close()
	session = nil
}
