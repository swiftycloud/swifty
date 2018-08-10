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

