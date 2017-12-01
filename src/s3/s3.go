package main

import (
	"encoding/hex"
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

func s3VerifyAdmin(r *http.Request) error {
	access_key := r.Header.Get("x-swy-key")
	access_sig := r.Header.Get("x-swy-sig")
	access_msg := r.Header.Get("x-swy-msg")

	if access_key == "" || access_sig == "" || access_msg == "" {
		return fmt.Errorf("No required headers found")
	}

	// FIXME Check for Kind
	akey, err := dbLookupAccessKey(access_key)
	if err != nil {
		return fmt.Errorf("Invalid key")
	}

	digest := makeHmac([]byte(akey.AccessKeySecret), []byte(access_msg))
	digesthex := hex.EncodeToString(digest)

	// -H "x-swy-key:6DLA43X797XL2I42IJ33"
	// -H "x-swy-msg:pleaseletmein"
	// -H "x-swy-sig:ac95ab4b16ebdb70fd96e49e44c97141dab43bfcc208f2145bb891bcdadedcb9"
	if digesthex != access_sig {
		return fmt.Errorf("Invalid signature")
	}

	return nil
}

func s3CheckAccess(akey *S3AccessKey, bucket_name, object_name string) error {
	// FIXME Implement lookup and ACL, for now just allow
	return nil
}
