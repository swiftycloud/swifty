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
	SwoId
	Commit		string		`bson:"commit"`
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
	c := dbSession.DB(dbState).C(DBColMware)
	err := c.Update(bson.M{"_id": desc.ObjID},
		bson.M{"$set": bson.M{
				"client":	desc.Client,
				"pass":		desc.Pass,
				"jsettings":	desc.JSettings,
				"state":	desc.State,
			}})
	if err != nil {
		log.Errorf("Can't update added %s: %s", desc.SwoId.Str(), err.Error())
	}

	return err
}

func dbMwareRemove(mwd *MwareDesc) error {
	c := dbSession.DB(dbState).C(DBColMware)
	return c.Remove(bson.M{"_id": mwd.ObjID})
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

func dbMwareResolveClient(client string) (MwareDesc, error) {
	return dbMwareGetOne(bson.M{"client": client})
}

func dbMwareGetAll(id *SwoId) ([]MwareDesc, error) {
	var recs []MwareDesc
	c := dbSession.DB(dbState).C(DBColMware)
	err := c.Find(bson.M{"tennant": id.Tennant, "project": id.Project}).All(&recs)
	return recs, err
}


func dbGetFuncStatusString(status int) string {
	status_str := map[int]string {
		swy.DBFuncStateQue:	"Queued",
		swy.DBFuncStateBld:	"Building",
		swy.DBFuncStateBlt:	"Built",
		swy.DBFuncStatePrt:	"Partial",
		swy.DBFuncStateRdy:	"Ready",
		swy.DBFuncStateTrm:	"Terminating",
	}

	return status_str[status]
}

func dbGetPodStateString(status int) string {
	var ret string

	str := map[int]string {
		swy.DBPodStateNak:	"Unknown",
		swy.DBPodStateQue:	"Queued",
		swy.DBPodStateRdy:	"Ready",
		swy.DBPodStateTrm:	"Terminating",
		swy.DBPodStateBsy:	"Busy",
	}

	ret, ok := str[status]
	if !ok {
		ret = str[swy.DBPodStateNak]
	}

	return ret
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
	return dbFuncFindOne(bson.M{"index": cookie})
}

func dbFuncFindStates(id *SwoId, states []int) (FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"tennant": id.Tennant, "project": id.Project, "name": id.Name,
		"state": bson.M{"$in": states}})
}

func dbFuncListByProjCond(id *SwoId, cond bson.M) ([]FunctionDesc, error) {
	vs, err := dbFuncFindAll(bson.M{"tennant": id.Tennant, "project": id.Project, "state": cond})
	return vs, err
}

func dbFuncListStates(id *SwoId, states []int) ([]FunctionDesc, error) {
	return dbFuncListByProjCond(id, bson.M{"$in": states})
}

func dbFuncList() ([]FunctionDesc, error) {
	return dbFuncFindAll(bson.M{})
}

func dbFuncListMwEvent(id *SwoId, mqueue string) ([]FunctionDesc, error) {
	return dbFuncFindAll(bson.M{"tennant": id.Tennant, "project": id.Project,
		"event.source": "mware", "event.mwid": id.Name, "event.mqueue": mqueue})
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
				"src.commit": fn.Src.Commit,
				"code.script": fn.Code.Script,
				"cronid": fn.CronID,
				"mware": fn.Mware,
				"oneshot": fn.OneShot,
				"urlcall": fn.URLCall,
			}})
	if err != nil {
		log.Errorf("Can't update added %s: %s", fn.Name, err.Error())
	}

	return err
}

func dbFuncUpdatePulled(fn *FunctionDesc) error {
	err := dbFuncUpdate(
		bson.M{"tennant": fn.Tennant, "project": fn.Project, "name": fn.Name},
		bson.M{"$set": bson.M{"src.commit": fn.Src.Commit, "state": fn.State, }})
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

func dbFuncRemove(fn *FunctionDesc) {
	c := dbSession.DB(dbState).C(DBColFunc)
	c.Remove(bson.M{"_id": fn.ObjID});
}

func logSaveResult(fn *FunctionDesc, event, stdout, stderr string) {
	c := dbSession.DB(dbState).C(DBColLogs)
	text := fmt.Sprintf("out: [%s], err: [%s]", stdout, stderr)
	c.Insert(DBLogRec{
		SwoId:		fn.SwoId,
		Commit:		fn.Src.Commit,
		Event:		event,
		Time:		time.Now(),
		Text:		text,
	})
}

func logSaveEvent(fn *FunctionDesc, event, text string) {
	c := dbSession.DB(dbState).C(DBColLogs)
	c.Insert(DBLogRec{
		SwoId:		fn.SwoId,
		Commit:		fn.Src.Commit,
		Event:		event,
		Time:		time.Now(),
		Text:		text,
	})
}

func logGetFor(id *SwoId) ([]DBLogRec, error) {
	var logs []DBLogRec
	c := dbSession.DB(dbState).C(DBColLogs)
	err := c.Find(bson.M{"tennant": id.Tennant, "project": id.Project, "function": id.Name}).All(&logs)
	return logs, err
}

func logRemove(fn *FunctionDesc) {
	c := dbSession.DB(dbState).C(DBColLogs)
	_, err := c.RemoveAll(bson.M{"tennant": fn.Tennant, "project": fn.Project, "function": fn.Name})
	if err != nil {
		log.Errorf("logs %s.%s remove error: %s", fn.Project, fn.Name, err.Error())
	}
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

func dbBalancerPodAdd(link *BalancerLink, uid, wdogaddr string) (error) {
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Insert(bson.M{
			"balancerid":	link.ObjID,
			"uid":		uid,
			"wdogaddr":	wdogaddr,
		})
	if err != nil {
		log.Errorf("balancer-db: Can't add pod %s/%s/%s: %s",
				link.DepName, uid, wdogaddr, err.Error())
	} else {
		eref := dbBalancerRefIncRS(link)
		if eref != nil {
			log.Errorf("balancer-db: Can't increment RS %s/%s/%s: %s",
					link.DepName, uid, wdogaddr, eref.Error())
		}
	}
	return err
}

func dbBalancerPodDel(link *BalancerLink, uid string) (error) {
	c := dbSession.DB(dbState).C(DBColBalancerRS)
	err := c.Remove(bson.M{
			"balancerid":	link.ObjID,
			"uid":	uid,
		})
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't del pod %s/%s: %s",
				link.DepName, uid, err.Error())
	} else {
		eref := dbBalancerRefDecRS(link)
		if eref != nil {
			log.Errorf("balancer-db: Can't decrement RS %s/%s: %s",
					link.DepName, uid, eref.Error())
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

func dbBalancerLinkFind(depname string) (*BalancerLink) {
	var link BalancerLink

	c := dbSession.DB(dbState).C(DBColBalancer)
	err := c.Find(bson.M{"depname": depname}).One(&link)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("balancer-db: Can't find link %s: %s",
				depname, err.Error())
		return nil
	}

	return &link
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
	err := c.Insert(bson.M{
			"depname":	link.DepName,
			"addr":		link.Addr,
			"port":		link.Port,
			"numrs":	link.NumRS,
			"cntrs":	link.CntRS,
		})
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
		Password:	conf.DB.Pass}

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
