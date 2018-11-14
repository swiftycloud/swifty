/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"net/http"
	"fmt"
	"context"
	"errors"

	"swifty/s3/mgo"
	"swifty/apis/s3"
)


func verifyAclValue(acl string, acls []string) bool {
	for _, v := range acls {
		if acl == v {
			return true
		}
	}

	return false
}

var adminAccToken string

func s3VerifyAdmin(r *http.Request) error {
	access_token := r.Header.Get(swys3api.SwyS3_AdminToken)

	if access_token != adminAccToken {
		if access_token != "" && S3ModeDevel {
			log.Errorf("Access token mismatch (%s!=%s)",
				access_token, adminAccToken)
		}
		return fmt.Errorf("X-SwyS3-Token header mismatched or missed")
	}

	return nil
}

func s3AuthorizeAdmin(ctx context.Context, r *http.Request) (*s3mgo.AccessKey, error) {
	access_token := r.Header.Get(swys3api.SwyS3_AdminToken)
	if access_token == "" {
		return nil, nil
	}

	if access_token != adminAccToken {
		return nil, errors.New("Bad admin authorization creds")
	}

	access_key := r.Header.Get(swys3api.SwyS3_AccessKey)
	if access_key == "" {
		return nil, errors.New("Access key missing")
	}

	return LookupAccessKey(ctx, access_key)
}

func s3CheckAccess(ctx context.Context, bname, oname string) error {
	// FIXME Implement lookup and ACL, for now just allow
	return nil
}
