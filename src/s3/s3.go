package main

import (
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
	S3StateAbort			= 3
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
	S3StateAbort:		[]uint32{ S3StateActive, S3StateInactive, },
}

func s3VerifyAdmin(r *http.Request) error {
	access_token := r.Header.Get("X-SwyS3-Token")

	if access_token != s3Secrets[conf.Daemon.Token] {
		if S3ModeDevel {
			log.Errorf("Access token mismatch (%s!=%s)",
				access_token, s3Secrets[conf.Daemon.Token])
		}
		return fmt.Errorf("X-SwyS3-Token header mismatched or missed")
	}

	return nil
}

func s3CheckAccess(akey *S3AccessKey, bucket_name, object_name string) error {
	// FIXME Implement lookup and ACL, for now just allow
	return nil
}
