/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"context"
	"encoding/json"
	"swifty/apis"
)

func noteThens(ctx context.Context, fmd *FnMemData, then_msg json.RawMessage) {
	var then swyapi.Then

	ctxlog(ctx).Warnf("Function %s wants then %v", fmd.fnid, then_msg)
	err := json.Unmarshal(then_msg, &then)
	if err != nil {
		logSaveEvent(ctx, fmd.fnid, "Bad then value: %s" + err.Error())
		return
	}
}
