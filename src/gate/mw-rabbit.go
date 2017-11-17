package main

import (
	"net/http"
	"encoding/json"
	"github.com/michaelklishin/rabbit-hole"
	"fmt"
	"strings"

	"../apis/apps"
)

type MQSettings struct {
	Vhost		string			`json:"vhost"`
}

func rabbitConn(conf *YAMLConf) (*rabbithole.Client, error) {
	addr := strings.Split(conf.Mware.MQ.Addr, ":")[0] + ":" + conf.Mware.MQ.AdminPort
	return rabbithole.NewClient("http://" + addr, conf.Mware.MQ.User, conf.Mware.MQ.Pass)
}

func rabbitErr(resp *http.Response, err error) error {
	if err != nil {
		return err
	} else if resp.StatusCode != http.StatusCreated &&
			resp.StatusCode != http.StatusNoContent &&
			resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	} else {
		return nil
	}
}

func InitRabbitMQ(conf *YAMLConf, mwd *MwareDesc, mware *swyapi.MwareItem) ([]byte, error) {
	rmq := MQSettings{ }

	err := mwareGenerateClient(mwd)
	if err != nil {
		return nil, err
	}

	rmq.Vhost = mwd.Client

	rmqc, err := rabbitConn(conf)
	if err != nil {
		return nil, err
	}

	err = rabbitErr(rmqc.PutUser(mwd.Client, rabbithole.UserSettings{Password: mwd.Pass}))
	if err != nil {
		return nil, fmt.Errorf("Can't create user %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.PutVhost(rmq.Vhost, rabbithole.VhostSettings{Tracing: false}))
	if err != nil {
		return nil, fmt.Errorf("Can't create vhost %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.UpdatePermissionsIn(rmq.Vhost, mwd.Client,
			rabbithole.Permissions{Configure: ".*", Write: ".*", Read: ".*"}))
	if err != nil {
		return nil, fmt.Errorf("Can't set permissions %s: %s", mwd.Client, err.Error())
	}

	/* Add permissions for us as well, just in case event listening is required */
	err = rabbitErr(rmqc.UpdatePermissionsIn(rmq.Vhost, conf.Mware.MQ.User,
			rabbithole.Permissions{Configure: ".*", Write: ".*", Read: ".*"}))
	if err != nil {
		return nil, fmt.Errorf("Can't set permissions %s: %s", mwd.Client, err.Error())
	}

	return json.Marshal(&rmq)
}

func FiniRabbitMQ(conf *YAMLConf, mwd *MwareDesc) error {
	var rmq MQSettings

	err := json.Unmarshal([]byte(mwd.JSettings), &rmq)
	if err != nil {
		return fmt.Errorf("rabbit: Can't unmarshal data %s: %s",
					mwd.JSettings, err.Error())
	}

	rmqc, err := rabbitConn(conf)
	if err != nil {
		return err
	}

	err = rabbitErr(rmqc.DeleteVhost(rmq.Vhost))
	if err != nil {
		log.Errorf("rabbit: can't delete vhost %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.DeleteUser(mwd.Client))
	if err != nil {
		log.Errorf("rabbit: can't delete user %s: %s", mwd.Client, err.Error())
	}

	return nil
}

func EventRabbitMQ(conf *YAMLConf, source *FnEventDesc, mwd *MwareDesc, on bool) (error) {
	var rmq MQSettings

	_ = json.Unmarshal([]byte(mwd.JSettings), &rmq)
	if on {
		return mqStartListener(conf, rmq.Vhost, source.MQueue)
	} else {
		mqStopListener(rmq.Vhost, source.MQueue)
		return nil
	}
}

func GetEnvRabbitMQ(conf *YAMLConf, mwd *MwareDesc) ([]string) {
	var rmq MQSettings
	var envs []string
	var err error

	err = json.Unmarshal([]byte(mwd.JSettings), &rmq)
	if err == nil {
		envs = append(mwGenEnvs(mwd, conf.Mware.MQ.Addr), mkEnv(mwd, "VHOST", rmq.Vhost))
	} else {
		log.Fatal("rabbit: Can't unmarshal DB entry %s", mwd.JSettings)
	}

	return envs
}

var MwareRabbitMQ = MwareOps {
	Init:	InitRabbitMQ,
	Fini:	FiniRabbitMQ,
	Event:	EventRabbitMQ,
	GetEnv:	GetEnvRabbitMQ,
}

