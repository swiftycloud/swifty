package main

import (
	"time"
	"fmt"
	"context"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"../common"
)

const (
	DBColFunc	= "Function"
	DBColMware	= "Mware"
	DBColLogs	= "Logs"
	DBColFnStats	= "FnStats"
	DBColBalancer	= "Balancer"
	DBColBalancerRS = "BalancerRS"
)

type DBLogRec struct {
	FnId		string		`bson:"fnid"`
	Event		string		`bson:"event"`
	Time		time.Time	`bson:"ts"`
	Text		string		`bson:"text"`
}

var dbSession *mgo.Session
var dbState string

func dbMwareAdd(desc *MwareDesc) error {
	c := dbSession.DB(dbState).C(DBColMware)
	return c.Insert(desc)
}

func dbMwareUpdateAdded(desc *MwareDesc) error {
	desc.State = swy.DBMwareStateRdy
	c := dbSession.DB(dbState).C(DBColMware)
	return c.Update(bson.M{"cookie": desc.Cookie},
		bson.M{"$set": bson.M{
				"client":	desc.Client,
				"secret":	desc.Secret,
				"namespace":	desc.Namespace,
				"state":	desc.State,
			}})
}

func dbMwareTerminate(mwd *MwareDesc) error {
	c := dbSession.DB(dbState).C(DBColMware)
	return c.Update(
		bson.M{"cookie": mwd.Cookie, "state": bson.M{"$in": []int{swy.DBMwareStateRdy, swy.DBMwareStateStl}}},
		bson.M{"$set": bson.M{"state": swy.DBMwareStateTrm, }})
}

func dbMwareRemove(mwd *MwareDesc) error {
	c := dbSession.DB(dbState).C(DBColMware)
	return c.Remove(bson.M{"cookie": mwd.Cookie})
}

func dbMwareSetStalled(mwd *MwareDesc) error {
	c := dbSession.DB(dbState).C(DBColMware)
	return c.Update( bson.M{"cookie": mwd.Cookie, },
		bson.M{"$set": bson.M{"state": swy.DBMwareStateStl, }})
}

func dbMwareGetOne(q bson.M) (MwareDesc, error) {
	c := dbSession.DB(dbState).C(DBColMware)
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

func dbMwareGetAll(id *SwoId) ([]MwareDesc, error) {
	var recs []MwareDesc
	c := dbSession.DB(dbState).C(DBColMware)
	err := c.Find(bson.M{"tennant": id.Tennant, "project": id.Project}).All(&recs)
	return recs, err
}


func dbFuncFindOne(q bson.M) (*FunctionDesc, error) {
	c := dbSession.DB(dbState).C(DBColFunc)
	var v FunctionDesc
	err := c.Find(q).One(&v)
	return &v, err
}

func dbFuncFindAll(q bson.M) (vs []FunctionDesc, err error) {
	c := dbSession.DB(dbState).C(DBColFunc)
	err = c.Find(q).All(&vs)
	return
}

func dbFuncUpdate(q, ch bson.M) (error) {
	c := dbSession.DB(dbState).C(DBColFunc)
	return c.Update(q, ch)
}

func dbFuncFind(id *SwoId) (*FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"cookie": id.Cookie()})
}

func dbFuncFindByCookie(cookie string) (*FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"cookie": cookie})
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

func dbFuncSetStateCond(id *SwoId, state int, states []int) error {
	return dbFuncUpdate(
		bson.M{"cookie": id.Cookie(), "state": bson.M{"$in": states}},
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
				"cronid": fn.CronID,
				"oneshot": fn.OneShot,
				"state": fn.State,
				"urlcall": fn.URLCall,
			}})
}

func dbFuncUpdatePulled(fn *FunctionDesc, update bson.M) error {
	return dbFuncUpdate(
		bson.M{"cookie": fn.Cookie},
		bson.M{"$set": update })
}

func dbFuncAdd(desc *FunctionDesc) error {
	c := dbSession.DB(dbState).C(DBColFunc)
	return c.Insert(desc)
}

func dbFuncRemove(fn *FunctionDesc) error {
	c := dbSession.DB(dbState).C(DBColFunc)
	return c.Remove(bson.M{"cookie": fn.Cookie});
}

func dbStatsGet(cookie string, st *FnStats) error {
	c := dbSession.DB(dbState).C(DBColFnStats)
	return c.Find(bson.M{"cookie": cookie}).One(st)
}

func dbStatsUpdate(st *FnStats) {
	c := dbSession.DB(dbState).C(DBColFnStats)
	c.Upsert(bson.M{"cookie": st.Cookie}, st)
}

func dbStatsDrop(cookie string) error {
	c := dbSession.DB(dbState).C(DBColFnStats)
	err := c.Remove(bson.M{"cookie": cookie})
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func logSaveResult(fnCookie, event, stdout, stderr string) {
	c := dbSession.DB(dbState).C(DBColLogs)
	text := fmt.Sprintf("out: [%s], err: [%s]", stdout, stderr)
	c.Insert(DBLogRec{
		FnId:		fnCookie,
		Event:		event,
		Time:		time.Now(),
		Text:		text,
	})
}

func logSaveEvent(fn *FunctionDesc, event, text string) {
	c := dbSession.DB(dbState).C(DBColLogs)
	c.Insert(DBLogRec{
		FnId:		fn.Cookie,
		Event:		event,
		Time:		time.Now(),
		Text:		text,
	})
}

func logGetFor(id *SwoId) ([]DBLogRec, error) {
	var logs []DBLogRec
	c := dbSession.DB(dbState).C(DBColLogs)
	err := c.Find(bson.M{"fnid": id.Cookie()}).All(&logs)
	return logs, err
}

func logGetCalls(id *SwoId) (int, error) {
	c := dbSession.DB(dbState).C(DBColLogs)
	return c.Find(bson.M{"fnid": id.Cookie(), "event": "run"}).Count()
}

func logRemove(fn *FunctionDesc) error {
	c := dbSession.DB(dbState).C(DBColLogs)
	_, err := c.RemoveAll(bson.M{"fnid": fn.Cookie})
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func dbBalancerRSListVersions(fn *FunctionDesc) ([]string, error) {
	var fv []string
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Find(bson.M{"fnid": fn.Cookie, "instance": swy.SwyPodInstRun}).Distinct("fnversion", &fv)
	return fv, err
}

func dbBalancerPodFindExact(fnid, version string) (*BalancerRS, error) {
	var v BalancerRS

	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Find(bson.M{
			"fnid":		fnid,
			"instance":	swy.SwyPodInstRun,
			"fnversion":	version,
		}).One(&v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &v, nil
}

func dbBalancerPodAdd(link *BalancerLink, pod *k8sPod) error {
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Insert(bson.M{
			"fnid":		link.FnId,
			"depname":	pod.DepName,
			"uid":		pod.UID,
			"wdogaddr":	pod.WdogAddr,
			"instance":	pod.Instance,
			"fnversion":	pod.Version,
		})
	if err != nil {
		return fmt.Errorf("add: %s", err.Error())
	}

	err = dbBalancerRefIncRS(link)
	if err != nil {
		return fmt.Errorf("inc rs: %s", err.Error())
	}

	return nil
}

func dbBalancerPodDel(link *BalancerLink, pod *k8sPod) (error) {
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Remove(bson.M{ "uid":	pod.UID, })
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}

		return fmt.Errorf("del: %s", err.Error())
	}

	err = dbBalancerRefDecRS(link)
	if err != nil {
		return fmt.Errorf("dec rs: %s", err.Error())
	}

	return nil
}

func dbBalancerPodPop(link *BalancerLink, pod *k8sPod) (*BalancerRS, error) {
	var v BalancerRS

	c := dbSession.DB(dbState).C(DBColBalancerRS)
	_, err := c.Find(bson.M{ "uid":	pod.UID }).Apply(mgo.Change{Remove: true}, &v)
	if err != nil {
		return nil, fmt.Errorf("pop: %s", err.Error())
	}

	err = dbBalancerRefDecRS(link)
	if err != nil {
		return &v, fmt.Errorf("dec rs: %s", err.Error())
	}

	return &v, nil
}


func dbBalancerPodDelAll(link *BalancerLink) (error) {
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Remove(bson.M{ "depname": link.DepName })
	if err == mgo.ErrNotFound {
		err = nil
	}

	return err
}

func dbBalancerOpRS(link *BalancerLink, update bson.M) (error) {
	var v BalancerLink
	c := dbSession.DB(dbState).C(DBColBalancer)
	change := mgo.Change{
		Upsert:		false,
		Remove:		false,
		Update:		update,
		ReturnNew:	false,
	}
	querier := bson.M{ "_id": link.ObjID, }
	_, err := c.Find(querier).Apply(change, &v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}

		return fmt.Errorf("oprs: %s", err.Error())
	}

	return nil
}

func dbBalancerRefIncRS(link *BalancerLink) (error) {
	return dbBalancerOpRS(link,
		bson.M{"$inc": bson.M{"cntrs": 1}})
}

func dbBalancerRefDecRS(link *BalancerLink) (error) {
	return dbBalancerOpRS(link,
		bson.M{"$inc": bson.M{"cntrs": -1}})
}

func dbBalancerGetConnInfo(field, value string) (*BalancerConn, error) {
	var resp []bson.M

	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Pipe([]bson.M{
		bson.M{"$match":bson.M{field: value}},
		bson.M{"$count":"cntrs"},
		bson.M{"$addFields":bson.M{field:value}},
		bson.M{"$lookup":bson.M{"from":"Balancer","localField":"depname","foreignField":"depname","as":"link"}},
	}).All(&resp)
	if err != nil {
		fmt.Errorf("No pipe %s", err.Error())
		return nil, err
	}

	l := resp[0]["link"].([]interface{})[0].(bson.M)

	return &BalancerConn{
		Addr: l["addr"].(string),
		Port: l["port"].(int),
		CntRS: resp[0]["cntrs"].(int),
		Public: l["public"].(bool),
	}, nil
}

func dbBalancerGetConnByDep(depname string) (*BalancerConn, error) {
	return dbBalancerGetConnInfo("depname", depname)
}

func dbBalancerGetConnByCookie(cookie string) (*BalancerConn, error) {
	return dbBalancerGetConnInfo("fnid", cookie)
}

func dbBalancerLinkFindByDepname(depname string) (*BalancerLink, error) {
	var link BalancerLink

	c := dbSession.DB(dbState).C(DBColBalancer)
	err := c.Find(bson.M{"depname": depname}).One(&link)

	return &link, err
}

func dbBalancerLinkFindAll() ([]BalancerLink, error) {
	var links []BalancerLink

	c := dbSession.DB(dbState).C(DBColBalancer)
	err := c.Find(bson.M{}).All(&links)
	if err != nil && err != mgo.ErrNotFound {
		return nil, err
	}

	return links, nil
}

func dbBalancerLinkAdd(link *BalancerLink) (error) {
	c := dbSession.DB(dbState).C(DBColBalancer)
	return c.Insert(link)
}

func dbBalancerLinkDel(link *BalancerLink) (error) {
	c := dbSession.DB(dbState).C(DBColBalancer)
	return c.Remove(bson.M{"depname": link.DepName})
}

func dbProjectListAll(ten string) (fn []string, mw []string, err error) {
	c := dbSession.DB(dbState).C(DBColFunc)
	err = c.Find(bson.M{"tennant": ten}).Distinct("project", &fn)
	if err != nil {
		return
	}

	c = dbSession.DB(dbState).C(DBColMware)
	err = c.Find(bson.M{"tennant": ten}).Distinct("project", &mw)
	return
}

func dbConnect(conf *YAMLConf) error {
	info := mgo.DialInfo{
		Addrs:		[]string{conf.DB.Addr},
		Database:	conf.DB.StateDB,
		Timeout:	60 * time.Second,
		Username:	conf.DB.User,
		Password:	gateSecrets[conf.DB.Pass]}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		glog.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB.Addr, conf.DB.StateDB, err.Error())
		return err
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	dbSession = session.Copy()
	dbState = conf.DB.StateDB

	// Make sure the indices are present
	index := mgo.Index{
			Unique:		true,
			DropDups:	true,
			Background:	true,
			Sparse:		true}

	index.Key = []string{"cookie"}
	dbSession.DB(dbState).C(DBColFunc).EnsureIndex(index)

	index.Key = []string{"addr", "depname"}
	dbSession.DB(dbState).C(DBColBalancer).EnsureIndex(index)

	index.Key = []string{"uid"}
	dbSession.DB(dbState).C(DBColBalancerRS).EnsureIndex(index)

	return nil

}

func dbDisconnect() {
	dbSession.Close()
	dbSession = nil
}
