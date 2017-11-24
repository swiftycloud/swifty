package main

import (
	"errors"
	"fmt"

	"../apis/apps"
	"../common"
)

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

	statsStartCollect(conf, fn)

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

func updateFunction(conf *YAMLConf, id *SwoId) error {
	var fn FunctionDesc
	var err error

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	// FIXME -- lock other requests :\
	if fn.State != swy.DBFuncStateRdy && fn.State != swy.DBFuncStateStl {
		err = fmt.Errorf("function %s is not running", fn.SwoId.Str())
		goto out
	}

	err = updateSources(&fn)
	if err != nil {
		goto out
	}

	if RtBuilding(&fn.Code) {
		if fn.State == swy.DBFuncStateRdy {
			fn.State = swy.DBFuncStateUpd
		} else {
			fn.State = swy.DBFuncStateBld
		}
	}

	err = dbFuncUpdatePulled(&fn)
	if err != nil {
		goto out
	}

	if RtBuilding(&fn.Code) {
		log.Debugf("Starting build dep")
		err = swk8sRun(conf, &fn, fn.InstBuild())
	} else {
		log.Debugf("Updating deploy")
		err = swk8sUpdate(conf, &fn)
	}

	if err != nil {
		goto out
	}

	logSaveEvent(&fn, "updated", fmt.Sprintf("to: %s", fn.Src.Commit))
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

	statsStopCollect(&conf, fn)
	cleanRepo(fn)
	logRemove(fn)
	dbFuncRemove(fn)
}

func removeFunction(conf *YAMLConf, id *SwoId) error {
	var err error
	var fn FunctionDesc

	// Allow to remove function if only we're in known state,
	// otherwise wait for function building to complete
	err = dbFuncSetStateCond(id, swy.DBFuncStateTrm,
					[]int{swy.DBFuncStateRdy, swy.DBFuncStateStl})
	if err != nil {
		goto out
	}

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	if !fn.OneShot {
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
