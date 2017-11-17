package main

import (
	"time"
	"container/list"
	"os"
	"io/ioutil"
	"strings"
	"encoding/json"
	"../apis/apps"
	"../common"
)

var statsPodPath = "/swystats"
var statsUpdateTimeout = 600 * time.Second

type FnStats struct {
	Called		uint64
}

type fnStats struct {
	st	FnStats
	path	string
	l	*list.Element
}

type statsUpdReq struct {
	Req	int
	Id	string
	Path	string
	Resp	chan FnStats
}

func fnStatsDir(conf *YAMLConf, fn *FunctionDesc) string {
	/* The statsStopCollect should be aligned with this */
	return conf.Daemon.Sources.Share + "/stats/" + fn.Cookie
}

func statsGet(fn *FunctionDesc) *FnStats {
	req := statsUpdReq{Req: 3, Id: fn.Cookie, Resp: make(chan FnStats) }
	statsUpdater <- req
	s := <-req.Resp
	return &s
}

func statsStartCollect(conf *YAMLConf, fn *FunctionDesc) {
	stDir := fnStatsDir(conf, fn)
	os.Mkdir(stDir, 0777)
	statsUpdater <- statsUpdReq{Req: 1, Id: fn.Cookie, Path: stDir}
}

func statsStopCollect(conf *YAMLConf, fn *FunctionDesc) {
	statsUpdater <- statsUpdReq{Req: 2, Id: fn.Cookie}
	/*
	 * The updated might not have yet finished with this one, but
	 * since it just ignores all the errors we don't care.
	 */
	swy.DropDir(conf.Daemon.Sources.Share + "/stats/", fn.Cookie)
}

func updateStats(st *fnStats) {
	newst := FnStats{}

	dir, err := os.Open(st.path)
	if err != nil {
		return
	}

	stents, err := dir.Readdir(-1)
	dir.Close()
	if err != nil {
		return
	}

	for _, sf := range stents {
		sfName := sf.Name()
		if strings.HasPrefix(sfName, ".") {
			continue
		}

		data, err := ioutil.ReadFile(st.path + "/" + sfName)
		if err != nil {
			continue
		}

		var lst swyapi.SwdStats
		json.Unmarshal(data, &lst)

		newst.Called += lst.Called
	}

	st.st = newst
}

var statsUpdater chan statsUpdReq

func statsRestart(conf *YAMLConf) error {
	fns, err := dbFuncList()
	if err != nil {
		return err
	}

	for _, fn := range fns {
		statsStartCollect(conf, &fn)
	}

	return nil
}

func statsInit(conf *YAMLConf) error {
	statsUpdater = make(chan statsUpdReq)
	stats := make(map[string]*fnStats)
	statsL := list.New()
	go func() {
		for {
			select {
			case req := <-statsUpdater:
				switch req.Req {
				case 1:
					statsUpdateTimeout = 1 * time.Second
					el := statsL.PushBack(req.Id)
					stats[req.Id] = &fnStats{path: req.Path, l: el}
				case 2:
					s, ok := stats[req.Id]
					if ok {
						statsL.Remove(s.l)
						delete(stats, req.Id)
						if statsL.Len() == 0 {
							statsUpdateTimeout = 600 * time.Second
						}
					}
				case 3:
					s, ok := stats[req.Id]
					if ok {
						req.Resp <- s.st
					} else {
						req.Resp <- FnStats{}
					}
				}
			case <- time.After(statsUpdateTimeout):
				el := statsL.Front()
				if el != nil {
					id := el.Value.(string)
					updateStats(stats[id])
					statsL.MoveToBack(el)
				}
			}
		}
	}()

	return statsRestart(conf)
}
