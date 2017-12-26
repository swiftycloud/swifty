package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
	"fmt"
)

type S3Iam struct {
	ObjID				bson.ObjectId	`json:"_id,omitempty" bson:"_id,omitempty"`
	IamID				string		`json:"iam-id,omitempty" bson:"iam-id,omitempty"`
	Namespace			string		`json:"namespace,omitempty" bson:"namespace,omitempty"`
	CreationTime			string		`json:"creation-time,omitempty" bson:"creation-time,omitempty"`
	State				uint32		`json:"state,omitempty" bson:"state,omitempty"`
	User				string		`json:"user,omitempty" bson:"user,omitempty"`
	Email				string		`json:"email,omitempty" bson:"email,omitempty"`
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
	return s3LookupIam(bson.M{"_id": akey.IamID,
				"state": S3StateActive})
}

// FIXME: There MUST not be plain namespace coming
// from notification, we always must obtain akey instead
// and figure out which namespace it belongs, but it
// require changes in gate code, so left as is for now.

func BIDFromNames(namespace, bucket string) string {
	/*
	 * BID stands for backend-id and is a unique identifier
	 * in the storage. For CEPH case this is pool ID and
	 * since all users live in a plain pool namespace, it
	 * should be unique across users and their buckets.
	 */
	return sha256sum([]byte(namespace + bucket))
}

func (iam *S3Iam)BucketBID(bname string) string {
	return BIDFromNames(iam.Namespace, bname)
}

func (iam *S3Iam)NamespaceID() string {
	return sha256sum([]byte(iam.Namespace))
}

func (iam *S3Iam)s3IamRemove() error {
	return dbS3Remove(iam, bson.M{"_id": iam.ObjID})
}

func (akey *S3AccessKey)s3IamRemove() error {
	return dbS3Remove(&S3Iam{}, bson.M{"_id": akey.IamID})
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

	// FIXME Add counter so namespace would be shareable
	iam = &S3Iam{
		ObjID:		bson.NewObjectId(),
		IamID:		sha256sum([]byte(rq.email)),
		Namespace:	rq.namespace,
		CreationTime:	time.Now().Format(time.RFC3339),
		State:		S3StateActive,
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
