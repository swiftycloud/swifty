package main

import (
	"net/http"
	"fmt"
)

// Some of error codes we use, the
// complete list is at Amazon S3 manual
type s3RespErrorMap struct {
	HttpStatus			int
	ErrorCode			string
}

const (
	S3ErrAccessDenied		int =  1
	S3ErrAccountProblem		int =  2
	S3ErrBucketAlreadyExists	int =  3
	S3ErrBucketNotEmpty		int =  4
	S3ErrEntityTooSmall		int =  5
	S3ErrEntityTooLarge		int =  6
	S3ErrIncompleteBody		int =  7
	S3ErrInternalError		int =  8
	S3ErrInvalidAccessKeyId		int =  9
	S3ErrInvalidArgument		int = 10
	S3ErrInvalidBucketName		int = 11
	S3ErrInvalidBucketState		int = 12
	S3ErrInvalidObjectState		int = 13
	S3ErrInvalidRequest		int = 14
	S3ErrInvalidSecurity		int = 15
	S3ErrInvalidStorageClass	int = 16
	S3ErrInvalidToken		int = 17
	S3ErrInvalidURI			int = 18
	S3ErrKeyTooLong			int = 19
	S3ErrMissingSecurityHeader	int = 20
	S3ErrNoSuchBucket		int = 21
	S3ErrNoSuchKey			int = 22
	S3ErrNotImplemented		int = 23
	S3ErrOperationAborted		int = 24
	S3ErrSignatureDoesNotMatch	int = 25
	S3ErrServiceUnavailable		int = 26
	S3ErrTooManyBuckets		int = 27
)

var s3RespErrorMapData = map[int]s3RespErrorMap {
	// Access denied for various reasons
	S3ErrAccessDenied: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"AccessDenied",
	},
	// Internal problem with account
	S3ErrAccountProblem: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"AccountProblem",
	},
	// Creating existing bucket
	S3ErrBucketAlreadyExists: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"BucketAlreadyExists",
	},
	// Deleting not empy bucket
	S3ErrBucketNotEmpty: s3RespErrorMap {
		HttpStatus:	http.StatusConflict,
		ErrorCode:	"BucketNotEmpty",
	},
	// Trying to upload too small entity
	S3ErrEntityTooSmall: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"EntityTooSmall",
	},
	// Trying to upload too big entity
	S3ErrEntityTooLarge: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"EntityTooLarge",
	},
	// Content-Length not present but body
	S3ErrIncompleteBody: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"IncompleteBody",
	},
	// Internal error, retry request
	S3ErrInternalError: s3RespErrorMap {
		HttpStatus:	http.StatusInternalServerError,
		ErrorCode:	"InternalError",
	},
	// Access key doesn't exist
	S3ErrInvalidAccessKeyId: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"InvalidAccessKeyId",
	},
	// Invalid Argument
	S3ErrInvalidArgument: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidArgument",
	},
	// The specified bucket is not valid
	S3ErrInvalidBucketName: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidBucketName",
	},
	// The request is not valid with the current state of the bucket
	S3ErrInvalidBucketState: s3RespErrorMap {
		HttpStatus:	http.StatusConflict,
		ErrorCode:	"InvalidBucketState",
	},
	// The operation is not valid for the current state of the object
	S3ErrInvalidObjectState: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"InvalidObjectState",
	},
	// Various invalid requests
	S3ErrInvalidRequest: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidRequest",
	},
	// The provided security credentials are not valid
	S3ErrInvalidSecurity: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"InvalidSecurity",
	},
	// The storage class you specified is not valid
	S3ErrInvalidStorageClass: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidStorageClass",
	},
	// The provided token is malformed or otherwise invalid
	S3ErrInvalidToken: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidToken",
	},
	// Couldn't parse the specified URI
	S3ErrInvalidURI: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidURI",
	},
	// Your key is too long
	S3ErrKeyTooLong: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"KeyTooLong",
	},
	// Your request is missing a required header
	S3ErrMissingSecurityHeader: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MissingSecurityHeader",
	},
	// The specified bucket does not exist
	S3ErrNoSuchBucket: s3RespErrorMap {
		HttpStatus:	http.StatusNotFound,
		ErrorCode:	"NoSuchBucket",
	},
	// The specified key does not exist
	S3ErrNoSuchKey: s3RespErrorMap {
		HttpStatus:	http.StatusNotFound,
		ErrorCode:	"NoSuchKey",
	},
	// A header you provided implies functionality that is not implemented
	S3ErrNotImplemented: s3RespErrorMap {
		HttpStatus:	http.StatusNotImplemented,
		ErrorCode:	"NotImplemented",
	},
	// A conflicting conditional operation is currently in
	// progress against this resource. Try again.
	S3ErrOperationAborted: s3RespErrorMap {
		HttpStatus:	http.StatusConflict,
		ErrorCode:	"OperationAborted",
	},
	// The request signature we calculated does not match the signature you provided
	S3ErrSignatureDoesNotMatch: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"SignatureDoesNotMatch",
	},
	// Reduce your request rate
	S3ErrServiceUnavailable: s3RespErrorMap {
		HttpStatus:	http.StatusServiceUnavailable,
		ErrorCode:	"ServiceUnavailable",
	},
	// You have attempted to create more buckets than allowed
	S3ErrTooManyBuckets: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"TooManyBuckets",
	},
}

func HTTPRespError(w http.ResponseWriter, errcode int, msg string) {
	if m, ok := s3RespErrorMapData[errcode]; ok {
		e := S3RespError {
			Code:		m.ErrorCode,
			Message:	msg,
		}
		err := HTTPMarshalXMLAndWrite(w, m.HttpStatus, &e)
		if err != nil {
			goto out
		}
	}
out:
	// Either error is unmapped, or some other internal
	// problem: just setup header and that's all
	http.Error(w,
		fmt.Sprintf("Internal error %d", errcode),
		http.StatusInternalServerError)
}
