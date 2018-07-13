package main

import (
	"net/http"
	"context"
	"errors"
	"gopkg.in/mgo.v2/bson"
	"github.com/michaelklishin/rabbit-hole"
	"fmt"
	"../apis/apps"
)

func rabbitConn(conf *YAMLConfMw) (*rabbithole.Client, error) {
	addr := conf.Rabbit.c.AddrP(conf.Rabbit.AdminPort)
	return rabbithole.NewClient("http://" + addr, conf.Rabbit.c.User, gateSecrets[conf.Rabbit.c.Pass])
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

func InitRabbitMQ(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(ctx, mwd)
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
	err = rabbitErr(rmqc.UpdatePermissionsIn(mwd.Namespace, conf.Rabbit.c.User,
			rabbithole.Permissions{Configure: ".*", Write: ".*", Read: ".*"}))
	if err != nil {
		return fmt.Errorf("Can't set permissions %s: %s", mwd.Client, err.Error())
	}

	return nil
}

func FiniRabbitMQ(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) error {
	rmqc, err := rabbitConn(conf)
	if err != nil {
		return err
	}

	err = rabbitErr(rmqc.DeleteVhost(mwd.Namespace))
	if err != nil {
		ctxlog(ctx).Errorf("rabbit: can't delete vhost %s: %s", mwd.Client, err.Error())
	}

	err = rabbitErr(rmqc.DeleteUser(mwd.Client))
	if err != nil {
		ctxlog(ctx).Errorf("rabbit: can't delete user %s: %s", mwd.Client, err.Error())
	}

	return nil
}

func mqEvent(ctx context.Context, mwid, queue, userid, data string) {
	var mware MwareDesc
	err := dbFind(ctx, bson.M{"mwaretype": "rabbit", "client": userid}, &mware)
	if err != nil {
		return
	}

	ctxlog(ctx).Debugf("mq: Resolved client to project %s", mware.Project)

	var funcs []*FunctionDesc
	/* FIXME -- list FNs with events here, now they are in separate DB */

	for _, fn := range funcs {
		ctxlog(ctx).Debugf("mq: `- [%s]", fn)
		/* FIXME -- this is synchronous */
		_, err := doRun(ctx, fn, "mq",
				&swyapi.SwdFunctionRun{Body: data})
		if err != nil {
			ctxlog(ctx).Errorf("mq: Error running FN %s", err.Error())
		}
	}
}

func EventRabbitMQ(ctx context.Context, conf *YAMLConfMw, source *FnEventDesc, mwd *MwareDesc, on bool) (error) {
	/*
	if on {
		return mqStartListener(conf.Rabbit.c.User, conf.Rabbit.c.Pass,
			conf.Rabbit.c.Addr() + "/" + mwd.Namespace, source.MQueue,
			func(ctx context.Context, userid string, data []byte) {
				if userid != "" {
					mqEvent(ctx, mwd.SwoId.Name, source.MQueue, userid, string(data))
				}
			})
	} else {
		mqStopListener(conf.Rabbit.c.Addr() + "/" + mwd.Namespace, source.MQueue)
		return nil
	}
	*/
	return errors.New("Not supported")
}

func GetEnvRabbitMQ(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string) {
	return append(mwGenUserPassEnvs(mwd, conf.Rabbit.c.Addr()), mkEnv(mwd, "VHOST", mwd.Namespace))
}

var MwareRabbitMQ = MwareOps {
	Init:	InitRabbitMQ,
	Fini:	FiniRabbitMQ,
	Event:	EventRabbitMQ,
	GetEnv:	GetEnvRabbitMQ,
	Devel:	true,
}

