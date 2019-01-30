/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2/bson"
	"context"
	"encoding/json"
	"swifty/apis"
)

func doThenCall(ctx context.Context, fmd *FnMemData, tc *swyapi.ThenCall) {
	ctxlog(ctx).Warnf("Function %s wants chain call [%s(%v)]", fmd.fnid, tc.Name, tc.Args)

	go func() {
		cctx, done := mkContext("::cron")
		defer done(cctx)

		id := fmd.id
		id.Name = tc.Name

		var fn FunctionDesc

		err := dbFind(cctx, bson.M{"cookie": id.Cookie()}, &fn)
		if err != nil {
			danglingEvents.WithLabelValues("then").Inc()
			ctxlog(cctx).Errorf("Can't find FN %s to run then", id.Str())
			return
		}

		if fn.State != DBFuncStateRdy {
			danglingEvents.WithLabelValues("then").Inc()
			return
		}

		doRunBg(cctx, &fn, "then", &swyapi.FunctionRun{Args: tc.Args})
	}()
}

func noteThens(ctx context.Context, fmd *FnMemData, then_msg json.RawMessage) {
	var then swyapi.Then

	err := json.Unmarshal(then_msg, &then)
	if err != nil {
		logSaveEvent(ctx, fmd.fnid, "Bad then value: %s" + err.Error())
		return
	}

	switch {
	case then.Call != nil:
		doThenCall(ctx, fmd, then.Call)
	}
}
