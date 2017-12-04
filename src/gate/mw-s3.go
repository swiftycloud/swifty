package main

import (
	"fmt"
	"strings"
	"net/http"
	"../common"
	"../common/http"
	"../apis/apps/s3"
)

func InitS3(conf *YAMLConfMw, mwd *MwareDesc) (error) {
	s3ns, err := swy.GenRandId(32)
	if err != nil {
		return err
	}

	addr := strings.Split(conf.S3.Addr, ":")[0] + ":" + conf.S3.AdminPort
	resp, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/admin/keygen",
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.S3.Token]},
		},
		&swys3ctl.S3CtlKeyGen{
			Namespace: s3ns,
		})
	if err != nil {
		return fmt.Errorf("Error requesting NS from S3: %s", err.Error())
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bad responce from S3 gate: %s", string(resp.Status))
	}

	var out swys3ctl.S3CtlKeyGenResult

	err = swyhttp.ReadAndUnmarshalResp(resp, &out)
	if err != nil {
		return fmt.Errorf("Error reading responce from S3: %s", err.Error())
	}

	log.Debugf("Added S3 client: %s:%s", out.AccessKeyID, s3ns)
	mwd.Client = out.AccessKeyID
	mwd.Secret = out.AccessKeySecret
	mwd.Namespace = s3ns
	return nil
}

func FiniS3(conf *YAMLConfMw, mwd *MwareDesc) error {
	addr := strings.Split(conf.S3.Addr, ":")[0] + ":" + conf.S3.AdminPort
	_, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/admin/keydel",
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.S3.Token]},
		},
		&swys3ctl.S3CtlKeyDel{
			AccessKeyID: mwd.Client,
		})
	if err != nil {
		return fmt.Errorf("Error deleting key from S3: %s", err.Error())
	}

	return nil
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
	GetEnv:	GetEnvS3,
}
