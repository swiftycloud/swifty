package main

import (
	"time"
	"fmt"
	"context"

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
)

type DBLogRec struct {
	FnId		string		`bson:"fnid"`
	Event		string		`bson:"event"`
	Time		time.Time	`bson:"ts"`
	Text		string		`bson:"text"`
}

var dbSession *mgo.Session

func dbTenantGetLimits(tenant string) (*swyapi.UserLimits, error) {
	c := dbSession.DB(DBTenantDB).C(DBColLimits)
	var v swyapi.UserLimits
	err := c.Find(bson.M{"id":tenant}).One(&v)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return &v, err
}

func dbMwareCount() (map[string]int, error) {
	var counts []struct {
		Id	string	`bson:"_id"`
		Count	int	`bson:"count"`
	}

	c := dbSession.DB(DBStateDB).C(DBColMware)
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

func dbMwareAdd(desc *MwareDesc) error {
	c := dbSession.DB(DBStateDB).C(DBColMware)
	return c.Insert(desc)
}

func dbMwareUpdateAdded(desc *MwareDesc) error {
	desc.State = swy.DBMwareStateRdy
	c := dbSession.DB(DBStateDB).C(DBColMware)
	return c.Update(bson.M{"cookie": desc.Cookie},
		bson.M{"$set": bson.M{
				"client":	desc.Client,
				"secret":	desc.Secret,
				"namespace":	desc.Namespace,
				"state":	desc.State,
			}})
}

func dbMwareTerminate(mwd *MwareDesc) error {
	c := dbSession.DB(DBStateDB).C(DBColMware)
	return c.Update(
		bson.M{"cookie": mwd.Cookie, "state": bson.M{"$in": []int{swy.DBMwareStateRdy, swy.DBMwareStateStl}}},
		bson.M{"$set": bson.M{"state": swy.DBMwareStateTrm, }})
}

func dbMwareRemove(mwd *MwareDesc) error {
	c := dbSession.DB(DBStateDB).C(DBColMware)
	return c.Remove(bson.M{"cookie": mwd.Cookie})
}

func dbMwareSetStalled(mwd *MwareDesc) error {
	c := dbSession.DB(DBStateDB).C(DBColMware)
	return c.Update( bson.M{"cookie": mwd.Cookie, },
		bson.M{"$set": bson.M{"state": swy.DBMwareStateStl, }})
}

func dbMwareGetOne(q bson.M) (MwareDesc, error) {
	c := dbSession.DB(DBStateDB).C(DBColMware)
	v := MwareDesc{}
	err := c.Find(q).One(&v)
	return v, err
}

func dbMwareGetItem(id *SwoId) (MwareDesc, error) {
	return dbMwareGetOne(bson.M{"cookie": id.Cookie()})
}

func dbMwareGetReady(id *SwoId) (MwareDesc, error) {
	return dbMwareGetOne(bson.M{"cookie": id.Cookie(), "state": swy.DBMwareStateRdy})
}

func dbMwareListProj(id *SwoId, mwtyp string) ([]MwareDesc, error) {
	var recs []MwareDesc
	c := dbSession.DB(DBStateDB).C(DBColMware)
	lk := bson.M{"tennant": id.Tennant, "project": id.Project}
	if mwtyp != "" {
		lk["mwaretype"] = mwtyp
	}
	err := c.Find(lk).All(&recs)
	return recs, err
}

func dbFuncCount() (int, error) {
	return dbSession.DB(DBStateDB).C(DBColFunc).Count()
}

func dbFuncCountProj(id *SwoId) (int, error) {
	return dbSession.DB(DBStateDB).C(DBColFunc).Find(bson.M{"tenant": id.Tennant, "project": id.Project}).Count()
}

func dbFuncFindOne(q bson.M) (*FunctionDesc, error) {
	c := dbSession.DB(DBStateDB).C(DBColFunc)
	var v FunctionDesc
	err := c.Find(q).One(&v)
	return &v, err
}

func dbFuncFindAll(q bson.M) (vs []FunctionDesc, err error) {
	c := dbSession.DB(DBStateDB).C(DBColFunc)
	err = c.Find(q).All(&vs)
	return
}

func dbFuncUpdate(q, ch bson.M) (error) {
	c := dbSession.DB(DBStateDB).C(DBColFunc)
	return c.Update(q, ch)
}

func dbFuncFind(id *SwoId) (*FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"cookie": id.Cookie()})
}

func dbFuncFindByCookie(cookie string) (*FunctionDesc, error) {
	fn, err := dbFuncFindOne(bson.M{"cookie": cookie})
	if err != nil {
		fn = nil
		if err == mgo.ErrNotFound {
			err = nil
		}
	}
	return fn, err
}

func dbFuncFindStates(id *SwoId, states []int) (*FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"cookie": id.Cookie(), "state": bson.M{"$in": states}})
}

func dbFuncList() ([]FunctionDesc, error) {
	return dbFuncFindAll(bson.M{})
}

func dbFuncListProj(id *SwoId) ([]FunctionDesc, error) {
	return dbFuncFindAll(bson.M{"tennant": id.Tennant, "project": id.Project})
}

func dbFuncListMwEvent(id *SwoId, rq bson.M) ([]FunctionDesc, error) {
	rq["tennant"] = id.Tennant
	rq["project"] = id.Project

	return dbFuncFindAll(rq)
}

func dbFuncListWithEvents() ([]FunctionDesc, error) {
	return dbFuncFindAll(bson.M{"event.source": bson.M{"$ne": ""}})
}

func dbFuncSetStateCond(id *SwoId, state, ostate int) error {
	return dbFuncUpdate(
		bson.M{"cookie": id.Cookie(), "state": ostate},
		bson.M{"$set": bson.M{"state": state}})
}

func dbFuncSetState(ctx context.Context, fn *FunctionDesc, state int) error {
	fn.State = state
	err := dbFuncUpdate(
		bson.M{"cookie": fn.Cookie, "state": bson.M{"$ne": state}},
		bson.M{"$set": bson.M{"state": state}})
	if err != nil {
		ctxlog(ctx).Errorf("dbFuncSetState: Can't change function %s state: %s",
				fn.Name, err.Error())
	}

	return err
}

func dbFuncUpdateAdded(fn *FunctionDesc) error {
	return dbFuncUpdate(
		bson.M{"cookie": fn.Cookie},
		bson.M{"$set": bson.M{
				"src.version": fn.Src.Version,
				"state": fn.State,
			}})
}

func dbFuncUpdatePulled(fn *FunctionDesc, update bson.M, olds int) error {
	return dbFuncUpdate(
		bson.M{"cookie": fn.Cookie, "state": olds},
		bson.M{"$set": update })
}

func dbFuncAdd(desc *FunctionDesc) error {
	c := dbSession.DB(DBStateDB).C(DBColFunc)
	return c.Insert(desc)
}

func dbFuncRemove(fn *FunctionDesc) error {
	c := dbSession.DB(DBStateDB).C(DBColFunc)
	return c.Remove(bson.M{"cookie": fn.Cookie});
}

func dbTenStatsGet(tenant string, st *TenStats) error {
	c := dbSession.DB(DBStateDB).C(DBColTenStats)
	err := c.Find(bson.M{"tenant": tenant}).One(st)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func dbTenStatsGetArch(tenant string, nr int) ([]TenStats, error) {
	var ret []TenStats
	c := dbSession.DB(DBStateDB).C(DBColTenStatsA)
	err := c.Find(bson.M{"tenant": tenant}).Sort("-till").Limit(nr).All(&ret)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return ret, err
}

func dbTenStatsGetLatestArch(tenant string) (*TenStats, error) {
	var ret TenStats
	a, err := dbTenStatsGetArch(tenant, 1)
	if len(a) != 0 {
		ret = a[0]
	}
	return &ret, err
}

func dbTenStatsUpdate(st *TenStats) {
	c := dbSession.DB(DBStateDB).C(DBColTenStats)
	_, err := c.Upsert(bson.M{"tenant": st.Tenant}, st)
	if err != nil {
		glog.Errorf("Error upserting tenant stats: %s", err.Error())
	}
}

func dbFnStatsGet(cookie string, st *FnStats) error {
	c := dbSession.DB(DBStateDB).C(DBColFnStats)
	err := c.Find(bson.M{"cookie": cookie}).One(st)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func dbFnStatsGetArch(id string, nr int) ([]FnStats, error) {
	var ret []FnStats
	c := dbSession.DB(DBStateDB).C(DBColFnStatsA)
	err := c.Find(bson.M{"cookie": id}).Sort("-till").Limit(nr).All(&ret)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return ret, err
}

func dbFnStatsUpdate(st *FnStats) {
	c := dbSession.DB(DBStateDB).C(DBColFnStats)
	_, err := c.Upsert(bson.M{"cookie": st.Cookie}, st)
	if err != nil {
		glog.Errorf("Error upserting fn stats: %s", err.Error())
	}
}

func dbFnStatsDrop(cookie string, st *FnStats) error {
	if st.Called != 0 {
		n := time.Now()
		st.Dropped = &n
		st.Till = &n

		c := dbSession.DB(DBStateDB).C(DBColFnStatsA)
		err := c.Insert(st)
		if err != nil {
			return err
		}
	}

	c := dbSession.DB(DBStateDB).C(DBColFnStats)
	err := c.Remove(bson.M{"cookie": cookie})
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func logSaveResult(fnCookie, event, stdout, stderr string) {
	c := dbSession.DB(DBStateDB).C(DBColLogs)
	text := fmt.Sprintf("out: [%s], err: [%s]", stdout, stderr)
	c.Insert(DBLogRec{
		FnId:		fnCookie,
		Event:		event,
		Time:		time.Now(),
		Text:		text,
	})
}

func logSaveEvent(fn *FunctionDesc, event, text string) {
	c := dbSession.DB(DBStateDB).C(DBColLogs)
	c.Insert(DBLogRec{
		FnId:		fn.Cookie,
		Event:		event,
		Time:		time.Now(),
		Text:		text,
	})
}

func logGetFor(id *SwoId) ([]DBLogRec, error) {
	var logs []DBLogRec
	c := dbSession.DB(DBStateDB).C(DBColLogs)
	err := c.Find(bson.M{"fnid": id.Cookie()}).All(&logs)
	return logs, err
}

func logGetCalls(id *SwoId) (int, error) {
	c := dbSession.DB(DBStateDB).C(DBColLogs)
	return c.Find(bson.M{"fnid": id.Cookie(), "event": "run"}).Count()
}

func logRemove(fn *FunctionDesc) error {
	c := dbSession.DB(DBStateDB).C(DBColLogs)
	_, err := c.RemoveAll(bson.M{"fnid": fn.Cookie})
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func dbBalancerRSListVersions(cookie string) ([]string, error) {
	var fv []string
	c := dbSession.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Find(bson.M{"fnid": cookie }).Distinct("fnversion", &fv)
	return fv, err
}

func dbBalancerPodAdd(pod *k8sPod) error {
	c := dbSession.DB(DBStateDB).C(DBColBalancerRS)
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

func dbBalancerPodUpd(fnId string, pod *k8sPod) error {
	c := dbSession.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Update(bson.M{"uid": pod.UID}, bson.M{"$set": bson.M {
			"fnid":		fnId,
			"fnversion":	pod.Version,
		}})
	if err != nil && err != mgo.ErrNotFound {
		return fmt.Errorf("add: %s", err.Error())
	}

	return nil
}

func dbBalancerPodDel(pod *k8sPod) (error) {
	c := dbSession.DB(DBStateDB).C(DBColBalancerRS)
	err := c.Remove(bson.M{ "uid":	pod.UID, })
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}

		return fmt.Errorf("del: %s", err.Error())
	}

	return nil
}

func dbBalancerPodDelAll(fnid string) (error) {
	c := dbSession.DB(DBStateDB).C(DBColBalancerRS)
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

func dbBalancerGetConnsByCookie(cookie string) ([]podConn, error) {
	var v []balancerEntry

	c := dbSession.DB(DBStateDB).C(DBColBalancerRS)
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

func dbBalancerGetConnExact(fnid, version string) (*podConn, error) {
	var v balancerEntry

	c := dbSession.DB(DBStateDB).C(DBColBalancerRS)
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

func dbProjectListAll(ten string) (fn []string, mw []string, err error) {
	c := dbSession.DB(DBStateDB).C(DBColFunc)
	err = c.Find(bson.M{"tennant": ten}).Distinct("project", &fn)
	if err != nil {
		return
	}

	c = dbSession.DB(DBStateDB).C(DBColMware)
	err = c.Find(bson.M{"tennant": ten}).Distinct("project", &mw)
	return
}

func dbDeployAdd(dep *DeployDesc) error {
	return dbSession.DB(DBStateDB).C(DBColDeploy).Insert(dep)
}

func dbDeployGet(id *SwoId) (*DeployDesc, error) {
	var dep DeployDesc
	err := dbSession.DB(DBStateDB).C(DBColDeploy).Find(bson.M{"cookie": id.Cookie()}).One(&dep)
	return &dep, err
}

func dbDeployDel(dep *DeployDesc) error {
	return dbSession.DB(DBStateDB).C(DBColDeploy).Remove(bson.M{"cookie": dep.Cookie})
}

func dbDeployList() (deps []DeployDesc, err error) {
	err = dbSession.DB(DBStateDB).C(DBColDeploy).Find(bson.M{}).All(&deps)
	return
}

func dbDeployStateUpdate(dep *DeployDesc, state int) error {
	dep.State = state
	return dbSession.DB(DBStateDB).C(DBColDeploy).Update(bson.M{"cookie": dep.Cookie},
			bson.M{"$set": bson.M{"state": state}})
}

func dbListFnEvents(fnid string) ([]FnEventDesc, error) {
	var ret []FnEventDesc
	err := dbSession.DB(DBStateDB).C(DBColEvents).Find(bson.M{"fnid": fnid}).All(&ret)
	return ret, err
}

func dbAddEvent(ed *FnEventDesc) error {
	return dbSession.DB(DBStateDB).C(DBColEvents).Insert(ed)
}

func dbFindEvent(id string) (*FnEventDesc, error) {
	var ed FnEventDesc
	err := dbSession.DB(DBStateDB).C(DBColEvents).Find(bson.M{"_id": bson.ObjectIdHex(id)}).One(&ed)
	return &ed, err
}

func dbRemoveEvent(id string) error {
	return dbSession.DB(DBStateDB).C(DBColEvents).Remove(bson.M{"_id": bson.ObjectIdHex(id)})
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

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		glog.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB, DBStateDB, err.Error())
		return err
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	dbSession = session.Copy()

	// Make sure the indices are present
	index := mgo.Index{
			Unique:		true,
			DropDups:	true,
			Background:	true,
			Sparse:		true}

	index.Key = []string{"cookie"}
	err = dbSession.DB(DBStateDB).C(DBColFunc).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for functions: %s", err.Error())
	}
	err = dbSession.DB(DBStateDB).C(DBColMware).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No cookie index for mware: %s", err.Error())
	}

	index.Key = []string{"uid"}
	err = dbSession.DB(DBStateDB).C(DBColBalancerRS).EnsureIndex(index)
	if err != nil {
		return fmt.Errorf("No uid index for balancerrs: %s", err.Error())
	}

	return nil

}

func dbDisconnect() {
	dbSession.Close()
	dbSession = nil
}
