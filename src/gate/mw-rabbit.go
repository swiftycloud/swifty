package main

import (
	"net/http"
	"gopkg.in/mgo.v2/bson"
	"github.com/michaelklishin/rabbit-hole"
	"fmt"
	"../common"
)

func rabbitConn(conf *YAMLConfMw) (*rabbithole.Client, error) {
	addr := swy.MakeAdminURL(conf.Rabbit.Addr, conf.Rabbit.AdminPort)
	return rabbithole.NewClient("http://" + addr, conf.Rabbit.Admin, gateSecrets[conf.Rabbit.Pass])
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

func InitRabbitMQ(conf *YAMLConfMw, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(mwd)
	if err != nil {
		return err
	}

	mwd.Namespace = mwd.Client

	rmqc, err := rabbitConn(conf)
	if err != nil {
		return err
	}

	err = rabbitErr(rmqc.PutUser(mwd.Client, rabbithole.UserSettings{Password: mwd.Secret}))
	if err != nil {
		return fmt.Errorf("Can't create user %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.PutVhost(mwd.Namespace, rabbithole.VhostSettings{Tracing: false}))
	if err != nil {
		return fmt.Errorf("Can't create vhost %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.UpdatePermissionsIn(mwd.Namespace, mwd.Client,
			rabbithole.Permissions{Configure: ".*", Write: ".*", Read: ".*"}))
	if err != nil {
		return fmt.Errorf("Can't set permissions %s: %s", mwd.Client, err.Error())
	}

	/* Add permissions for us as well, just in case event listening is required */
	err = rabbitErr(rmqc.UpdatePermissionsIn(mwd.Namespace, conf.Rabbit.Admin,
			rabbithole.Permissions{Configure: ".*", Write: ".*", Read: ".*"}))
	if err != nil {
		return fmt.Errorf("Can't set permissions %s: %s", mwd.Client, err.Error())
	}

	return nil
}

func FiniRabbitMQ(conf *YAMLConfMw, mwd *MwareDesc) error {
	rmqc, err := rabbitConn(conf)
	if err != nil {
		return err
	}

	err = rabbitErr(rmqc.DeleteVhost(mwd.Namespace))
	if err != nil {
		log.Errorf("rabbit: can't delete vhost %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.DeleteUser(mwd.Client))
	if err != nil {
		log.Errorf("rabbit: can't delete user %s: %s", mwd.Client, err.Error())
	}

	return nil
}

func mqEvent(mwid, queue, userid, data string) {
	mware, err := dbMwareGetOne(bson.M{"mwaretype": "rabbit", "client": userid})
	if err != nil {
		return
	}

	log.Debugf("mq: Resolved client to project %s", mware.Project)

	funcs, err := dbFuncListMwEvent(&mware.SwoId, bson.M {
		"event.source": "mware",
		"event.mwid": mware.SwoId.Name,
		"event.mqueue": queue,
	})
	if err != nil {
		/* FIXME -- this should be notified? Or what? */
		log.Errorf("mq: Can't list functions for event")
		return
	}

	for _, fn := range funcs {
		log.Debugf("mq: `- [%s]", fn)
		/* FIXME -- this is synchronous */
		_, err := doRun(fn.Cookie, "mware:" + mwid + ":" + queue, map[string]string{"body": data})
		if err != nil {
			log.Errorf("mq: Error running FN %s", err.Error())
		}
	}
}

func EventRabbitMQ(conf *YAMLConfMw, source *FnEventDesc, mwd *MwareDesc, on bool) (error) {
	if on {
		return mqStartListener(conf.Rabbit.Admin, conf.Rabbit.Pass,
			conf.Rabbit.Addr + "/" + mwd.Namespace, source.MQueue,
			func(userid string, data []byte) {
				if userid != "" {
					mqEvent(mwd.SwoId.Name, source.MQueue, userid, string(data))
				}
			})
	} else {
		mqStopListener(conf.Rabbit.Addr + "/" + mwd.Namespace, source.MQueue)
		return nil
	}
}

func GetEnvRabbitMQ(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string) {
	return append(mwGenUserPassEnvs(mwd, conf.Rabbit.Addr), mkEnv(mwd, "VHOST", mwd.Namespace))
}

var MwareRabbitMQ = MwareOps {
	Init:	InitRabbitMQ,
	Fini:	FiniRabbitMQ,
	Event:	EventRabbitMQ,
	GetEnv:	GetEnvRabbitMQ,
	Devel:	true,
}

