package main

import (
	"gopkg.in/mgo.v2"
)

func quotaExceeded(ses *mgo.Session, db string, st *MgoStat) bool {
	return false
}
