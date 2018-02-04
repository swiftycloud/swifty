package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
	"fmt"
)

type S3Iam struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state,omitempty"`

	AccountObjID			bson.ObjectId	`bson:"account-id,omitempty"`
	Policy				S3Policy	`bson:"policy,omitempty"`
	IamID				string		`bson:"iam-id,omitempty"`
	Namespace			string		`bson:"namespace,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`
	User				string		`bson:"user,omitempty"`
	Email				string		`bson:"email,omitempty"`
}

func s3LookupIam(query bson.M) (*S3Iam, error) {
	var res S3Iam

	err := dbS3FindOne(query, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func s3IamFindByNamespace(namespace string) (*S3Iam, error) {
	return s3LookupIam(bson.M{"namespace": namespace,
				"state": S3StateActive})
}

func (akey *S3AccessKey)s3IamFind() (*S3Iam, error) {
	return s3LookupIam(bson.M{"_id": akey.IamObjID,
				"state": S3StateActive})
}

/* Key, that gives access to bucket */
func (iam *S3Iam)MakeBucketKey(bucket, act string) *S3AccessKey {
	return &S3AccessKey{
		IamObjID: iam.ObjID,
		Bucket: bucket,
	}
}


func (iam *S3Iam)NamespaceID() string {
	return sha256sum([]byte(iam.Namespace))
}

type iamResp struct {
	iam *S3Iam
	err error
}

type iamReq struct {
	namespace string
	user string
	email string
	resp chan *iamResp
}

var iamReqs chan *iamReq

func s3IamGet(namespace, user, email string) (*S3Iam, error) {
	rq := &iamReq {
		namespace: namespace,
		user: user, email: email,
		resp: make(chan *iamResp),
	}
	iamReqs <- rq
	rsp := <-rq.resp

	return rsp.iam, rsp.err
}

func iamGetter(rq *iamReq) *iamResp {
	var ObjectId bson.ObjectId
	var err error

	if rq.namespace == "" {
		return &iamResp{ nil, fmt.Errorf("s3,iam: Empty namespace passed") }
	}

	iam, err := s3IamFindByNamespace(rq.namespace)
	if err == nil {
		return &iamResp{ iam, nil }
	}

	if err != mgo.ErrNotFound {
		return &iamResp{ nil, err }
	}

	if rq.user == "" || rq.email == "" {
		rq.user = genKey(16, AccessKeyLetters)
		rq.email = genKey(8, AccessKeyLetters) + "@fake.mail"
		if rq.user == "" || rq.email == "" {
			return &iamResp{ nil, fmt.Errorf("s3,iam: Can't generate user/email") }
		}
	}

	ObjectId = bson.NewObjectId()
	// FIXME Add counter so namespace would be shareable
	iam = &S3Iam{
		ObjID:		ObjectId,
		AccountObjID:	ObjectId,
		State:		S3StateActive,

		IamID:		sha256sum([]byte(rq.email)),
		Namespace:	rq.namespace,
		CreationTime:	time.Now().Format(time.RFC3339),
		User:		rq.user,
		Email:		rq.email,
	}

	err = dbS3Insert(iam)
	if err != nil {
		return &iamResp{ nil, err }
	}

	log.Debugf("s3,iam: Created namespace %v", iam)
	return &iamResp{ iam, nil }
}

func init() {
	iamReqs = make(chan *iamReq)
	go func() {
		for {
			rq := <-iamReqs
			rq.resp <- iamGetter(rq)
		}
	}()
}
