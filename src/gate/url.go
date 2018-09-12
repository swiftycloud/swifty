package main

import (
	"context"
	"gopkg.in/mgo.v2/bson"
	"../apis"
	"sync"
	"net/http"
)

type URL interface {
	Handle(context.Context, http.ResponseWriter, *http.Request, *statsOpaque)
}

var urls sync.Map

type FnURL struct {
	URL
	fd	*FnMemData
}

func urlEvFind(ctx context.Context, urlid string) (*FnEventDesc, error) {
	var ed FnEventDesc
	/* URLid is FN cookie now, but we may allocate random value for it */
	err := dbFind(ctx, bson.M{"fnid": urlid, "source": "url"}, &ed)
	if err != nil {
		return nil, err
	}
	return &ed, err
}

func urlFind(ctx context.Context, urlid string) (URL, error) {
	res, ok := urls.Load(urlid)
	if !ok {
		ed, err := urlEvFind(ctx, urlid)
		if err != nil {
			if dbNF(err) {
				err = nil
			}
			return nil, err
		}

		fdm, err := memdGet(ctx, ed.FnId)
		if err != nil {
			return nil, err
		}

		res, _ = urls.LoadOrStore(urlid, &FnURL{ fd: fdm })
	}

	return res.(URL), nil
}

/* FIXME -- set up public IP address/port for this FN */

func urlEventStart(ctx context.Context, _ *FunctionDesc, ed *FnEventDesc) error {
	return nil /* XXX -- pre-populate urls? */
}

func urlEventStop(ctx context.Context, ed *FnEventDesc) error {
	return nil
}

func urlEventClean(ctx context.Context, ed *FnEventDesc) {
	urls.Delete(ed.FnId)
}

var urlEOps = EventOps {
	setup:	func(ed *FnEventDesc, evt *swyapi.FunctionEvent) { /* nothing to do */ },
	start:	urlEventStart,
	stop:	urlEventStop,
	cleanup:urlEventClean,
}
