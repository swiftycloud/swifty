/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"errors"
	"log"
)

var connstr string

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

	connstr = conf.Target.User + ":" + conf.Target.Pass + "@tcp(" + conf.Target.Addr + ")/" + conf.Target.DB
	log.Printf("TGT: %s\n", connstr)
	return nil
}
