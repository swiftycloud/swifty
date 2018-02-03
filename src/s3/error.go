package main

import (
	"net/http"
	"fmt"

	"../apis/apps/s3"
)

// Some of error codes we use, the
// complete list is at Amazon S3 manual
type s3RespErrorMap struct {
	HttpStatus			int
	ErrorCode			string
}

const (
	// Common error codes
	S3ErrAccessDenied				int =  1
	S3ErrAccountProblem				int =  2
	S3ErrAmbiguousGrantByEmailAddress		int =  3
	S3ErrBadDigest					int =  4
	S3ErrBucketAlreadyExists			int =  5
	S3ErrBucketAlreadyOwnedByYou			int =  6
	S3ErrBucketNotEmpty				int =  7
	S3ErrCredentialsNotSupported			int =  8
	S3ErrCrossLocationLoggingProhibited		int =  9
	S3ErrEntityTooSmall				int = 10
	S3ErrEntityTooLarge				int = 11
	S3ErrExpiredToken				int = 12
	S3ErrIllegalVersioningConfigurationException	int = 13
	S3ErrIncompleteBody				int = 14
	S3ErrIncorrectNumberOfFilesInPostRequest	int = 15
	S3ErrInlineDataTooLarge				int = 16
	S3ErrInternalError				int = 17
	S3ErrInvalidAccessKeyId				int = 18
	S3ErrInvalidAddressingHeader			int = 19
	S3ErrInvalidArgument				int = 20
	S3ErrInvalidBucketName				int = 21
	S3ErrInvalidBucketState				int = 22
	S3ErrInvalidDigest				int = 23
	S3ErrInvalidEncryptionAlgorithmError		int = 24
	S3ErrInvalidLocationConstraint			int = 25
	S3ErrInvalidObjectState				int = 26
	S3ErrInvalidPart				int = 27
	S3ErrInvalidPartOrder				int = 28
	S3ErrInvalidPayer				int = 29
	S3ErrInvalidPolicyDocument			int = 30
	S3ErrInvalidRange				int = 31
	S3ErrInvalidRequest				int = 32
	S3ErrInvalidSecurity				int = 33
	S3ErrInvalidSOAPRequest				int = 34
	S3ErrInvalidStorageClass			int = 35
	S3ErrInvalidTargetBucketForLogging		int = 36
	S3ErrInvalidToken				int = 37
	S3ErrInvalidURI					int = 38
	S3ErrKeyTooLong					int = 39
	S3ErrMalformedACLError				int = 40
	S3ErrMalformedPOSTRequest			int = 41
	S3ErrMalformedXML				int = 42
	S3ErrMaxMessageLengthExceeded			int = 43
	S3ErrMaxPostPreDataLengthExceededError		int = 44
	S3ErrMetadataTooLarge				int = 45
	S3ErrMethodNotAllowed				int = 46
	S3ErrMissingAttachment				int = 47
	S3ErrMissingContentLength			int = 48
	S3ErrMissingRequestBodyError			int = 49
	S3ErrMissingSecurityElement			int = 50
	S3ErrMissingSecurityHeader			int = 51
	S3ErrNoLoggingStatusForKey			int = 52
	S3ErrNoSuchBucket				int = 53
	S3ErrNoSuchKey					int = 54
	S3ErrNoSuchLifecycleConfiguration		int = 55
	S3ErrNoSuchUpload				int = 56
	S3ErrNoSuchVersion				int = 57
	S3ErrNotImplemented				int = 58
	S3ErrNotSignedUp				int = 59
	S3ErrNoSuchBucketPolicy				int = 60
	S3ErrOperationAborted				int = 61
	S3ErrPermanentRedirect				int = 62
	S3ErrPreconditionFailed				int = 63
	S3ErrRedirect					int = 64
	S3ErrRestoreAlreadyInProgress			int = 65
	S3ErrRequestIsNotMultiPartContent		int = 66
	S3ErrRequestTimeout				int = 67
	S3ErrRequestTimeTooSkewed			int = 68
	S3ErrRequestTorrentOfBucketError		int = 69
	S3ErrSignatureDoesNotMatch			int = 70
	S3ErrServiceUnavailable				int = 71
	S3ErrSlowDown					int = 72
	S3ErrTemporaryRedirect				int = 73
	S3ErrTokenRefreshRequired			int = 74
	S3ErrTooManyBuckets				int = 75
	S3ErrUnexpectedContent				int = 76
	S3ErrUnresolvableGrantByEmailAddress		int = 77
	S3ErrUserKeyMustBeSpecified			int = 78

	// IAM error codes
	S3ErrAccessDeniedException			int = 79
	S3ErrIncompleteSignature			int = 80
	S3ErrInternalFailure				int = 81
	S3ErrInvalidAction				int = 82
	S3ErrInvalidClientTokenId			int = 83
	S3ErrInvalidParameterCombination		int = 84
	S3ErrInvalidParameterValue			int = 85
	S3ErrInvalidQueryParameter			int = 86
	S3ErrMalformedQueryString			int = 87
	S3ErrMissingAction				int = 88
	S3ErrMissingAuthenticationToken			int = 89
	S3ErrMissingParameter				int = 90
	S3ErrOptInRequired				int = 91
	S3ErrRequestExpired				int = 92
	S3ErrThrottlingException			int = 93
	S3ErrValidationError				int = 94

	// Own error codes
	S3ErrInvalidObjectName				int = 1024
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
	// Email is associated more than one account
	S3ErrAmbiguousGrantByEmailAddress: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"AmbiguousGrantByEmailAddress",
	},
	// Content-MD5 header mismatch
	S3ErrBadDigest: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"BadDigest",
	},
	// Creating existing bucket
	S3ErrBucketAlreadyExists: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"BucketAlreadyExists",
	},
	// Previous attempt to create bucket successed
	S3ErrBucketAlreadyOwnedByYou: s3RespErrorMap {
		HttpStatus:	409,
		ErrorCode:	"BucketAlreadyOwnedByYou",
	},
	// Deleting not empy bucket
	S3ErrBucketNotEmpty: s3RespErrorMap {
		HttpStatus:	http.StatusConflict,
		ErrorCode:	"BucketNotEmpty",
	},
	// This request dosnt support credentials
	S3ErrCredentialsNotSupported: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"CredentialsNotSupported",
	},
	// Cross-location logging not allowed
	S3ErrCrossLocationLoggingProhibited: s3RespErrorMap {
		HttpStatus:	403,
		ErrorCode:	"CrossLocationLoggingProhibited",
	},
	// Trying to upload too small entity
	S3ErrEntityTooSmall: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"EntityTooSmall",
	},
	// Provided token is expired
	S3ErrExpiredToken: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"ExpiredToken",
	},
	// Versioning in request is invalid
	S3ErrIllegalVersioningConfigurationException: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"IllegalVersioningConfigurationException",
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
	// Only one file per request is allowed
	S3ErrIncorrectNumberOfFilesInPostRequest: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"IncorrectNumberOfFilesInPostRequest",
	},
	// Inline data is too large
	S3ErrInlineDataTooLarge: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InlineDataTooLarge",
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
	// You must specify the Anonymous role
	S3ErrInvalidAddressingHeader: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidAddressingHeader",
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
	// Content-MD5 is invalid
	S3ErrInvalidDigest: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidDigest",
	},
	// Only AES256 is valid
	S3ErrInvalidEncryptionAlgorithmError: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidEncryptionAlgorithmError",
	},
	// Location constraint is not valid
	S3ErrInvalidLocationConstraint: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidLocationConstraint",
	},
	// The operation is not valid for the current state of the object
	S3ErrInvalidObjectState: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"InvalidObjectState",
	},
	// When finishing multipart upload some parts are missing or etag mismatched
	S3ErrInvalidPart: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidPart",
	},
	// Parts are not in ascending order
	S3ErrInvalidPartOrder: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidPartOrder",
	},
	// All access to this object has been disabled
	S3ErrInvalidPayer: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"InvalidPayer",
	},
	// Content of the form doesnt meet policy
	S3ErrInvalidPolicyDocument: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidPolicyDocument",
	},
	// Requested range can't be satisfied
	S3ErrInvalidRange: s3RespErrorMap {
		HttpStatus:	416,
		ErrorCode:	"InvalidRange",
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
	// The SOAP request body is invalid
	S3ErrInvalidSOAPRequest: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidSOAPRequest",
	},
	// The storage class you specified is not valid
	S3ErrInvalidStorageClass: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidStorageClass",
	},
	// Can't setup loggin for bucket (doesn't exist or anything else)
	S3ErrInvalidTargetBucketForLogging: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidTargetBucketForLogging",
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
	// The XML you provided was not well formed
	S3ErrMalformedACLError: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MalformedACLError",
	},
	// Body for multipart/form-data corrupted
	S3ErrMalformedPOSTRequest: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MalformedPOSTRequest",
	},
	// Unparsable XML data obtained
	S3ErrMalformedXML: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MalformedXML",
	},
	// The request is too big
	S3ErrMaxMessageLengthExceeded: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MaxMessageLengthExceeded",
	},
	// POST request fields preceding the upload file were too large
	S3ErrMaxPostPreDataLengthExceededError: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MaxPostPreDataLengthExceededError",
	},
	// Too many metadata keys/vals
	S3ErrMetadataTooLarge: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MetadataTooLarge",
	},
	// The specified method is not allowed
	S3ErrMethodNotAllowed: s3RespErrorMap {
		HttpStatus:	http.StatusMethodNotAllowed,
		ErrorCode:	"MethodNotAllowed",
	},
	// A SOAP attachment was expected
	S3ErrMissingAttachment: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MissingAttachment",
	},
	// A SOAP attachment was expected
	S3ErrMissingContentLength: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MissingContentLength",
	},
	// Empty XML document in request
	S3ErrMissingRequestBodyError: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MissingRequestBodyError",
	},
	// The SOAP 1.1 request is missing a security element
	S3ErrMissingSecurityElement: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MissingSecurityElement",
	},
	// Your request is missing a required header
	S3ErrMissingSecurityHeader: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MissingSecurityHeader",
	},
	// There is no such thing as a logging for object
	S3ErrNoLoggingStatusForKey: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"NoLoggingStatusForKey",
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
	// The lifecycle configuration does not exist
	S3ErrNoSuchLifecycleConfiguration: s3RespErrorMap {
		HttpStatus:	http.StatusNotFound,
		ErrorCode:	"NoSuchLifecycleConfiguration",
	},
	// The specified multipart upload does not exist.
	// The upload ID might be invalid, or the multipart
	// upload might have been aborted or completed
	S3ErrNoSuchUpload: s3RespErrorMap {
		HttpStatus:	http.StatusNotFound,
		ErrorCode:	"NoSuchUpload",
	},
	// Version ID in request mismatch existing version
	S3ErrNoSuchVersion: s3RespErrorMap {
		HttpStatus:	http.StatusNotFound,
		ErrorCode:	"NoSuchVersion",
	},
	// A header you provided implies functionality that is not implemented
	S3ErrNotImplemented: s3RespErrorMap {
		HttpStatus:	http.StatusNotImplemented,
		ErrorCode:	"NotImplemented",
	},
	// Account is not signed up for the S3 service
	S3ErrNotSignedUp: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"NotSignedUp",
	},
	// The specified bucket does not have a bucket policy
	S3ErrNoSuchBucketPolicy: s3RespErrorMap {
		HttpStatus:	http.StatusNotFound,
		ErrorCode:	"NoSuchBucketPolicy",
	},
	// A conflicting conditional operation is currently in
	// progress against this resource. Try again.
	S3ErrOperationAborted: s3RespErrorMap {
		HttpStatus:	http.StatusConflict,
		ErrorCode:	"OperationAborted",
	},
	// The bucket you are attempting to access must be
	// addressed using the specified endpoint
	S3ErrPermanentRedirect: s3RespErrorMap {
		HttpStatus:	http.StatusMovedPermanently,
		ErrorCode:	"PermanentRedirect",
	},
	// At least one of the preconditions you specified did not hold
	S3ErrPreconditionFailed: s3RespErrorMap {
		HttpStatus:	http.StatusPreconditionFailed,
		ErrorCode:	"PreconditionFailed",
	},
	// Temporary redirect
	S3ErrRedirect: s3RespErrorMap {
		HttpStatus:	http.StatusTemporaryRedirect,
		ErrorCode:	"Redirect",
	},
	// Object restore is already in progress
	S3ErrRestoreAlreadyInProgress: s3RespErrorMap {
		HttpStatus:	http.StatusConflict,
		ErrorCode:	"RestoreAlreadyInProgress",
	},
	// Bucket POST must be of the enclosure-type multipart/form-data
	S3ErrRequestIsNotMultiPartContent: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"RequestIsNotMultiPartContent",
	},
	// Socket connection to the server was not read from
	// or written to within the timeout period
	S3ErrRequestTimeout: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"RequestTimeout",
	},
	// The difference between the request time
	// and the server's time is too large
	S3ErrRequestTimeTooSkewed: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"RequestTimeTooSkewed",
	},
	// Requesting the torrent file of a bucket is not permitted
	S3ErrRequestTorrentOfBucketError: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"RequestTorrentOfBucketError",
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
	// Reduce your request rate
	S3ErrSlowDown: s3RespErrorMap {
		HttpStatus:	http.StatusServiceUnavailable,
		ErrorCode:	"SlowDown",
	},
	// You are being redirected to the bucket
	// while DNS updates
	S3ErrTemporaryRedirect: s3RespErrorMap {
		HttpStatus:	http.StatusTemporaryRedirect,
		ErrorCode:	"TemporaryRedirect",
	},
	// The provided token must be refreshed
	S3ErrTokenRefreshRequired: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"TokenRefreshRequired",
	},
	// You have attempted to create more buckets than allowed
	S3ErrTooManyBuckets: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"TooManyBuckets",
	},
	// This request does not support content
	S3ErrUnexpectedContent: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"UnexpectedContent",
	},
	// The email address you provided does not
	// match any account on record
	S3ErrUnresolvableGrantByEmailAddress: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"UnresolvableGrantByEmailAddress",
	},
	// The bucket POST must contain the specified field name.
	// If it is specified check the order of the fields
	S3ErrUserKeyMustBeSpecified: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"UserKeyMustBeSpecified",
	},

	// You do not have sufficient access to perform this action
	S3ErrAccessDeniedException: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"AccessDeniedException",
	},
	// The request signature does not conform to standards
	S3ErrIncompleteSignature: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"IncompleteSignature",
	},
	// The request processing has failed because of an unknown error, exception or failure
	S3ErrInternalFailure: s3RespErrorMap {
		HttpStatus:	http.StatusInternalServerError,
		ErrorCode:	"InternalFailure",
	},
	// The action or operation requested is invalid. Verify that the action is typed correctly
	S3ErrInvalidAction: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidAction",
	},
	// The X.509 certificate or access key ID provided does not exist in our records
	S3ErrInvalidClientTokenId: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"InvalidClientTokenId",
	},
	// Parameters that must not be used together were used together
	S3ErrInvalidParameterCombination: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidParameterCombination",
	},
	// An invalid or out-of-range value was supplied for the input parameter
	S3ErrInvalidParameterValue: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidParameterValue",
	},
	// The query string is malformed or does not adhere to standards
	S3ErrInvalidQueryParameter: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidQueryParameter",
	},
	// The query string contains a syntax error
	S3ErrMalformedQueryString: s3RespErrorMap {
		HttpStatus:	http.StatusNotFound,
		ErrorCode:	"MalformedQueryString",
	},
	// The request is missing an action or a required parameter
	S3ErrMissingAction: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MissingAction",
	},
	// The request must contain either a valid (registered) access key ID or X.509 certificate
	S3ErrMissingAuthenticationToken: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"MissingAuthenticationToken",
	},
	// A required parameter for the specified action is not supplied
	S3ErrMissingParameter: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"MissingParameter",
	},
	// The access key ID needs a subscription for the service
	S3ErrOptInRequired: s3RespErrorMap {
		HttpStatus:	http.StatusForbidden,
		ErrorCode:	"OptInRequired",
	},
	// The request reached the service more than 15 minutes after the date stamp
	S3ErrRequestExpired: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"RequestExpired",
	},
	// The request was denied due to request throttling
	S3ErrThrottlingException: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"ThrottlingException",
	},
	// The input fails to satisfy the constraints specified by a service
	S3ErrValidationError: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"ValidationError",
	},

	// The specified object is not valid
	S3ErrInvalidObjectName: s3RespErrorMap {
		HttpStatus:	http.StatusBadRequest,
		ErrorCode:	"InvalidObjectName",
	},
}

func HTTPRespError(w http.ResponseWriter, errcode int, params ...string) {
	if m, ok := s3RespErrorMapData[errcode]; ok {
		e := swys3api.S3Error { Code: m.ErrorCode, }

		switch len(params) {
		case 3:
			e.RequestID = params[2]
			fallthrough
		case 2:
			e.Resource = params[1]
			fallthrough
		case 1:
			e.Message = params[0]
		}

		err := HTTPMarshalXMLAndWrite(w, m.HttpStatus, &e)
		if err != nil {
			goto out
		}
		return
	}
out:
	// Either error is unmapped, or some other internal
	// problem: just setup header and that's all
	http.Error(w,
		fmt.Sprintf("Internal error %d", errcode),
		http.StatusInternalServerError)
}
