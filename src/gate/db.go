package main

import (
	"time"
	"fmt"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"../common"
)

const (
	DBName		= "swifty"
	DBColFunc	= "Function"
	DBColMware	= "Mware"
	DBColLogs	= "Logs"
	DBColBalancer	= "Balancer"
	DBColBalancerRS = "BalancerRS"
)

type DBLogRec struct {
	Project		string		`bson:"project"`
	FuncName	string		`bson:"function"`
	Commit		string		`bson:"commit"`
	Event		string		`bson:"event"`
	Time		time.Time	`bson:"ts"`
	Text		string		`bson:"text"`
}

var dbSession *mgo.Session
var dbDBName string = DBName

type dbMwareStateArgs struct {
	Col	*mgo.Collection
	Q	interface{}
	Ch	interface{}
}

func dbMwareSetStateCb(data interface{}) error {
	var args *dbMwareStateArgs = data.(*dbMwareStateArgs)
	return args.Col.Update(args.Q, args.Ch)
}

func dbMwareSetState(project string, mwareid string, state int) error {
	err := swy.Retry10(dbMwareSetStateCb,
			&dbMwareStateArgs{
				Col:	dbSession.DB(dbDBName).C(DBColMware),
				Q:	bson.M{"project": project, "mwareid": mwareid, "state": bson.M{"$ne": state}},
				Ch:	bson.M{"$set": bson.M{"state": state}},
			})
	if err != nil {
		return fmt.Errorf("dbMwareSetState: Can't set state %d project %s mwareid %s:%s",
					state, project, mwareid, err.Error())
	}
	return nil
}

func dbMwareLock(project string, mwareid string) error {
	return dbMwareSetState(project, mwareid, swy.DBMwareStateBsy)
}

func dbMwareUnlock(project string, mwareid string) error {
	return dbMwareSetState(project, mwareid, swy.DBMwareStateRdy)
}

func dbMwareAddRefOrInsertLocked(project string, mwareid string) (bool, MwareDesc, error) {
	c := dbSession.DB(dbDBName).C(DBColMware)
	v := MwareDesc{}

	// It locks the record if new added
	change := mgo.Change{
		Upsert:		true,
		Remove:		false,
		Update:		bson.M{"$inc": bson.M{"counter": 1},
					"$setOnInsert":
						bson.M{"state": swy.DBMwareStateBsy,
							"project": project,
							"mwareid": mwareid}},
		ReturnNew:	true,
	}

	querier := bson.M{	"project": project,
				"mwareid": mwareid,
				"state": bson.M{"$eq": swy.DBMwareStateRdy}}

	info, err := c.Find(querier).Apply(change, &v)
	if err != nil {
		return false, v, err
	}

	if info.Updated == 0 {
		v.ObjID = info.UpsertedId.(bson.ObjectId)
		return false, v, nil
	}

	return true, v, nil
}

func dbMwareRemove(mwd MwareDesc) error {
	c := dbSession.DB(dbDBName).C(DBColMware)
	return c.Remove(bson.M{"_id": mwd.ObjID})
}

func dbMwareDecRefLocked(project string, mwareid string) (bool, MwareDesc, error) {
	c := dbSession.DB(dbDBName).C(DBColMware)
	v := MwareDesc{}

	change := mgo.Change{
		Upsert:		false,
		Remove:		false,
		Update:		bson.M{"$inc": bson.M{"counter": -1}},
		ReturnNew:	true,
	}

	querier := bson.M{	"project": project,
				"mwareid": mwareid,
				"state": bson.M{"$eq": swy.DBMwareStateBsy}}

	info, err := c.Find(querier).Apply(change, &v)
	if err != nil {
		return false, v, err
	}

	if info.Updated > 0 {
		if v.Counter == 0 {
			return true, v, nil
		}
	}

	return false, v, nil
}

func dbMwareRemoveLocked(mwd MwareDesc) error {
	c := dbSession.DB(dbDBName).C(DBColMware)
	querier := bson.M{	"project": mwd.Project,
				"mwareid": mwd.MwareID,
				"state": bson.M{"$eq": swy.DBMwareStateBsy}}

	err := c.Remove(querier);
	if err != nil {
		return fmt.Errorf("dbMwareRemove: Can't remove mwareid %s: %s",
						mwd.MwareID, err.Error())
	}
	return nil
}

func dbMwareAddSettingsUnlock(mwd MwareDesc, settings []byte) error {
	c := dbSession.DB(dbDBName).C(DBColMware)
	v := MwareDesc{}

	// It unlocks the record
	change := mgo.Change{
		Upsert:		false,
		Remove:		false,
		Update:
			bson.M{	"project":	mwd.Project,
				"mwareid":	mwd.MwareID,
				"mwaretype":	mwd.MwareType,
				"client":	mwd.Client,
				"pass":		mwd.Pass,
				"counter":	1,
				"jsettings":	string(settings),
				"state":	swy.DBMwareStateRdy,
			},
		ReturnNew:	true,
	}

	querier := bson.M{	"_id": mwd.ObjID,
				"state": bson.M{"$eq": swy.DBMwareStateBsy}}
	_, err := c.Find(querier).Apply(change, &v)
	if err != nil {
		c.Remove(bson.M{"_id": mwd.ObjID})
		return fmt.Errorf("dbMwareAdd: Can't add mware %s, removing: %s",
					mwd.MwareID, err.Error())
	}
	return nil
}

func dbMwareGetItem(project string, mwareid string) (MwareDesc, error) {
	c := dbSession.DB(dbDBName).C(DBColMware)
	v := MwareDesc{}
	err := c.Find(bson.M{"project": project, "mwareid": mwareid}).One(&v)
	return v, err
}

func dbMwareGetAll(project string) ([]MwareDesc, error) {
	var recs []MwareDesc
	c := dbSession.DB(dbDBName).C(DBColMware)
	err := c.Find(bson.M{"project": project}).All(&recs)
	return recs, err
}

func dbMwareResolveClient(client string) (MwareDesc, error) {
	c := dbSession.DB(dbDBName).C(DBColMware)
	rec := MwareDesc{}
	err := c.Find(bson.M{"client": client}).One(&rec)
	return rec, err
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
	c := dbSession.DB(dbDBName).C(DBColFunc)
	err = c.Find(q).One(&v)
	return
}

func dbFuncFindAll(q bson.M) (vs []FunctionDesc, err error) {
	c := dbSession.DB(dbDBName).C(DBColFunc)
	err = c.Find(q).All(&vs)
	return
}

func dbFuncUpdate(q, ch bson.M) (error) {
	c := dbSession.DB(dbDBName).C(DBColFunc)
	return c.Update(q, ch)
}

func dbFuncFind(project, funcname string) (FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"project": project, "name": funcname})
}

func dbFuncFindStates(project, funcname string, states []int) (FunctionDesc, error) {
	return dbFuncFindOne(bson.M{"project":	project, "name": funcname,
		"state": bson.M{"$in": states}})
}

func dbFuncListByProjCond(project string, cond bson.M) ([]FunctionDesc, error) {
	vs, err := dbFuncFindAll(bson.M{"project": project, "state": cond})
	return vs, err
}

func dbFuncListAll(project string, states []int) ([]FunctionDesc, error) {
	return dbFuncListByProjCond(project, bson.M{"$in": states})
}

func dbFuncListByMwEvent(project, mwid, mqueue string) ([]FunctionDesc, error) {
	return dbFuncFindAll(bson.M{"project": project,
		"event.source": "mware", "event.mwid": mwid, "event.mqueue": mqueue})
}

func dbFuncListWithEvents() ([]FunctionDesc, error) {
	return dbFuncFindAll(bson.M{"event.source": bson.M{"$ne": ""}})
}

func dbFuncSetStateCond(project string, funcname string, state int, states []int) error {
	err := dbFuncUpdate(
		bson.M{"project": project, "name": funcname, "state": bson.M{"$in": states}},
		bson.M{"$set": bson.M{"state": state}})
	if err != nil {
		log.Errorf("dbFuncSetState: Can't change function %s to state %d: %s",
				funcname, state, err.Error())
	}

	return err
}

func dbFuncSetState(fn *FunctionDesc, state int) error {
	fn.State = state
	err := dbFuncUpdate(
		bson.M{"project": fn.Project, "name": fn.FuncName, "state": bson.M{"$ne": state}},
		bson.M{"$set": bson.M{"state": state}})
	if err != nil {
		log.Errorf("dbFuncSetState: Can't change function %s state: %s",
				fn.FuncName, err.Error())
	}

	return err
}

func dbFuncUpdateAdded(fn *FunctionDesc) error {
	err := dbFuncUpdate(
		bson.M{"project": fn.Project, "name": fn.FuncName},
		bson.M{"$set": bson.M{
				"commit": fn.Commit,
				"cronid": fn.CronID,
				"mware": fn.Mware,
				"oneshot": fn.OneShot,
				"urlcall": fn.URLCall,
			}})
	if err != nil {
		log.Errorf("Can't update added %s: %s", fn.FuncName, err.Error())
	}

	return err
}

func dbFuncUpdatePulled(fn *FunctionDesc) error {
	err := dbFuncUpdate(
		bson.M{"project": fn.Project, "name": fn.FuncName},
		bson.M{"$set": bson.M{"commit": fn.Commit, "oldcommit": fn.OldCommit, "state": fn.State, }})
	if err != nil {
		log.Errorf("Can't update pulled %s: %s", fn.FuncName, err.Error())
	}

	return err
}

func dbFuncAdd(desc *FunctionDesc) error {
	desc.Index = desc.Cookie
	log.Debugf("ADD FN DB %v", desc)
	c := dbSession.DB(dbDBName).C(DBColFunc)
	err := c.Insert(desc)
	if err != nil {
		log.Errorf("dbFuncAdd: Can't add function %v: %s",
				desc, err.Error())
	}

	return err
}

func dbFuncRemove(fn *FunctionDesc) {
	c := dbSession.DB(dbDBName).C(DBColFunc)
	c.Remove(bson.M{"index": fn.Cookie});
}

func logSaveResult(fn *FunctionDesc, event, stdout, stderr string) {
	c := dbSession.DB(dbDBName).C(DBColLogs)
	text := fmt.Sprintf("out: [%s], err: [%s]", stdout, stderr)
	c.Insert(DBLogRec{
		Project:	fn.Project,
		FuncName:	fn.FuncName,
		Commit:		fn.Commit,
		Event:		event,
		Time:		time.Now(),
		Text:		text,
	})
}

func logSaveEvent(fn *FunctionDesc, event, text string) {
	c := dbSession.DB(dbDBName).C(DBColLogs)
	c.Insert(DBLogRec{
		Project:	fn.Project,
		FuncName:	fn.FuncName,
		Commit:		fn.Commit,
		Event:		event,
		Time:		time.Now(),
		Text:		text,
	})
}

func logGetFor(project, funcname string) ([]DBLogRec, error) {
	var logs []DBLogRec
	c := dbSession.DB(dbDBName).C(DBColLogs)
	err := c.Find(bson.M{"project": project, "function": funcname}).All(&logs)
	return logs, err
}

func logRemove(fn *FunctionDesc) {
	c := dbSession.DB(dbDBName).C(DBColLogs)
	_, err := c.RemoveAll(bson.M{"project": fn.Project, "function": fn.FuncName})
	if err != nil {
		log.Errorf("logs %s.%s remove error: %s", fn.Project, fn.FuncName, err.Error())
	} else {
		log.Debugf("Removed logs for %s", fn.FuncName);
	}
}

func dbBalancerPodFind(link *BalancerLink, uid string) (*BalancerRS) {
	var v BalancerRS

	c := dbSession.DB(dbDBName).C(DBColBalancerRS)
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

	c := dbSession.DB(dbDBName).C(DBColBalancerRS)
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
	c := dbSession.DB(dbDBName).C(DBColBalancerRS)
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
	c := dbSession.DB(dbDBName).C(DBColBalancerRS)
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
	c := dbSession.DB(dbDBName).C(DBColBalancerRS)
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
	c := dbSession.DB(dbDBName).C(DBColBalancer)
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

	c := dbSession.DB(dbDBName).C(DBColBalancer)
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

	c := dbSession.DB(dbDBName).C(DBColBalancer)
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
	c := dbSession.DB(dbDBName).C(DBColBalancer)
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
	c := dbSession.DB(dbDBName).C(DBColBalancer)
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

func dbProjectListAll() (fn []string, mw []string, err error) {
	c := dbSession.DB(dbDBName).C(DBColFunc)
	err = c.Find(nil).Distinct("project", &fn)
	if err != nil {
		return
	}

	c = dbSession.DB(dbDBName).C(DBColMware)
	err = c.Find(nil).Distinct("project", &mw)
	return
}

func dbConnect(conf *YAMLConf) error {
	info := mgo.DialInfo{
		Addrs:		[]string{conf.DB.Addr},
		Database:	conf.DB.Name,
		Timeout:	60 * time.Second,
		Username:	conf.DB.User,
		Password:	conf.DB.Pass}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		log.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB.Addr, conf.DB.Name, err.Error())
		return err
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	dbSession = session.Copy()
	dbDBName = conf.DB.Name

	// Make sure the indices are present
	index := mgo.Index{
			Unique:		true,
			DropDups:	true,
			Background:	true,
			Sparse:		true}

	index.Key = []string{"index"}
	dbSession.DB(dbDBName).C(DBColFunc).EnsureIndex(index)

	index.Key = []string{"addr", "depname"}
	dbSession.DB(dbDBName).C(DBColBalancer).EnsureIndex(index)

	index.Key = []string{"uid"}
	dbSession.DB(dbDBName).C(DBColBalancerRS).EnsureIndex(index)

	return nil

}

func dbDisconnect() {
	dbSession.Close()
	dbSession = nil
	dbDBName = DBName
}
