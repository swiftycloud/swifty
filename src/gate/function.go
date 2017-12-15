package main

import (
	"errors"
	"fmt"
	"encoding/json"
	"gopkg.in/mgo.v2/bson"

	"../apis/apps"
	"../common"
	"../common/xratelimit"
)

/*
 * On function states:
 *
 * Que: PODs are on their way
 * Bld: building is in progress (POD is starting or build cmd is running)
 * Blt: build completed, PODs are on their way
 * Rdy: ready to run (including rolling update in progress)
 * Upd: ready, but new build is coming (Rdy + Bld)
 * Stl: stalled -- first build failed. Only update or remove is possible
 *
 * handleFunctionAdd:
 *      if build -> Bld
 *      else     -> Que
 *      start PODs
 *
 * handleFunctionUpdate:
 *      if build -> Upd
 *               start PODs
 *      else     updatePods
 *
 * notifyPodUpdate:
 *      if Bld   doRun(build)
 *               if err   -> Stl
 *               else     -> Blt
 *                           restartPods
 *      elif Upd doRun(build)
 *               if OK    updatePODs
 *               -> Rdy
 *      else     -> Rdy
 *
 */
var fnStates = map[int]string {
	swy.DBFuncStateQue: "preparing",
	swy.DBFuncStateStl: "stalled",
	swy.DBFuncStateBld: "building",
	swy.DBFuncStateBlt: "built", // FIXME -- WTF?
	swy.DBFuncStatePrt: "partial",
	swy.DBFuncStateRdy: "ready",
	swy.DBFuncStateUpd: "updating",
	swy.DBFuncStateTrm: "terminating",
}

type FnCodeDesc struct {
	Lang		string		`bson:"lang"`
	Env		[]string	`bson:"env"`
}

type FnSrcDesc struct {
	Type		string		`bson:"type"`
	Repo		string		`bson:"repo,omitempty"`
	Version		string		`bson:"version"`		// Top commit in the repo
	Code		string		`bson:"-"`
}

type FnEventDesc struct {
	Source		string		`bson:"source"`
	CronTab		string		`bson:"crontab,omitempty"`
	MwareId		string		`bson:"mwid,omitempty"`
	MQueue		string		`bson:"mqueue,omitempty"`
	S3Bucket	string		`bson:"s3bucket,omitempty"`
}

type FnSizeDesc struct {
	Replicas	int		`bson:"replicas"`
	Mem		uint64		`bson:"mem"`
	Tmo		uint64		`bson:"timeout"`
	Burst		uint		`bson:"burst"`
	Rate		uint		`bson:"rate"`
}

type FunctionDesc struct {
	// These objects are kept in Mongo, which requires the below two
	// fields to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`

	SwoId				`bson:",inline"`
	Cookie		string		`bson:"cookie"`		// Some "unique" identifier
	State		int		`bson:"state"`		// Function state
	CronID		int		`bson:"cronid"`		// ID of cron trigger (if present)
	URLCall		bool		`bson:"urlcall"`	// Function is callable via direct URL
	Event		FnEventDesc	`bson:"event"`
	Mware		[]string	`bson:"mware"`
	Code		FnCodeDesc	`bson:"code"`
	Src		FnSrcDesc	`bson:"src"`
	Size		FnSizeDesc	`bson:"size"`
	OneShot		bool		`bson:"oneshot"`
}

var zeroVersion = "0"

func (fi *FnInst)DepName() string {
	dn := "swd-" + fi.fn.Cookie[:32]
	if fi.Build {
		dn += "-bld"
	}
	return dn
}

func (fi *FnInst)Replicas() int32 {
	if fi.Build {
		return 1
	} else {
		return int32(fi.fn.Size.Replicas)
	}
}

/*
 * We may have several instances of Fn running
 * Regular -- this one is up-n-running with the fn ready to run
 * Build -- this is a single replica deployment building the fn
 * Old -- this is Regular, but with the sources of previous version.
 *        In parallel to the Old one we may have one Build instance
 *        running building an Fn from new sources.
 * At some point in time the Old instance gets replaced with the
 * new Regular one.
 */
type FnInst struct {
	Build		bool
	fn		*FunctionDesc
}

func (fn *FunctionDesc) Inst() *FnInst {
	return &FnInst { Build: false, fn: fn }
}

func (fn *FunctionDesc) InstBuild() *FnInst {
	return &FnInst { Build: true, fn: fn }
}

func genFunctionDescJSON(fn *FunctionDesc, fi *FnInst) string {
	jdata, _ := json.Marshal(&swyapi.SwdFunctionDesc{
				PodToken:	fn.Cookie,
				Timeout:	fn.Size.Tmo,
				Build:		fi.Build,
			})

	return string(jdata[:])
}

func getFunctionDesc(tennant string, p_add *swyapi.FunctionAdd) *FunctionDesc {
	fn := &FunctionDesc {
		SwoId: SwoId {
			Tennant: tennant,
			Project: p_add.Project,
			Name:	 p_add.FuncName,
		},
		Event:		FnEventDesc {
			Source:		p_add.Event.Source,
			CronTab:	p_add.Event.CronTab,
			MwareId:	p_add.Event.MwareId,
			MQueue:		p_add.Event.MQueue,
			S3Bucket:	p_add.Event.S3Bucket,
		},
		Src:		FnSrcDesc {
			Type:		p_add.Sources.Type,
			Repo:		p_add.Sources.Repo,
			Code:		p_add.Sources.Code,
		},
		Size:		FnSizeDesc {
			Replicas:	1,
			Mem:		p_add.Size.Memory,
			Tmo:		p_add.Size.Timeout,
			Rate:		p_add.Size.Rate,
			Burst:		p_add.Size.Burst,
		},
		Code:		FnCodeDesc {
			Lang:		p_add.Code.Lang,
			Env:		p_add.Code.Env,
		},
		Mware:	p_add.Mware,
	}

	fn.Cookie = fn.SwoId.Cookie()
	return fn
}

func addFunction(conf *YAMLConf, tennant string, params *swyapi.FunctionAdd) error {
	var err error
	var fn *FunctionDesc
	var fi *FnInst

	err = swy.ValidateProjectAndFuncName(params.Project, params.FuncName)
	if err != nil {
		goto out
	}

	if !RtLangEnabled(params.Code.Lang) {
		err = errors.New("Unsupported language")
		goto out
	}

	fn = getFunctionDesc(tennant, params)
	if RtBuilding(&fn.Code) {
		fn.State = swy.DBFuncStateBld
	} else {
		fn.State = swy.DBFuncStateQue
	}

	log.Debugf("function/add %s (cookie %s)", fn.SwoId.Str(), fn.Cookie[:32])

	err = dbFuncAdd(fn)
	if err != nil {
		goto out
	}

	if fn.Event.Source != "" {
		err = eventSetup(conf, fn, true)
		if err != nil {
			err = fmt.Errorf("Unable to setup even %s: %s", fn.Event, err.Error())
			goto out_clean_func
		}
	}

	err = getSources(fn)
	if err != nil {
		goto out_clean_evt
	}

	err = dbFuncUpdateAdded(fn)
	if err != nil {
		goto out_clean_repo
	}

	if RtBuilding(&fn.Code) {
		fi = fn.InstBuild()
	} else {
		fi = fn.Inst()
	}

	err = swk8sRun(conf, fn, fi)
	if err != nil {
		goto out_clean_repo
	}

	logSaveEvent(fn, "registered", "")
	return nil

out_clean_repo:
	cleanRepo(fn)
out_clean_evt:
	if fn.Event.Source != "" {
		eventSetup(conf, fn, false)
	}
out_clean_func:
	dbFuncRemove(fn)
out:
	return err
}

func swyFixSize(sz *swyapi.FunctionSize, conf *YAMLConf) error {
	if sz.Timeout == 0 {
		sz.Timeout = conf.Runtime.Timeout.Def * 1000
	} else if sz.Timeout > conf.Runtime.Timeout.Max * 1000 {
		return errors.New("Too big timeout")
	}

	if sz.Memory == 0 {
		sz.Memory = conf.Runtime.Memory.Def
	} else if sz.Memory > conf.Runtime.Memory.Max ||
			sz.Memory < conf.Runtime.Memory.Min {
		return errors.New("Too small/big memory size")
	}

	return nil
}

func updateFunction(conf *YAMLConf, id *SwoId, params *swyapi.FunctionUpdate) error {
	var fn FunctionDesc
	var err error
	var rebuild bool
	var rlfix bool

	update := make(bson.M)

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	// FIXME -- lock other requests :\
	if fn.State != swy.DBFuncStateRdy && fn.State != swy.DBFuncStateStl {
		err = fmt.Errorf("function %s is not running", fn.SwoId.Str())
		goto out
	}

	if params.Code != "" {
		log.Debugf("Will update sources for %s", fn.SwoId.Str())
		err = updateSources(&fn, params)
		if err != nil {
			goto out
		}

		update["src.version"] = fn.Src.Version
		rebuild = RtBuilding(&fn.Code)
	}

	if params.Size != nil {
		err = swyFixSize(params.Size, conf)
		if err != nil {
			goto out
		}

		if fn.Size.Tmo != params.Size.Timeout {
			log.Debugf("Will update tmo for %s", fn.SwoId.Str())
			fn.Size.Tmo = params.Size.Timeout
			update["size.timeout"] = params.Size.Timeout
		}

		if fn.Size.Mem != params.Size.Memory {
			log.Debugf("Will update mem for %s", fn.SwoId.Str())
			fn.Size.Mem = params.Size.Memory
			update["size.mem"] = params.Size.Memory
		}

		if params.Size.Rate != 0 && (params.Size.Rate != fn.Size.Rate ||
						params.Size.Burst != fn.Size.Burst) {
			log.Debugf("Will update ratelimit for %s", fn.SwoId.Str())
			fn.Size.Burst = params.Size.Burst
			fn.Size.Rate = params.Size.Rate
			update["size.rate"] = params.Size.Rate
			update["size.burst"] = params.Size.Burst
			rlfix = true
		}
	}

	if len(update) == 0 {
		log.Debugf("Nothing to update for %s", fn.SwoId.Str())
		goto out
	}

	if rebuild {
		if fn.State == swy.DBFuncStateRdy {
			fn.State = swy.DBFuncStateUpd
		} else {
			fn.State = swy.DBFuncStateBld
		}
	}

	update["state"] = fn.State

	err = dbFuncUpdatePulled(&fn, update)
	if err != nil {
		goto out
	}

	if rlfix {
		fdm := memdGetFn(&fn)
		if fn.Size.Rate != 0 {
			if fdm.crl != nil {
				/* Update */
				fdm.crl.Update(fn.Size.Burst, fn.Size.Rate)
			} else {
				/* Create */
				fdm.crl = xratelimit.MakeRL(fn.Size.Burst, fn.Size.Rate)
			}
		} else {
			/* Remove */
			fdm.crl = nil
		}
	}

	if rebuild {
		log.Debugf("Starting build dep")
		err = swk8sRun(conf, &fn, fn.InstBuild())
	} else {
		log.Debugf("Updating deploy")
		err = swk8sUpdate(conf, &fn)
	}

	if err != nil {
		goto out
	}

	logSaveEvent(&fn, "updated", fmt.Sprintf("to: %s", fn.Src.Version))
out:
	return err
}

func forgetFunction(fn *FunctionDesc) {
	var err error

	if fn.Event.Source != "" {
		err = eventSetup(&conf, fn, false)
		if err != nil {
			log.Errorf("remove event %s error: %s", fn.Event, err.Error())
		}
	}

	memdGone(fn)
	cleanRepo(fn)
	logRemove(fn)
	dbFuncRemove(fn)
}

func removeFunction(conf *YAMLConf, id *SwoId) error {
	var err error
	var fn FunctionDesc

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	// Allow to remove function if only we're in known state,
	// otherwise wait for function building to complete
	err = dbFuncSetStateCond(id, swy.DBFuncStateTrm,
					[]int{swy.DBFuncStateRdy, swy.DBFuncStateStl})
	if err != nil {
		goto out
	}

	if !fn.OneShot && (fn.State != swy.DBFuncStateStl) {
		err = swk8sRemove(conf, &fn, fn.Inst())
		if err != nil {
			log.Errorf("remove deploy error: %s", err.Error())
			goto out
		}
	}

	forgetFunction(&fn)
out:
	return err
}
