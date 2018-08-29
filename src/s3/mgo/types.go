package s3mgo

import (
	"gopkg.in/mgo.v2/bson"
)

type S3Account struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	AwsID				string		`bson:"aws-id,omitempty"`
	Namespace			string		`bson:"namespace,omitempty"`

	CreationTime			string		`bson:"creation-time,omitempty"`
	User				string		`bson:"user,omitempty"`
	Email				string		`bson:"email,omitempty"`
}

type S3Iam struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	AwsID				string		`bson:"aws-id,omitempty"`
	AccountObjID			bson.ObjectId	`bson:"account-id,omitempty"`

	Policy				S3Policy	`bson:"policy,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`
	User				string		`bson:"user,omitempty"`
}

type ActionBits		[2]uint64
type ActionBitsMgo	[16]byte

// Most permissive mode
const (
	Resourse_Any				= "*"
)

type S3Policy struct {
	Effect		string		`bson:"effect,omitempty"`
	Action		ActionBitsMgo	`bson:"action,omitempty"`
	Resource	[]string	`bson:"resource,omitempty"`
}

const S3TimeStampMax = int64(0x7fffffffffffffff)

type S3AccessKey struct {
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

type S3BucketNotify struct {
	Queue				string		`bson:"queue"`
	Put				uint32		`bson:"put"`
	Delete				uint32		`bson:"delete"`
}

type S3Tag struct {
	Key				string		`bson:"key"`
	Value				string		`bson:"value,omitempty"`
}

type S3BucketEncrypt struct {
	Algo				string		`bson:"algo"`
	MasterKeyID			string		`bson:"algo,omitempty"`
}

type S3Bucket struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BCookie				string		`bson:"bcookie,omitempty"`

	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	NamespaceID			string		`bson:"nsid,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`

	// Todo
	Versioning			bool		`bson:"versioning,omitempty"`
	TagSet				[]S3Tag		`bson:"tags,omitempty"`
	Encrypt				S3BucketEncrypt	`bson:"encrypt,omitempty"`
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
	BasicNotify			*S3BucketNotify	`bson:"notify,omitempty"`

	MaxObjects			int64		`bson:"max-objects"`
	MaxBytes			int64		`bson:"max-bytes"`
}

type S3ObjectProps struct {
	CreationTime			string		`bson:"creation-time,omitempty"`
	Acl				string		`bson:"acl,omitempty"`
	Key				string		`bson:"key"`

	// Todo
	Meta				[]S3Tag		`bson:"meta,omitempty"`
	TagSet				[]S3Tag		`bson:"tags,omitempty"`
	Policy				string		`bson:"policy,omitempty"`

	// Not supported props
	// torrent
	// objects archiving
}

type S3Object struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	OCookie				string		`bson:"ocookie"`

	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	Version				int		`bson:"version"`
	Rover				int64		`bson:"rover"`
	Size				int64		`bson:"size"`
	ETag				string		`bson:"etag"`

	S3ObjectProps					`bson:",inline"`
}

type S3ObjectPart struct {
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
	Chunks				[]bson.ObjectId	`bson:"chunks"`
}

type S3DataChunk struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	Bytes		[]byte		`bson:"bytes"`
}
