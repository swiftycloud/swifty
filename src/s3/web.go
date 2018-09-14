package main

import (
	"context"
	"github.com/gorilla/mux"
	"gopkg.in/mgo.v2/bson"
	"path/filepath"
	"net/http"
	"strings"
	"io/ioutil"
	"encoding/xml"
	"fmt"
	"errors"
	"./mgo"
	"../common/http"
	"../apis/s3"
)

func handleGetWebsite(ctx context.Context, bname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxMayAccess(ctx, bname) {
		return &S3Error{ ErrorCode: S3ErrAccessDenied }
	}
	if !ctxAllowed(ctx, S3P_GetBucketWebsite) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	b, err := FindBucket(ctx, bname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrNoSuchBucket }
	}

	ws, err := s3WebsiteLookup(ctx, b)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidAction }
	}

	resp := swys3api.S3WebsiteConfig {
		IndexDoc: swys3api.S3WebIndex {
			Suff: ws.IdxDoc,
		},
		ErrDoc: swys3api.S3WebErrDoc {
			Key: ws.ErrDoc,
		},
	}

	HTTPRespXML(w, resp)
	return nil
}

func handlePutWebsite(ctx context.Context, bname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxMayAccess(ctx, bname) {
		return &S3Error{ ErrorCode: S3ErrAccessDenied }
	}
	if !ctxAllowed(ctx, S3P_PutBucketWebsite) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrIncompleteBody }
	}

	var cfg swys3api.S3WebsiteConfig

	err = xml.Unmarshal(body, &cfg)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrMissingRequestBodyError }
	}

	b, err := FindBucket(ctx, bname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrNoSuchBucket }
	}

	_, err = s3WebsiteInsert(ctx, b, &cfg)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	return nil
}

func handleDelWebsite(ctx context.Context, bname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxMayAccess(ctx, bname) {
		return &S3Error{ ErrorCode: S3ErrAccessDenied }
	}
	if !ctxAllowed(ctx, S3P_DeleteBucketWebsite) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	b, err := FindBucket(ctx, bname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrNoSuchBucket }
	}

	ws, err := s3WebsiteLookup(ctx, b)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidAction }
	}

	dbS3Remove(ctx, ws)
	return nil
}

var webRoot string

func handleWebReq(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	ctx, done := mkContext("web")
	defer done(ctx)

	host := strings.SplitN(r.Host, ":", 2)[0]
	if !strings.HasSuffix(host, webRoot) {
		http.Error(w, "", 502)
		return
	}

	subdom := strings.TrimSuffix(host, webRoot)
	aux := strings.SplitN(subdom, ".", 3)
	if len(aux) != 3  || !bson.IsObjectIdHex(aux[1]) {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	var account s3mgo.S3Account
	query := bson.M{ "_id": bson.ObjectIdHex(aux[1]), "state": S3StateActive }
	err := dbS3FindOne(ctx, query, &account)
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	var ws S3Website
	query = bson.M{ "bcookie": account.BCookie(aux[0]), "state": S3StateActive }
	err = dbS3FindOne(ctx, query, &ws)
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	iam := &s3mgo.S3Iam {
		State:		S3StateActive,
		AccountObjID:	account.ObjID, /* FIXME -- cache account object here
						* to speed-up the s3AccountLookup()
						*/
		Policy:		*getWebPolicy(aux[0]),
	}

	oname := r.URL.Path[1:]
	if oname == "" {
		oname = ws.index()
	} else {
		if strings.HasSuffix(oname, "/") {
			oname += ws.index()
		}
	}

	ext := filepath.Ext(oname)
	if ext != "" {
		log.Debugf("Ext: %s", ext)
		mime, ok := mimes[ext[1:]]
		if ok {
			log.Debugf("Mime: %s", mime)
			ctx.(*s3Context).mime = mime
		}
	}

	ctxAuthorize(ctx, iam)
	serr := handleObject(ctx, w, r, aux[0], oname)
	if serr != nil {
		if serr.ErrorCode != S3ErrNoSuchKey {
			http.Error(w, serr.Message, http.StatusInternalServerError)
			return
		}

		if ws.ErrDoc != "" {
			/* Try to report back the 4xx.html page */
			ctx.(*s3Context).errCode = http.StatusNotFound
			serr = handleObject(ctx, w, r, aux[0], ws.ErrDoc)
			if serr == nil {
				return
			}
		}

		http.Error(w, "", http.StatusNotFound)
	}
}

func handleAdminOp(w http.ResponseWriter, r *http.Request) {
	var op string = mux.Vars(r)["op"]
	var err error

	ctx, done := mkContext("adminreq")
	defer done(ctx)

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err = s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	switch op {
	case "keygen":
		handleKeygen(ctx, w, r)
		return
	case "keydel":
		handleKeydel(ctx, w, r)
		return
	}

	http.Error(w, fmt.Sprintf("Unknown operation"), http.StatusBadRequest)
}

var mimes map[string]string

func webReadMimes(path string) error {
	if path == "" {
		log.Debugf("No mime-types file given")
		return nil
	}

	types, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	mimes = make(map[string]string)
	for _, ln := range(strings.Split(string(types), "\n")) {
		if ln == "" {
			break
		}
		mtyp := strings.Fields(ln)
		if len(mtyp) < 2 {
			return errors.New("Parse error: " + ln)
		}
		for _, ext := range(mtyp[1:]) {
			mimes[ext] = mtyp[0]
		}
	}

	return nil
}

