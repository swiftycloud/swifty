package main

import (
	"github.com/gorilla/websocket"
	"context"
	"net/http"
	"strconv"
	"sync"
	"swifty/apis"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/xrest"
)

func InitWebSocket(ctx context.Context, mwd *MwareDesc) (error) {
	var err error

	mwd.Secret, err = xh.GenRandId(32)
	if err != nil {
		return err
	}

	return nil
}

func FiniWebSocket(ctx context.Context, mwd *MwareDesc) error {
	wsCloseConns(mwd.Cookie)
	return nil
}

func GetEnvWebSocket(ctx context.Context, mwd *MwareDesc) map[string][]byte {
	return map[string][]byte{
		mwd.envName("TOKEN"):	[]byte(mwd.Secret),
		mwd.envName("URL"):	[]byte(wsURL(mwd)),
	}
}

func wsURL(mwd *MwareDesc) string {
	url := conf.Daemon.WSGate
	if url == "" {
		url = conf.Daemon.Addr
	}
	return url + "/websockets/" + mwd.Cookie
}

func InfoWebSocket(ctx context.Context, mwd *MwareDesc, ifo *swyapi.MwareInfo) error {
	url := wsURL(mwd)
	ifo.URL = &url
	return nil
}

var MwareWebSocket = MwareOps {
	Init:	InitWebSocket,
	Fini:	FiniWebSocket,
	GetEnv:	GetEnvWebSocket,
	Info:	InfoWebSocket,
	Devel:	true,
}

type wsConnMap struct {
	lock	sync.RWMutex
	cons	map[string]*websocket.Conn
	rover	int64
}

var wsConns sync.Map

func wsAddConn(lid string, c *websocket.Conn) string {
	aux, ok := wsConns.Load(lid)
	if !ok {
		aux, _ = wsConns.LoadOrStore(lid, &wsConnMap{cons: make(map[string]*websocket.Conn)})
	}

	wcs := aux.(*wsConnMap)

	wcs.lock.Lock()
	wcs.rover += 1
	cid := strconv.FormatInt(wcs.rover, 16)
	wcs.cons[cid] = c
	wcs.lock.Unlock()

	return cid
}

func wsDelConn(lid string, cid string) {
	aux, ok := wsConns.Load(lid)
	if !ok {
		glog.Errorf("Deleting conn from no-list!")
		return /* Shouldn't happen */
	}

	wcs := aux.(*wsConnMap)

	wcs.lock.Lock()
	delete(wcs.cons, cid)
	wcs.lock.Unlock()
}

func wsCloseConns(lid string) {
	aux, ok := wsConns.Load(lid)
	if !ok {
		return
	}

	wsConns.Delete(lid)

	wcs := aux.(*wsConnMap)

	wcs.lock.Lock()
	for _, c := range wcs.cons {
		c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
		c.Close()
	}
	wcs.lock.Unlock()
}

func wsUnicastMessage(ctx context.Context, cid string, wcs *wsConnMap, rq *swyapi.WsMwReq) *xrest.ReqErr {

	c, ok := wcs.cons[cid]
	if ok {
		err := c.WriteMessage(rq.MType, rq.Msg)
		if err != nil {
			; /* XXX What? */
		}
	}

	if !ok {
		return GateErrM(swyapi.GateNotFound, "Target not found")
	}

	return nil
}

func wsBroadcastMessage(ctx context.Context, wcs *wsConnMap, rq *swyapi.WsMwReq) *xrest.ReqErr {

	for _, c := range wcs.cons {
		err := c.WriteMessage(rq.MType, rq.Msg)
		if err != nil {
			; /* XXX What? */
		}
	}

	return nil
}

func wsFunctionReq(ctx context.Context, mwd *MwareDesc, cid string, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var rq swyapi.WsMwReq

	err := xhttp.RReq(r, &rq)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	aux, ok := wsConns.Load(mwd.Cookie)
	if !ok {
		return GateErrM(swyapi.GateNotFound, "Target not found")
	}

	wcs := aux.(*wsConnMap)

	var cerr *xrest.ReqErr

	wcs.lock.RLock()
	defer wcs.lock.RUnlock()

	if cid != "" {
		cerr = wsUnicastMessage(ctx, cid, wcs, &rq)
	} else {
		cerr = wsBroadcastMessage(ctx, wcs, &rq)
	}

	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func wsClientReq(mwd *MwareDesc, c *websocket.Conn) {
	defer c.Close() /* XXX -- will it race OK with wsCloseConns()? */

	cid := wsAddConn(mwd.Cookie, c)
	defer wsDelConn(mwd.Cookie, cid)

	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			glog.Errorf("WS read: %s", err.Error())
			break
		}

		/* XXX -- trigger FN here */
	}
}
