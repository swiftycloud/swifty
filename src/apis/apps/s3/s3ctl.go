package swys3api

type S3CtlKeyGen struct {
	Namespace		string		`json:"namespace,omitempty"`
	Bucket			string		`json:"bucket,omitempty"`
}

type S3CtlKeyGenResult struct {
	AccessKeyID		string		`json:"access-key-id"`
	AccessKeySecret		string		`json:"access-key-secret"`
}

type S3CtlKeyDel struct {
	AccessKeyID		string		`json:"access-key-id"`
}

type S3CtlKeyGetRoot struct {
	AccessKeyID		string		`json:"access-key-id"`
}

type S3CtlBucketReq struct {
	Namespace		string		`json:"namespace,omitempty"`
	Bucket			string		`json:"bucket,omitempty"`
	Acl			string		`json:"acl,omitempty"`
}

type S3Subscribe struct {
	Namespace		string		`json:"namespace"`
	Bucket			string		`json:"bucket"`
	Ops			string		`json:"ops"`
	Queue			string		`json:"queue"`
}

type S3Event struct {
	Namespace		string		`json:"namespace"`
	Bucket			string		`json:"bucket"`
	Object			string		`json:"object,omitempty"`
	Op			string		`json:"op"`
}
