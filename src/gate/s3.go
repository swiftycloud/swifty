package main

import (
	"strings"
	"path/filepath"
	"fmt"
	"context"
	"net/http"
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
	"../common/http"
	"../apis"
	"../apis/s3"
	"../common/xrest"
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

func s3KeyGen(conf *YAMLConfS3, namespace, bucket string, lifetime uint32) (*swys3api.KeyGenResult, error) {
	addr := conf.c.Addr()

	resp, err := xhttp.MarshalAndPost(
		&xhttp.RestReq{
			Method:  "POST",
			Address: "http://" + addr + "/v1/api/keys",
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.c.Pass]},
		},
		&swys3api.KeyGen{
			Namespace: namespace,
			Bucket: bucket,
			Lifetime: lifetime,
		})
	if err != nil {
		return nil, fmt.Errorf("Error requesting NS from S3: %s", err.Error())
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bad responce from S3 gate: %s", string(resp.Status))
	}

	var out swys3api.KeyGenResult

	err = xhttp.ReadAndUnmarshalResp(resp, &out)
	if err != nil {
		return nil, fmt.Errorf("Error reading responce from S3: %s", err.Error())
	}

	return &out, nil
}

func s3KeyDel(conf *YAMLConfS3, key string) error {
	addr := conf.c.Addr()

	_, err := xhttp.MarshalAndPost(
		&xhttp.RestReq{
			Method:  "DELETE",
			Address: "http://" + addr + "/v1/api/keys",
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.c.Pass]},
		},
		&swys3api.KeyDel{
			AccessKeyID: key,
		})
	if err != nil {
		return fmt.Errorf("Error deleting key from S3: %s", err.Error())
	}

	return nil
}

const (
	gates3queue = "events"
)

func s3Subscribe(ctx context.Context, conf *YAMLConfMw, evt *FnEventS3) error {
	addr := conf.S3.c.Addr()

	_, err := xhttp.MarshalAndPost(
		&xhttp.RestReq{
			Method:  "POST",
			Address: "http://" + addr + "/v1/api/notify",
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.S3.c.Pass]},
			Success: http.StatusAccepted,
		},
		&swys3api.Subscribe{
			Namespace: evt.Ns,
			Bucket: evt.Bucket,
			Ops: evt.Ops,
			Queue: gates3queue,
		})
	if err != nil {
		return fmt.Errorf("Error subscibing: %s", err.Error())
	}

	return nil
}

func s3Unsubscribe(ctx context.Context, conf *YAMLConfMw, evt *FnEventS3) error {
	addr := conf.S3.c.Addr()

	_, err := xhttp.MarshalAndPost(
		&xhttp.RestReq{
			Method:  "DELETE",
			Address: "http://" + addr + "/v1/api/notify",
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.S3.c.Pass]},
			Success: http.StatusAccepted,
		},
		&swys3api.Subscribe{
			Namespace: evt.Ns,
			Bucket: evt.Bucket,
			Ops: evt.Ops,
		})
	if err != nil {
		ctxlog(ctx).Errorf("Error unsubscibing: %s", err.Error())
	}
	return err
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

		/* FIXME -- this is synchronous */
		_, err = doRun(ctx, &fn, "s3",
				&swyapi.SwdFunctionRun{Args: map[string]string {
					"bucket": evt.Bucket,
					"object": evt.Object,
					"op": evt.Op,
				}})
		if err != nil {
			ctxlog(ctx).Errorf("s3: Error running FN %s", err.Error())
		}
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

	id := ctxSwoId(ctx, acc.Project, "")
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
