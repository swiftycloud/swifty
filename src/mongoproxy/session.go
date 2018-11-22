package main

import (
	"errors"
	"gopkg.in/mgo.v2"
)

var pinfo *mgo.DialInfo

func configureSession(conf *Config) error {
	if conf.Target.Addr == "" {
		return errors.New("No target.address")
	}
	if conf.Target.DB == "" {
		return errors.New("No target.db")
	}
	if conf.Target.User == "" {
		return errors.New("No target.user")
	}
	if conf.Target.Pass == "" {
		return errors.New("No target.password")
	}

	pinfo = &mgo.DialInfo {
		Addrs:		[]string{conf.Target.Addr},
		Database:	conf.Target.DB,
		Username:	conf.Target.User,
		Password:	conf.Target.Pass,
	}

	return nil
}
