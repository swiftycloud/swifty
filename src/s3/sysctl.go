package main

import (
	"context"
	"swifty/common/xrest/sysctl"
	"swifty/common/xrest"
	"net/http"
)

func handleSysctls(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	cer := xrest.HandleMany(context.Background(), w, r, sysctl.Sysctls{}, nil)
	if cer != nil {
		http.Error(w, cer.Message, http.StatusBadRequest)
	}
}

func handleSysctl(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var upd string
	cer := xrest.HandleOne(context.Background(), w, r, sysctl.Sysctls{}, &upd)
	if cer != nil {
		http.Error(w, cer.Message, http.StatusBadRequest)
	}
}
