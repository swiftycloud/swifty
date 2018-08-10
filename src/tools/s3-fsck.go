package main

import (
	"fmt"
	"flag"
	"time"
	"gopkg.in/mgo.v2"
)

const (
	DBName					= "swifty-s3"
	DBColS3Iams				= "S3Iams"
	DBColS3Buckets				= "S3Buckets"
	DBColS3Objects				= "S3Objects"
	DBColS3Uploads				= "S3Uploads"
	DBColS3ObjectData			= "S3ObjectData"
	DBColS3DataChunks			= "S3DataChunks"
	DBColS3AccessKeys			= "S3AccessKeys"
	DBColS3Websites				= "S3Websites"
)

var session *mgo.Session

func dbConnect(user, pass, host string) error {
	info := mgo.DialInfo{
		Addrs:		[]string{host},
		Database:	DBName,
		Timeout:	60 * time.Second,
		Username:	user,
		Password:	pass}

	var err error
	session, err = mgo.DialWithInfo(&info);
	if err != nil {
		fmt.Printf("Can't dial %s@%s: %s\n", user, host, err.Error())
		return err
	}

	session.SetMode(mgo.Monotonic, true)
	return nil
}

func main() {
	var user string
	var pass string
	var host string

	flag.StringVar(&user, "user", "swifty-s3", "user name")
	flag.StringVar(&pass, "pass", "", "password")
	flag.StringVar(&host, "host", "127.0.0.1", "db host")
	flag.Parse()

	err := dbConnect(user, pass, host)
	if err != nil {
		return
	}
}
