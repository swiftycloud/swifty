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
