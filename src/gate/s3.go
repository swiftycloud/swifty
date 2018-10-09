package main

import (
	"strings"
	"path/filepath"
	"fmt"
	"context"
	"net/http"
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
	"swifty/common/http"
	"swifty/apis"
	"swifty/apis/s3"
	"swifty/common/xrest"
)

type FnEventS3 struct {
	Ns		string		`bson:"ns"`
	Bucket		string		`bson:"bucket"`
	Ops		string		`bson:"ops"`
	Pattern		string		`bson:"pattern"`
}

func (s3 *FnEventS3)hasOp(op string) bool {
	ops := strings.Split(s3.Ops, ",")
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

func (s3 *FnEventS3)matchPattern(oname string) bool {
	if s3.Pattern == "" {
		return true
	}

	m, err := filepath.Match(s3.Pattern, oname)
	return err == nil && m
}

func s3Call(rq *xhttp.RestReq, in interface{}, out interface{}) error {
	err, _ := s3Call2(rq, in, out)
	return err
}

func s3Call2(rq *xhttp.RestReq, in interface{}, out interface{}) (error, int) {
	addr := conf.Mware.S3.c.Addr()
	rq.Address = "http://" + addr + rq.Address
	rq.Timeout = 120
	rq.Headers = map[string]string{"X-SwyS3-Token": gateSecrets[conf.Mware.S3.c.Pass]}

	resp, err := xhttp.Req(rq, in)
	if err != nil {
		code := -1
		if resp != nil {
			code = resp.StatusCode
		}
		return fmt.Errorf("Error talking to S3: %s", err.Error()), code
	}

	defer resp.Body.Close()

	if out != nil {
		err = xhttp.RResp(resp, out)
		if err != nil {
			return fmt.Errorf("Error reading responce from S3: %s", err.Error()), -1
		}
	}

	return nil, 0
}

func s3KeyGen(conf *YAMLConfS3, namespace, bucket string, lifetime uint32) (*swys3api.KeyGenResult, error) {
	var out swys3api.KeyGenResult

	err := s3Call(
		&xhttp.RestReq{
			Method: "POST",
			Address: "/v1/api/keys",
		}, &swys3api.KeyGen {
			Namespace: namespace,
			Bucket: bucket,
			Lifetime: lifetime,
		}, &out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

func s3KeyDel(conf *YAMLConfS3, key string) error {
	return s3Call(
		&xhttp.RestReq{
			Method:  "DELETE",
			Address: "/v1/api/keys",
		},
		&swys3api.KeyDel{
			AccessKeyID: key,
		}, nil)
}

const (
	gates3queue = "events"
)

func s3Subscribe(ctx context.Context, conf *YAMLConfMw, evt *FnEventS3) error {
	return s3Call(
		&xhttp.RestReq{
			Method:  "POST",
			Address: "/v1/api/notify",
			Success: http.StatusAccepted,
		},
		&swys3api.Subscribe{
			Namespace: evt.Ns,
			Bucket: evt.Bucket,
			Ops: evt.Ops,
			Queue: gates3queue,
		}, nil)
}

func s3Unsubscribe(ctx context.Context, conf *YAMLConfMw, evt *FnEventS3) error {
	return s3Call(
		&xhttp.RestReq{
			Method:  "DELETE",
			Address: "/v1/api/notify",
		},
		&swys3api.Subscribe{
			Namespace: evt.Ns,
			Bucket: evt.Bucket,
			Ops: evt.Ops,
		}, nil)
}

func handleS3Event(ctx context.Context, user string, data []byte) {
	var evt swys3api.Event

	err := json.Unmarshal(data, &evt)
	if err != nil {
		ctxlog(ctx).Errorf("Invalid event from S3")
		return
	}

	var evs []*FnEventDesc

	err = dbFindAll(ctx, bson.M{"source":"s3", "s3.ns": evt.Namespace, "s3.bucket": evt.Bucket}, &evs)
	if err != nil {
		/* FIXME -- this should be notified? Or what? */
		ctxlog(ctx).Errorf("mq: Can't list triggers for s3 event")
		return
	}

	for _, ed := range evs {
		if !ed.S3.hasOp(evt.Op) {
			continue
		}

		if !ed.S3.matchPattern(evt.Object) {
			continue
		}

		var fn FunctionDesc

		err := dbFind(ctx, bson.M{"cookie": ed.FnId}, &fn)
		if err != nil {
			continue
		}

		if fn.State != DBFuncStateRdy {
			continue
		}

		doRunBg(ctx, &fn, "s3",
				&swyapi.WdogFunctionRun{Args: map[string]string {
					"bucket": evt.Bucket,
					"object": evt.Object,
					"op": evt.Op,
				}})
	}
}

func s3EventStart(ctx context.Context, fn *FunctionDesc, evt *FnEventDesc) error {
	evt.S3.Ns = fn.SwoId.S3Namespace()
	conf := &conf.Mware
	err := mqStartListener(conf.S3.cn.User, conf.S3.cn.Pass,
		conf.S3.cn.Addr() + "/" + conf.S3.cn.Domn,
		gates3queue, handleS3Event)
	if err == nil {
		err = s3Subscribe(ctx, conf, evt.S3)
		if err != nil {
			mqStopListener(conf.S3.cn.Addr() + "/" + conf.S3.cn.Domn, gates3queue)
		}
	}

	return err
}

func s3EventStop(ctx context.Context, evt *FnEventDesc) error {
	conf := &conf.Mware
	err := s3Unsubscribe(ctx, conf, evt.S3)
	if err == nil {
		mqStopListener(conf.S3.cn.Addr() + "/" + conf.S3.cn.Domn, "events")
	}
	return err
}

func s3Endpoint(conf *YAMLConfS3, public bool) string {
	/*
	 * XXX 2 -- functions may go directly to S3 host, but certificates
	 * and routing may kill us
	 */
	return conf.API
}

func GenBucketKeysS3(ctx context.Context, conf *YAMLConfMw, fid *SwoId, bucket string) (map[string]string, error) {
	k, err := s3KeyGen(&conf.S3, fid.S3Namespace(), bucket, 0)
	if err != nil {
		ctxlog(ctx).Errorf("Error generating key for %s/%s: %s", fid.Str(), bucket, err.Error())
		return nil, fmt.Errorf("Key generation error")
	}

	return map[string]string {
		mkEnvName("s3", bucket, "ADDR"):	s3Endpoint(&conf.S3, false),
		mkEnvName("s3", bucket, "KEY"):		k.AccessKeyID,
		mkEnvName("s3", bucket, "SECRET"):	k.AccessKeySecret,
	}, nil
}

func s3GetCreds(ctx context.Context, acc *swyapi.S3Access) (*swyapi.S3Creds, *xrest.ReqErr) {
	creds := &swyapi.S3Creds{}

	creds.Endpoint = s3Endpoint(&conf.Mware.S3, true)
	creds.Expires = acc.Lifetime

	for _, acc := range(acc.Access) {
		if acc == "hidden" {
			creds.Expires = conf.Mware.S3.HiddenKeyTmo
			continue
		}

		return nil, GateErrM(swyapi.GateBadRequest, "Unknown access option " + acc)
	}

	if creds.Expires == 0 {
		return nil, GateErrM(swyapi.GateBadRequest, "Perpetual keys not allowed")
	}

	id := ctxSwoId(ctx, DefaultProject, "")
	k, err := s3KeyGen(&conf.Mware.S3, id.S3Namespace(), acc.Bucket, creds.Expires)
	if err != nil {
		ctxlog(ctx).Errorf("Can't get S3 keys for %s.%s", id.Str(), acc.Bucket, err.Error())
		return nil, GateErrM(swyapi.GateGenErr, "Error getting S3 keys")
	}

	creds.Key = k.AccessKeyID
	creds.Secret = k.AccessKeySecret
	creds.AccID = k.AccID

	return creds, nil
}

var s3EOps = EventOps {
	setup: func(ed *FnEventDesc, evt *swyapi.FunctionEvent) {
		ed.S3 = &FnEventS3{
			Bucket: evt.S3.Bucket,
			Ops: evt.S3.Ops,
			Pattern: evt.S3.Pattern,
		}
	},
	start:	s3EventStart,
	stop:	s3EventStop,
}

func getS3Stats(ctx context.Context) (*swyapi.S3NsStats, *xrest.ReqErr) {
	ns := ctxSwoId(ctx, "", "").S3Namespace()
	var st swys3api.AcctStats

	err, code := s3Call2(
		&xhttp.RestReq{
			Method:  "GET",
			Address: "/v1/api/stats/" + ns,
		}, nil, &st)
	if err != nil {
		if code == http.StatusNotFound {
			return nil, nil
		}

		ctxlog(ctx).Errorf("Error talking to S3: %s", err.Error())
		return nil, GateErrM(swyapi.GateGenErr, "Error talking to S3")
	}

	return &swyapi.S3NsStats{
		CntObjects:	st.CntObjects,
		CntBytes:	st.CntBytes,
		OutBytes:	st.OutBytes,
		OutBytesWeb:	st.OutBytesWeb,
	}, nil
}
