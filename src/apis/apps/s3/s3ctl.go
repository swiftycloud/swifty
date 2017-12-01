package swys3ctl

type S3CtlKeyGen struct {
	Namespace		string		`json:"namespace,omitempty"`
}

type S3CtlKeyGenResult struct {
	AccessKeyID		string		`json:"access-key-id"`
	AccessKeySecret		string		`json:"access-key-secret"`
}

type S3CtlKeyDel struct {
	AccessKeyID		string		`json:"access-key-id"`
}
