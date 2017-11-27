package main

import (
	_ "gopkg.in/mgo.v2"
	_ "gopkg.in/mgo.v2/bson"

	"net/http"
	"fmt"
)

func verifyAclValue(acl string, acls []string) bool {
	for _, v := range acls {
		if acl == v {
			return true
		}
	}

	return false
}

const (
	S3StateNone			= 0
	S3StateActive			= 1
	S3StateInactive			= 2
)

const (
	S3StogateMaxObjects		= int64(10000)
	S3StogateMaxBytes		= int64(100 << 20)
	S3StorageSizePerObj		= int64(8 << 20)
)

var s3StateTransition = map[uint32][]uint32 {
	S3StateNone:		[]uint32{ S3StateNone, },
	S3StateActive:		[]uint32{ S3StateNone, },
	S3StateInactive:	[]uint32{ S3StateActive, },
}

func s3CheckAccess(akey *S3AccessKey, bucket_name, object_name string) error {
	// FIXME Implement lookup and ACL, for now just allow
	return nil
}

func s3VerifyAuthorization(r *http.Request) (*S3AccessKey, error) {
	var akey *S3AccessKey = nil
	var err error = nil

	accessKey := member(r.Header.Get("Authorization"),
				"Credential=", "/")
	if accessKey != "" {
		akey, _, _ = dbLookupAccessKey(accessKey)
		if akey == nil {
			err = fmt.Errorf("Authorization: No access key %v found", accessKey)
		}
	} else {
		err = fmt.Errorf("Authorization: No access key supplied")
	}

	return akey, err
}
