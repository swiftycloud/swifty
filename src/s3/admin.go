/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"github.com/gorilla/mux"
	"context"
	"errors"
	"net/http"
	"swifty/s3/mgo"
	"swifty/common/http"
	"swifty/apis/s3"
)

func handleKeys(w http.ResponseWriter, r *http.Request) {
	var err error

	ctx, done := mkContext("keysreq")
	defer done(ctx)

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err = s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "POST":
		handleKeygen(ctx, w, r)
	case "DELETE":
		handleKeydel(ctx, w, r)
	}
}

func handleKeygen(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var akey *s3mgo.AccessKey
	var kg swys3api.KeyGen
	var err error

	err = xhttp.RReq(r, &kg)
	if err != nil {
		goto out
	}

	// FIXME Check for allowed values
	if kg.Namespace == "" {
		err = errors.New("Missing namespace name")
		goto out
	}

	akey, err = genNewAccessKey(ctx, kg.Namespace, kg.Bucket, kg.Lifetime)
	if err != nil {
		goto out
	}

	err = xhttp.Respond(w, &swys3api.KeyGenResult{
			AccessKeyID:	akey.AccessKeyID,
			AccessKeySecret:s3DecryptAccessKeySecret(akey),
			AccID:		akey.AccountObjID.Hex(),
		})
	if err != nil {
		goto out
	}
	return

out:
	log.Errorf("Can't: %s", err.Error())
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleKeydel(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var kd swys3api.KeyDel
	var err error

	err = xhttp.RReq(r, &kd)
	if err != nil {
		goto out
	}

	if kd.AccessKeyID == "" {
		err = errors.New("Missing key")
		goto out
	}

	err = dbRemoveAccessKey(ctx, kd.AccessKeyID)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return
out:
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleNotify(w http.ResponseWriter, r *http.Request) {
	var params swys3api.Subscribe

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	ctx, done := mkContext("notifyreq")
	defer done(ctx)

	/* For now make it admin-only op */
	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	err = xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	switch r.Method {
	case "POST":
		err = s3Subscribe(ctx, &params)
	case "DELETE":
		err = s3Unsubscribe(ctx, &params)
	}

	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusAccepted)
	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	var st *s3mgo.AcctStats

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	ctx, done := mkContext("statsreq")
	defer done(ctx)
	ns := mux.Vars(r)["ns"]
	log.Debugf("Getting stats for %s", ns)

	act, err := s3AccountFind(ctx, ns)
	if err != nil {
		http.Error(w, "No such namespace", http.StatusNotFound)
		return
	}

	st, err = StatsFindFor(ctx, act)
	if err != nil {
		http.Error(w, "Error getting stats", http.StatusInternalServerError)
		return
	}

	err = xhttp.Respond(w, &swys3api.AcctStats{
		CntObjects:	st.CntObjects,
		CntBytes:	st.CntBytes,
		OutBytes:	st.OutBytes,
		OutBytesWeb:	st.OutBytesWeb,
	})
	if err != nil {
		http.Error(w, "Bad response", http.StatusNoContent)
	}
}

func handleLimits(w http.ResponseWriter, r *http.Request) {
	var lim swys3api.AcctLimits

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	err = xhttp.RReq(r, &lim)
	if err != nil {
		http.Error(w, "Cannot read limits", http.StatusBadRequest)
		return
	}

	ctx, done := mkContext("statsreq")
	defer done(ctx)
	ns := mux.Vars(r)["ns"]
	log.Debugf("Setting limits for %s", ns)

	act, err := s3AccountFind(ctx, ns)
	if err != nil {
		http.Error(w, "No such namespace", http.StatusNotFound)
		return
	}

	err = LimitsSetFor(ctx, act, &lim)
	if err != nil {
		log.Errorf("Error setting limits: %s", err.Error())
		http.Error(w, "Error setting limits", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

