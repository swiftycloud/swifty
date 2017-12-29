package main

import (
	"time"
	"fmt"

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
	err := c.Insert(desc)
	if err != nil {
		log.Errorf("Can't add mware %s: %s", desc.SwoId.Str(), err.Error())
	}

	return err
}

func dbMwareUpdateAdded(desc *MwareDesc) error {
	desc.State = swy.DBMwareStateRdy
	c := dbSession.DB(dbState).C(DBColMware)
	err := c.Update(bson.M{"cookie": desc.Cookie},
		bson.M{"$set": bson.M{
				"client":	desc.Client,
				"secret":	desc.Secret,
				"namespace":	desc.Namespace,
				"state":	desc.State,
			}})
	if err != nil {
		log.Errorf("Can't update added %s: %s", desc.SwoId.Str(), err.Error())
	}

	return err
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
	return dbMwareGetOne(bson.M{"tennant": id.Tennant,
			"project": id.Project, "name": id.Name})
}

func dbMwareGetReady(id *SwoId) (MwareDesc, error) {
	return dbMwareGetOne(bson.M{"tennant": id.Tennant,
			"project": id.Project, "name": id.Name,
			"state": swy.DBMwareStateRdy})
}

func dbMwareGetAll(id *SwoId) ([]MwareDesc, error) {
	var recs []MwareDesc
	c := dbSession.DB(dbState).C(DBColMware)
	err := c.Find(bson.M{"tennant": id.Tennant, "project": id.Project}).All(&recs)
	return recs, err
}


func dbFuncFindOne(q bson.M) (v FunctionDesc, err error) {
	c := dbSession.DB(dbState).C(DBColFunc)
	err = c.Find(q).One(&v)
	return
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

func dbFuncFind(id *SwoId) (FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"tennant": id.Tennant, "project": id.Project, "name": id.Name})
}

func dbFuncFindByCookie(cookie string) (FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"cookie": cookie})
}

func dbFuncFindStates(id *SwoId, states []int) (FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"tennant": id.Tennant, "project": id.Project, "name": id.Name,
		"state": bson.M{"$in": states}})
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
	err := dbFuncUpdate(
		bson.M{"tennant": id.Tennant, "project": id.Project, "name": id.Name,
				"state": bson.M{"$in": states}},
		bson.M{"$set": bson.M{"state": state}})
	if err != nil {
		log.Errorf("dbFuncSetState: Can't change function %s to state %d: %s",
				id.Name, state, err.Error())
	}

	return err
}

func dbFuncSetState(fn *FunctionDesc, state int) error {
	fn.State = state
	err := dbFuncUpdate(
		bson.M{"tennant": fn.Tennant, "project": fn.Project, "name": fn.Name,
				"state": bson.M{"$ne": state}},
		bson.M{"$set": bson.M{"state": state}})
	if err != nil {
		log.Errorf("dbFuncSetState: Can't change function %s state: %s",
				fn.Name, err.Error())
	}

	return err
}

func dbFuncUpdateAdded(fn *FunctionDesc) error {
	err := dbFuncUpdate(
		bson.M{"tennant": fn.Tennant, "project": fn.Project, "name": fn.Name},
		bson.M{"$set": bson.M{
				"src.version": fn.Src.Version,
				"cronid": fn.CronID,
				"oneshot": fn.OneShot,
				"urlcall": fn.URLCall,
			}})
	if err != nil {
		log.Errorf("Can't update added %s: %s", fn.Name, err.Error())
	}

	return err
}

func dbFuncUpdatePulled(fn *FunctionDesc, update bson.M) error {
	err := dbFuncUpdate(
		bson.M{"tennant": fn.Tennant, "project": fn.Project, "name": fn.Name},
		bson.M{"$set": update })
	if err != nil {
		log.Errorf("Can't update pulled %s: %s", fn.Name, err.Error())
	}

	return err
}

func dbFuncAdd(desc *FunctionDesc) error {
	c := dbSession.DB(dbState).C(DBColFunc)
	err := c.Insert(desc)
	if err != nil {
		log.Errorf("dbFuncAdd: Can't add function %v: %s",
				desc, err.Error())
	}

	return err
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
	if err != nil {
		log.Errorf("logs %s remove error: %s", fn.SwoId.Str(), err.Error())
	}
	return err
}

func dbBalancerRSListVersions(fn *FunctionDesc) ([]string, error) {
	var fv []string
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Find(bson.M{"fnid": fn.Cookie}).Distinct("fnversion", &fv)
	return fv, err
}

func dbBalancerPodFind(link *BalancerLink, uid string) (*BalancerRS) {
	var v BalancerRS

	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Find(bson.M{
			"balancerid":	link.ObjID,
			"uid":		uid,
		}).One(&v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't find pod %s/%s: %s",
				link.DepName, uid, err.Error())
		return nil
	}

	return &v
}

func dbBalancerPodFindExact(fnid, version string) (*BalancerRS) {
	var v BalancerRS

	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Find(bson.M{
			"fnid":		fnid,
			"fnversion":	version,
		}).One(&v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't find pod %s/%s: %s",
				fnid, version, err.Error())
		return nil
	}

	return &v
}

func dbBalancerPodFindAll(link *BalancerLink) ([]BalancerRS) {
	var v []BalancerRS

	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Find(bson.M{
			"balancerid": link.ObjID,
		}).All(&v)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't find pods %s: %s",
				link.DepName, err.Error())
		return nil
	}

	return v
}

func dbBalancerPodAdd(link *BalancerLink, pod *k8sPod) error {
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Insert(bson.M{
			"balancerid":	link.ObjID,
			"uid":		pod.UID,
			"wdogaddr":	pod.WdogAddr,
			"fnid":		link.FnId,
			"fnversion":	pod.Version,
		})
	if err != nil {
		log.Errorf("balancer-db: Can't add pod %s/%s/%s: %s",
				link.DepName, pod.UID, pod.WdogAddr, err.Error())
	} else {
		eref := dbBalancerRefIncRS(link)
		if eref != nil {
			log.Errorf("balancer-db: Can't increment RS %s/%s/%s: %s",
					link.DepName, pod.UID, pod.WdogAddr, eref.Error())
		}
	}
	return err
}

func dbBalancerPodDel(link *BalancerLink, pod *k8sPod) (error) {
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Remove(bson.M{
			"balancerid":	link.ObjID,
			"uid":	pod.UID,
		})
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't del pod %s/%s: %s",
				link.DepName, pod.UID, err.Error())
	} else {
		eref := dbBalancerRefDecRS(link)
		if eref != nil {
			log.Errorf("balancer-db: Can't decrement RS %s/%s: %s",
					link.DepName, pod.UID, eref.Error())
		}
	}
	return err
}

func dbBalancerPodDelAll(link *BalancerLink) (error) {
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Remove(bson.M{
			"balancerid":	link.ObjID,
		})
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't del all pods %s: %s",
				link.DepName, err.Error())
	} else {
		return dbBalancerRefZeroRS(link)
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
		log.Errorf("balancer-db: OpRS error %s/%v: %s",
				link.DepName, update, err.Error())
	}
	return err
}

func dbBalancerRefZeroRS(link *BalancerLink) (error) {
	return dbBalancerOpRS(link,
		bson.M{"$set": bson.M{"cntrs": 0}})
}

func dbBalancerRefIncRS(link *BalancerLink) (error) {
	return dbBalancerOpRS(link,
		bson.M{"$inc": bson.M{"cntrs": 1}})
}

func dbBalancerRefDecRS(link *BalancerLink) (error) {
	return dbBalancerOpRS(link,
		bson.M{"$inc": bson.M{"cntrs": -1}})
}

func dbBalancerLinkFind(q bson.M) (*BalancerLink) {
	var link BalancerLink

	c := dbSession.DB(dbState).C(DBColBalancer)
	err := c.Find(q).One(&link)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't find link: %s", err.Error())
		return nil
	}

	return &link
}

func dbBalancerLinkFindByDepname(depname string) (*BalancerLink) {
	return dbBalancerLinkFind(bson.M{"depname": depname})
}

func dbBalancerLinkFindByCookie(cookie string) (*BalancerLink) {
	return dbBalancerLinkFind(bson.M{"fnid": cookie})
}

func dbBalancerLinkFindAll() ([]BalancerLink, error) {
	var links []BalancerLink

	c := dbSession.DB(dbState).C(DBColBalancer)
	err := c.Find(bson.M{}).All(&links)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("balancer-db: Can't find links %s/%s: %s", err.Error())
			return nil, err
		}
	}

	return links, nil
}

func dbBalancerLinkAdd(link *BalancerLink) (error) {
	c := dbSession.DB(dbState).C(DBColBalancer)
	err := c.Insert(link)
	if err != nil {
		log.Errorf("balancer-db: Can't insert link %v: %s",
				link, err.Error())
	}
	return err
}

func dbBalancerLinkDel(link *BalancerLink) (error) {
	c := dbSession.DB(dbState).C(DBColBalancer)
	err := c.Remove(bson.M{
			"depname":	link.DepName,
		})
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't remove link %v: %s",
				link, err.Error())
	}
	return err
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
		log.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
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
