/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package s3mgo

import (
	"gopkg.in/mgo.v2/bson"
	"time"
)

type Account struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	AwsID				string		`bson:"aws-id,omitempty"`
	Namespace			string		`bson:"namespace,omitempty"`

	CreationTime			string		`bson:"creation-time,omitempty"`
	User				string		`bson:"user,omitempty"`
	Email				string		`bson:"email,omitempty"`
}

type AcctStats struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	NamespaceID			string		`bson:"nsid,omitempty"`

	CntObjects			int64		`bson:"cnt-objects"`
	CntBytes			int64		`bson:"cnt-bytes"`
	OutBytes			int64		`bson:"out-bytes"`
	OutBytesWeb			int64		`bson:"out-bytes-web"`

	OutBytesTotOff			int64		`bson:"out-bytes-tot-off"`

	/* Ach stuff */
	/*
	 * Dirty -- it's the ID of the original stats object on which we
	 * need to update offsets for increasing counters. Set on archive
	 * at creation, and is cleaned once the offsets are update in
	 * the original stats. See scraper code for details.
	 */
	Dirty				*bson.ObjectId	`bson:"dirty,omitempty"`
	Till				*time.Time	`bson:"till,omitempty"`
	Lim				*AcctLimits	`bson:"limits,omitempty"`
}

type AcctLimits struct {
	CntBytes			int64		`bson:"cnt-bytes"`
	OutBytesTot			int64		`bson:"out-bytes-tot"`
}

type Iam struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	AwsID				string		`bson:"aws-id,omitempty"`
	AccountObjID			bson.ObjectId	`bson:"account-id,omitempty"`

	Policy				Policy		`bson:"policy,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`
	User				string		`bson:"user,omitempty"`
}

type ActionBits		[2]uint64
type ActionBitsMgo	[16]byte

// Most permissive mode
const (
	Resourse_Any				= "*"
)

type Policy struct {
	Effect		string		`bson:"effect,omitempty"`
	Action		ActionBitsMgo	`bson:"action,omitempty"`
	Resource	[]string	`bson:"resource,omitempty"`
}

const TimeStampMax = int64(0x7fffffffffffffff)

type AccessKey struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	AccountObjID			bson.ObjectId	`bson:"account-id,omitempty"`
	IamObjID			bson.ObjectId	`bson:"iam-id,omitempty"`

	CreationTimestamp		int64		`bson:"creation-timestamp,omitempty"`
	ExpirationTimestamp		int64		`bson:"expiration-timestamp,omitempty"`

	AccessKeyID			string		`bson:"access-key-id"`
	AccessKeySecret			string		`bson:"access-key-secret"`
}

type BucketNotify struct {
	Queue				string		`bson:"queue"`
	Put				uint32		`bson:"put"`
	Delete				uint32		`bson:"delete"`
}

type Tag struct {
	Key				string		`bson:"key"`
	Value				string		`bson:"value,omitempty"`
}

type BucketEncrypt struct {
	Algo				string		`bson:"algo"`
	MasterKeyID			string		`bson:"algo,omitempty"`
}

type Bucket struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BCookie				string		`bson:"bcookie,omitempty"`

	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	NamespaceID			string		`bson:"nsid,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`

	// Todo
	Versioning			bool		`bson:"versioning,omitempty"`
	TagSet				[]Tag		`bson:"tags,omitempty"`
	Encrypt				BucketEncrypt	`bson:"encrypt,omitempty"`
	Location			string		`bson:"location,omitempty"`
	Policy				string		`bson:"policy,omitempty"`
	Logging				bool		`bson:"logging,omitempty"`
	Lifecycle			string		`bson:"lifecycle,omitempty"`
	RequestPayment			string		`bson:"request-payment,omitempty"`

	// Not supported props
	// analytics
	// cors
	// metrics
	// replication
	// website
	// accelerate
	// inventory
	// notification

	Ref				int64		`bson:"ref"`
	CntObjects			int64		`bson:"cnt-objects"`
	CntBytes			int64		`bson:"cnt-bytes"`
	Rover				int64		`bson:"rover"`
	Name				string		`bson:"name"`
	CannedAcl			string		`bson:"canned-acl"`
	BasicNotify			*BucketNotify	`bson:"notify,omitempty"`

	MaxObjects			int64		`bson:"max-objects"`
	MaxBytes			int64		`bson:"max-bytes"`
}

type ObjectProps struct {
	CreationTime			string		`bson:"creation-time,omitempty"`
	Acl				string		`bson:"acl,omitempty"`
	Key				string		`bson:"key"`

	// Todo
	Meta				[]Tag		`bson:"meta,omitempty"`
	TagSet				[]Tag		`bson:"tags,omitempty"`
	Policy				string		`bson:"policy,omitempty"`

	// Not supported props
	// torrent
	// objects archiving
}

type Object struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	OCookie				string		`bson:"ocookie"`

	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	Version				int		`bson:"version"`
	Rover				int64		`bson:"rover"`
	Size				int64		`bson:"size"`
	ETag				string		`bson:"etag"`

	ObjectProps					`bson:",inline"`
}

type ObjectPart struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`

	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	RefID				bson.ObjectId	`bson:"ref-id,omitempty"`
	BCookie				string		`bson:"bcookie,omitempty"`
	OCookie				string		`bson:"ocookie,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`
	Size				int64		`bson:"size"`
	Part				uint		`bson:"part"`
	ETag				string		`bson:"etag"`
	Data				[]byte		`bson:"data,omitempty"`
	Chunks				[]bson.ObjectId	`bson:"chunks"`
}

type DataChunk struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	Bytes		[]byte		`bson:"bytes"`
}
