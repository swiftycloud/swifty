package main

import (
	"time"
	"fmt"
	"context"
	"reflect"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"../common"
	"../apis/apps"
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
	DBColBalancerRS = "BalancerRS"
	DBColDeploy	= "Deploy"
	DBColLimits	= "Limits"
	DBColEvents	= "Events"
	DBColRepos	= "Repos"
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
}

func dbColl(object interface{}) (string) {
	typ := reflect.TypeOf(object)
	if name, ok := dbColMap[typ]; ok {
		return name
	}
	glog.Fatalf("Unmapped object %s", typ.String())
	return ""
}

func dbRemove(ctx context.Context, o interface{}, q bson.M) error {
	return gctx(ctx).S.DB(DBStateDB).C(dbColl(o)).Remove(q)
}

func dbRemoveId(ctx context.Context, o interface{}, id bson.ObjectId) error {
	return dbRemove(ctx, o, bson.M{"_id": id})
}

func dbInsert(ctx context.Context, o interface{}) error {
	return gctx(ctx).S.DB(DBStateDB).C(dbColl(o)).Insert(o)
}

func dbFindAll(ctx context.Context, q interface{}, o interface{}) error {
	return gctx(ctx).S.DB(DBStateDB).C(dbColl(o)).Find(q).All(o)
}

func dbFindAllCommon(ctx context.Context, q bson.D, o interface{}) error {
	q = append(q, bson.DocElem{"tennant", gctx(ctx).Tenant})
	return dbFindAll(ctx, q, o)
}

type DBLogRec struct {
	FnId		string		`bson:"fnid"`
	Event		string		`bson:"event"`
	Time		time.Time	`bson:"ts"`
	Text		string		`bson:"text"`
}

var session *mgo.Session

func dbNF(err error) bool {
	return err == mgo.ErrNotFound
}

func ctxObjId(ctx context.Context, id string) bson.M {
	return bson.M {
		"tennant": gctx(ctx).Tenant,
		"_id": bson.ObjectIdHex(id),
	}
}

func dbTenantGetLimits(ctx context.Context, tenant string) (*swyapi.UserLimits, error) {
	c := gctx(ctx).S.DB(DBTenantDB).C(DBColLimits)
	var v swyapi.UserLimits
	err := c.Find(bson.M{"id":tenant}).One(&v)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return &v, err
}

func dbMwareCount(ctx context.Context) (map[string]int, error) {
	var counts []struct {
		Id	string	`bson:"_id"`
		Count	int	`bson:"count"`
	}

	c := gctx(ctx).S.DB(DBStateDB).C(DBColMware)
	err := c.Pipe([]bson.M{
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

func dbMwareUpdateAdded(ctx context.Context, desc *MwareDesc) error {
	desc.State = swy.DBMwareStateRdy
	c := gctx(ctx).S.DB(DBStateDB).C(DBColMware)
	return c.Update(bson.M{"cookie": desc.Cookie},
		bson.M{"$set": bson.M{
				"client":	desc.Client,
				"secret":	desc.Secret,
				"namespace":	desc.Namespace,
				"state":	desc.State,
			}})
}

func dbMwareTerminate(ctx context.Context, mwd *MwareDesc) error {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColMware)
	return c.Update(
		bson.M{"cookie": mwd.Cookie, "state": bson.M{"$in": []int{swy.DBMwareStateRdy, swy.DBMwareStateStl}}},
		bson.M{"$set": bson.M{"state": swy.DBMwareStateTrm, }})
}

func dbMwareSetStalled(ctx context.Context, mwd *MwareDesc) error {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColMware)
	return c.Update( bson.M{"cookie": mwd.Cookie, },
		bson.M{"$set": bson.M{"state": swy.DBMwareStateStl, }})
}

func dbMwareGetOne(ctx context.Context, q bson.M) (*MwareDesc, error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColMware)
	v := MwareDesc{}
	err := c.Find(q).One(&v)
	return &v, err
}

func dbMwareGetItem(ctx context.Context, id *SwoId) (*MwareDesc, error) {
	return dbMwareGetOne(ctx, bson.M{"cookie": id.Cookie()})
}

func dbMwareGetReady(ctx context.Context, id *SwoId) (*MwareDesc, error) {
	return dbMwareGetOne(ctx, bson.M{"cookie": id.Cookie(), "state": swy.DBMwareStateRdy})
}

func dbFuncCount(ctx context.Context) (int, error) {
	return gctx(ctx).S.DB(DBStateDB).C(DBColFunc).Count()
}

func dbFuncCountProj(ctx context.Context, id *SwoId) (int, error) {
	return gctx(ctx).S.DB(DBStateDB).C(DBColFunc).Find(bson.M{"tenant": id.Tennant, "project": id.Project}).Count()
}

func dbFuncFindOne(ctx context.Context, q bson.M) (*FunctionDesc, error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColFunc)
	var v FunctionDesc
	err := c.Find(q).One(&v)
	return &v, err
}

func dbFuncFindAll(ctx context.Context, q interface{}) (vs []*FunctionDesc, err error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColFunc)
	err = c.Find(q).All(&vs)
	return
}

func dbFuncUpdate(ctx context.Context, q, ch bson.M) (error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColFunc)
	return c.Update(q, ch)
}

func dbFuncFind(ctx context.Context, id *SwoId) (*FunctionDesc, error) {
	return dbFuncFindOne(ctx, bson.M{"cookie": id.Cookie()})
}

func dbFuncFindByCookie(ctx context.Context, cookie string) (*FunctionDesc, error) {
	fn, err := dbFuncFindOne(ctx, bson.M{"cookie": cookie})
	if err != nil {
		fn = nil
		if err == mgo.ErrNotFound {
			err = nil
		}
	}
	return fn, err
}

func dbFuncList(ctx context.Context) ([]*FunctionDesc, error) {
	return dbFuncFindAll(ctx, bson.M{})
}

func dbFuncListMwEvent(ctx context.Context, id *SwoId, rq bson.M) ([]*FunctionDesc, error) {
	rq["tennant"] = id.Tennant
	rq["project"] = id.Project

	return dbFuncFindAll(ctx, rq)
}

func dbFuncListWithEvents(ctx context.Context) ([]*FunctionDesc, error) {
	return dbFuncFindAll(ctx, bson.M{"event.source": bson.M{"$ne": ""}})
}

func dbFuncSetStateCond(ctx context.Context, id *SwoId, state, ostate int) error {
	return dbFuncUpdate(ctx,
		bson.M{"cookie": id.Cookie(), "state": ostate},
		bson.M{"$set": bson.M{"state": state}})
}

func dbFuncSetState(ctx context.Context, fn *FunctionDesc, state int) error {
	fn.State = state
	err := dbFuncUpdate(ctx,
		bson.M{"cookie": fn.Cookie, "state": bson.M{"$ne": state}},
		bson.M{"$set": bson.M{"state": state}})
	if err != nil {
		ctxlog(ctx).Errorf("dbFuncSetState: Can't change function %s state: %s",
				fn.Name, err.Error())
	}

	return err
}

func dbFuncUpdateAdded(ctx context.Context, fn *FunctionDesc) error {
	return dbFuncUpdate(ctx,
		bson.M{"cookie": fn.Cookie},
		bson.M{"$set": bson.M{
				"src": &fn.Src,
				"state": fn.State,
			}})
}

func dbFuncUpdatePulled(ctx context.Context, fn *FunctionDesc, update bson.M, olds int) error {
	return dbFuncUpdate(ctx,
		bson.M{"cookie": fn.Cookie, "state": olds},
		bson.M{"$set": update })
}

func dbFuncUpdateOne(ctx context.Context, fn *FunctionDesc, update bson.M) error {
	return dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID}, bson.M{"$set": update })
}

func dbTenStatsGet(ctx context.Context, tenant string, st *TenStats) error {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColTenStats)
	err := c.Find(bson.M{"tenant": tenant}).One(st)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func dbTenStatsGetArch(ctx context.Context, tenant string, nr int) ([]TenStats, error) {
	var ret []TenStats
	c := gctx(ctx).S.DB(DBStateDB).C(DBColTenStatsA)
	err := c.Find(bson.M{"tenant": tenant}).Sort("-till").Limit(nr).All(&ret)
	if err == mgo.ErrNotFound {
		err = nil
	}
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

func dbTenStatsUpdate(ctx context.Context, tenant string, delta *TenStatValues) error {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColTenStats)
	_, err := c.Upsert(bson.M{"tenant": tenant}, bson.M{
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
	c := gctx(ctx).S.DB(DBStateDB).C(DBColFnStats)
	err := c.Find(bson.M{"cookie": cookie}).One(st)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func dbFnStatsGetArch(ctx context.Context, id string, nr int) ([]FnStats, error) {
	var ret []FnStats
	c := gctx(ctx).S.DB(DBStateDB).C(DBColFnStatsA)
	err := c.Find(bson.M{"cookie": id}).Sort("-till").Limit(nr).All(&ret)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return ret, err
}

func dbFnStatsUpdate(ctx context.Context, cookie string, delta *FnStatValues, lastCall time.Time) error {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColFnStats)
	_, err := c.Upsert(bson.M{"cookie": cookie}, bson.M{
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

		c := gctx(ctx).S.DB(DBStateDB).C(DBColFnStatsA)
		err := c.Insert(st)
		if err != nil {
			return err
		}
	}

	c := gctx(ctx).S.DB(DBStateDB).C(DBColFnStats)
	err := c.Remove(bson.M{"cookie": cookie})
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func logSaveResult(ctx context.Context, fnCookie, event, stdout, stderr string) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColLogs)
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
	c := gctx(ctx).S.DB(DBStateDB).C(DBColLogs)
	c.Insert(DBLogRec{
		FnId:		fnid,
		Event:		"event",
		Time:		time.Now(),
		Text:		text,
	})
}

func logGetFor(ctx context.Context, id *SwoId, since *time.Time) ([]DBLogRec, error) {
	var logs []DBLogRec
	c := gctx(ctx).S.DB(DBStateDB).C(DBColLogs)
	q := bson.M{"fnid": id.Cookie()}
	if since != nil {
		q["ts"] = bson.M{"$gt": since}
	}
	err := c.Find(q).Sort("ts").All(&logs)
	return logs, err
}

func logRemove(ctx context.Context, fn *FunctionDesc) error {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColLogs)
	_, err := c.RemoveAll(bson.M{"fnid": fn.Cookie})
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func dbBalancerRSListVersions(ctx context.Context, cookie string) ([]string, error) {
	var fv []string
	c := gctx(ctx).S.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Find(bson.M{"fnid": cookie }).Distinct("fnversion", &fv)
	return fv, err
}

func dbBalancerPodAdd(ctx context.Context, pod *k8sPod) error {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Insert(bson.M{
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
	c := gctx(ctx).S.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Update(bson.M{"uid": pod.UID}, bson.M{"$set": bson.M {
			"fnid":		fnId,
			"fnversion":	pod.Version,
		}})
	if err != nil && err != mgo.ErrNotFound {
		return fmt.Errorf("add: %s", err.Error())
	}

	return nil
}

func dbBalancerPodDel(ctx context.Context, pod *k8sPod) (error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Remove(bson.M{ "uid":	pod.UID, })
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}

		return fmt.Errorf("del: %s", err.Error())
	}

	return nil
}

func dbBalancerPodDelStuck(ctx context.Context) (error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColBalancerRS)
	_, err := c.RemoveAll(bson.M{ "fnid": bson.M{"$exists": false}})
	if err == mgo.ErrNotFound {
		err = nil
	}

	return err
}

func dbBalancerPodDelAll(ctx context.Context, fnid string) (error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColBalancerRS)
	_, err := c.RemoveAll(bson.M{ "fnid": fnid })
	if err == mgo.ErrNotFound {
		err = nil
	}

	return err
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

func dbBalancerGetConnsByCookie(ctx context.Context, cookie string) ([]podConn, error) {
	var v []balancerEntry

	c := gctx(ctx).S.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Find(bson.M{
			"fnid": cookie,
		}).All(&v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	var ret []podConn
	for _, b := range(v) {
		ret = append(ret, podConn{Addr: b.WdogAddr, Port: b.WdogPort, Host: b.Host})
	}

	return ret, nil
}

func dbBalancerGetConnExact(ctx context.Context, fnid, version string) (*podConn, error) {
	var v balancerEntry

	c := gctx(ctx).S.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Find(bson.M{
			"fnid":		fnid,
			"fnversion":	version,
		}).One(&v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &podConn{Addr: v.WdogAddr, Port: v.WdogPort}, nil
}

func dbProjectListAll(ctx context.Context, ten string) (fn []string, mw []string, err error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColFunc)
	err = c.Find(bson.M{"tennant": ten}).Distinct("project", &fn)
	if err != nil {
		return
	}

	c = gctx(ctx).S.DB(DBStateDB).C(DBColMware)
	err = c.Find(bson.M{"tennant": ten}).Distinct("project", &mw)
	return
}

func dbDeployGet(ctx context.Context, q bson.M) (*DeployDesc, error) {
	var dep DeployDesc
	err := gctx(ctx).S.DB(DBStateDB).C(DBColDeploy).Find(q).One(&dep)
	return &dep, err
}

func dbDeployList(ctx context.Context, q bson.M) (deps []DeployDesc, err error) {
	err = gctx(ctx).S.DB(DBStateDB).C(DBColDeploy).Find(q).All(&deps)
	return
}

func dbDeployStateUpdate(ctx context.Context, dep *DeployDesc, state int) error {
	dep.State = state
	return gctx(ctx).S.DB(DBStateDB).C(DBColDeploy).Update(bson.M{"cookie": dep.Cookie},
			bson.M{"$set": bson.M{"state": state}})
}

func dbListFnEvents(ctx context.Context, fnid string) ([]*FnEventDesc, error) {
	var ret []*FnEventDesc
	err := gctx(ctx).S.DB(DBStateDB).C(DBColEvents).Find(bson.M{"fnid": fnid}).All(&ret)
	return ret, err
}

func dbListEvents(ctx context.Context, q bson.M) ([]FnEventDesc, error) {
	var ret []FnEventDesc
	err := gctx(ctx).S.DB(DBStateDB).C(DBColEvents).Find(q).All(&ret)
	return ret, err
}

func dbFindEvent(ctx context.Context, id string) (*FnEventDesc, error) {
	var ed FnEventDesc
	err := gctx(ctx).S.DB(DBStateDB).C(DBColEvents).Find(bson.M{"_id": bson.ObjectIdHex(id)}).One(&ed)
	return &ed, err
}

func dbFuncEventByName(ctx context.Context, fn *FunctionDesc, name string) (*FnEventDesc, error) {
	var ed FnEventDesc
	err := gctx(ctx).S.DB(DBStateDB).C(DBColEvents).Find(bson.M{"fnid": fn.Cookie, "name": name}).One(&ed)
	return &ed, err
}

func dbUpdateEvent(ctx context.Context, ed *FnEventDesc) error {
	return gctx(ctx).S.DB(DBStateDB).C(DBColEvents).Update(bson.M{"_id": ed.ObjID}, ed)
}

func dbRepoGetOne(ctx context.Context, q bson.M) (*RepoDesc, error) {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColRepos)
	v := RepoDesc{}
	err := c.Find(q).One(&v)
	return &v, err
}

func dbRepoDeactivate(ctx context.Context, rd *RepoDesc) error {
	c := gctx(ctx).S.DB(DBStateDB).C(DBColRepos)
	return c.Update(
		bson.M{"_id": rd.ObjID},
		bson.M{"$set": bson.M{"state": swy.DBRepoStateRem, }})
}

func LogsCleanerInit(ctx context.Context, conf *YAMLConf) error {
	if conf.LogsKeepDays > 0 {
		ctxlog(ctx).Debugf("Start logs cleaner (%d days old)", conf.LogsKeepDays)
		go func() {
			for {
				time.Sleep(LogsCleanPeriod)

				ctx, done := mkContext("::logsgc")
				c := gctx(ctx).S.DB(DBStateDB).C(DBColLogs)
				dur := time.Now().AddDate(0, 0, -conf.LogsKeepDays)
				c.RemoveAll(bson.M{"ts": bson.M{"$lt": dur }})
				ctxlog(ctx).Debugf("Cleaned logs < %s", dur.String())
				done(ctx)
			}
		}()
	}
	return nil
}

func dbConnect(conf *YAMLConf) error {
	var err error

	dbc := swy.ParseXCreds(conf.DB)

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

	index.Key = []string{"uid"}
	err = dbs.DB(DBStateDB).C(DBColBalancerRS).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No uid index for balancerrs: %s", err.Error())
	}

	return nil

}

func dbDisconnect() {
	session.Close()
	session = nil
}
