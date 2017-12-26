package main

import (
	"fmt"
	"net/http"
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
	"../common"
	"../common/http"
	"../apis/apps/s3"
)

func s3KeyGen(conf *YAMLConfS3, addr, namespace, bucket string) (string, string, error) {
	resp, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/admin/keygen",
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.Token]},
		},
		&swys3api.S3CtlKeyGen{
			Namespace: namespace,
			Bucket: bucket,
		})
	if err != nil {
		return "", "", fmt.Errorf("Error requesting NS from S3: %s", err.Error())
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Bad responce from S3 gate: %s", string(resp.Status))
	}

	var out swys3api.S3CtlKeyGenResult

	err = swyhttp.ReadAndUnmarshalResp(resp, &out)
	if err != nil {
		return "", "", fmt.Errorf("Error reading responce from S3: %s", err.Error())
	}

	return out.AccessKeyID, out.AccessKeySecret, nil
}

func s3KeyDel(conf *YAMLConfS3, addr, key string) error {
	_, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/admin/keydel",
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.Token]},
		},
		&swys3api.S3CtlKeyDel{
			AccessKeyID: key,
		})
	if err != nil {
		return fmt.Errorf("Error deleting key from S3: %s", err.Error())
	}

	return nil
}

func s3BucketReq(conf *YAMLConfS3, addr, req, namespace, bucket string) error {
	var code int
	if req == "badd" {
		code = http.StatusCreated
	} else {
		code = http.StatusNoContent
	}

	_, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/admin/" + req,
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.Token]},
			Success: code,
		},
		&swys3api.S3CtlBucketReq{
			Namespace: namespace,
			Bucket: bucket,
		})
	if err != nil {
		return fmt.Errorf("Error %s bucket for S3: %s", req, err.Error())
	}

	return nil
}

func InitS3(conf *YAMLConfMw, mwd *MwareDesc) (error) {
	/* There can be only one s3 namespace per project */
	pns := mwd.SwoId.Tennant + "::" + mwd.SwoId.Project
	addr := swy.MakeAdminURL(conf.S3.Addr, conf.S3.AdminPort)

	key, secret, err := s3KeyGen(&conf.S3, addr, pns, mwd.SwoId.Name)
	if err != nil {
		return err
	}

	err = s3BucketReq(&conf.S3, addr, "badd", pns, mwd.SwoId.Name)
	if err != nil {
		s3KeyDel(&conf.S3, addr, key)
		return err
	}

	log.Debugf("Added S3 client: %s:%s", key, pns)
	mwd.Client = key
	mwd.Secret = secret
	mwd.Namespace = pns
	return nil
}

func FiniS3(conf *YAMLConfMw, mwd *MwareDesc) error {
	pns := mwd.Namespace
	addr := swy.MakeAdminURL(conf.S3.Addr, conf.S3.AdminPort)

	er1 := s3BucketReq(&conf.S3, addr, "bdel", pns, mwd.SwoId.Name)
	er2 := s3KeyDel(&conf.S3, addr, mwd.Client)
	if er1 == nil {
		er1 = er2
	}
	return er1
}

const (
	gates3queue = "events"
)

func s3Subscribe(conf *YAMLConfMw, namespace, bucket string) error {
	addr := swy.MakeAdminURL(conf.S3.Addr, conf.S3.AdminPort)
	_, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/notify/subscribe",
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.S3.Token]},
			Success: http.StatusAccepted,
		},
		&swys3api.S3Subscribe{
			Namespace: namespace,
			Bucket: bucket,
			Ops: "put",
			Queue: gates3queue,
		})
	if err != nil {
		return fmt.Errorf("Error subscibing: %s", err.Error())
	}

	return nil
}

func s3Unsubscribe(conf *YAMLConfMw, namespace, bucket string) {
	addr := swy.MakeAdminURL(conf.S3.Addr, conf.S3.AdminPort)
	_, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/notify/unsubscribe",
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.S3.Token]},
			Success: http.StatusAccepted,
		},
		&swys3api.S3Subscribe{
			Namespace: namespace,
			Bucket: bucket,
		})
	if err != nil {
		log.Errorf("Error unsubscibing: %s", err.Error())
	}
}

func handleS3Event(user string, data []byte) {
	var evt swys3api.S3Event

	err := json.Unmarshal(data, &evt)
	if err != nil {
		log.Errorf("Invalid event from S3")
		return
	}

	mw, err := dbMwareGetOne(bson.M{"mwaretype": "s3", "namespace": evt.Namespace})
	if err != nil {
		log.Errorf("No S3 mware for ns %s", evt.Namespace)
		return
	}

	funcs, err := dbFuncListMwEvent(&mw.SwoId, bson.M {
		"event.source": "mware",
		"event.mwid": mw.SwoId.Name,
		"event.s3bucket": evt.Bucket,
	})
	if err != nil {
		/* FIXME -- this should be notified? Or what? */
		log.Errorf("mq: Can't list functions for s3 event")
		return
	}

	for _, fn := range funcs {
		log.Debugf("s3 event -> [%s]", fn.SwoId.Str())
		/* FIXME -- this is synchronous */
		_, err := doRun(&fn, "mware:" + mw.SwoId.Name + ":" + evt.Bucket,
				map[string]string {
					"bucket": evt.Bucket,
					"object": evt.Object,
					"op": evt.Op,
				})
		if err != nil {
			log.Errorf("mq: Error running FN %s", err.Error())
		}
	}
}

func EventS3(conf *YAMLConfMw, source *FnEventDesc, mwd *MwareDesc, on bool) (error) {
	if on {
		err := mqStartListener(conf.S3.Notify.User, conf.S3.Notify.Pass,
				conf.S3.Notify.URL, gates3queue, handleS3Event)
		if err == nil {
			err = s3Subscribe(conf, mwd.Namespace, source.S3Bucket)
			if err != nil {
				mqStopListener(conf.S3.Notify.URL, gates3queue)
			}
		}

		return err
	} else {
		s3Unsubscribe(conf, mwd.Namespace, source.S3Bucket)
		mqStopListener(conf.S3.Notify.URL, "events")
		return nil
	}
}

func GetEnvS3(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string) {
	var ret [][2]string
	ret = append(ret, mkEnv(mwd, "ADDR", conf.S3.Addr))
	ret = append(ret, mkEnv(mwd, "S3KEY", mwd.Client))
	ret = append(ret, mkEnv(mwd, "S3SEC", mwd.Secret))
	return ret
}

var MwareS3 = MwareOps {
	Init:	InitS3,
	Fini:	FiniS3,
	Event:	EventS3,
	GetEnv:	GetEnvS3,
}
