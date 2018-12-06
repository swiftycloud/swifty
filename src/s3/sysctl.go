package main

import (
	"context"
	"swifty/common/xrest/sysctl"
	"swifty/common/xrest"
	"net/http"
)

func handleSysctls(w http.ResponseWriter, r *http.Request) {
	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	cer := xrest.HandleMany(context.Background(), w, r, sysctl.Sysctls{}, nil)
	if cer != nil {
		http.Error(w, cer.Message, http.StatusBadRequest)
	}
}

func handleSysctl(w http.ResponseWriter, r *http.Request) {
	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var upd string
	cer := xrest.HandleOne(context.Background(), w, r, sysctl.Sysctls{}, &upd)
	if cer != nil {
		http.Error(w, cer.Message, http.StatusBadRequest)
	}
}
