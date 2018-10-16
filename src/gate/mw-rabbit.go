package main

import (
	"net/http"
	"context"
	"gopkg.in/mgo.v2/bson"
	"github.com/michaelklishin/rabbit-hole"
	"fmt"
	"swifty/apis"
)

func rabbitConn() (*rabbithole.Client, error) {
	addr := conf.Mware.Rabbit.c.AddrP(conf.Mware.Rabbit.AdminPort)
	return rabbithole.NewClient("http://" + addr, conf.Mware.Rabbit.c.User, gateSecrets[conf.Mware.Rabbit.c.Pass])
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

func InitRabbitMQ(ctx context.Context, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(ctx, mwd)
	if err != nil {
		return err
	}

	mwd.Namespace = mwd.Client

	rmqc, err := rabbitConn()
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
	err = rabbitErr(rmqc.UpdatePermissionsIn(mwd.Namespace, conf.Mware.Rabbit.c.User,
			rabbithole.Permissions{Configure: ".*", Write: ".*", Read: ".*"}))
	if err != nil {
		return fmt.Errorf("Can't set permissions %s: %s", mwd.Client, err.Error())
	}

	return nil
}

func FiniRabbitMQ(ctx context.Context, mwd *MwareDesc) error {
	rmqc, err := rabbitConn()
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

	var funcs []*FunctionDesc
	/* FIXME -- list FNs with events here, now they are in separate DB */

	for _, fn := range funcs {
		doRunBg(ctx, fn, "mq",
				&swyapi.FunctionRun{Body: data})
	}
}

func GetEnvRabbitMQ(ctx context.Context, mwd *MwareDesc) map[string][]byte {
	e := mwd.stdEnvs(conf.Mware.Rabbit.c.Addr())
	e[mwd.envName("VHOST")] = []byte(mwd.Namespace)
	return e
}

var MwareRabbitMQ = MwareOps {
	Init:	InitRabbitMQ,
	Fini:	FiniRabbitMQ,
	GetEnv:	GetEnvRabbitMQ,
	Devel:	true,
}

