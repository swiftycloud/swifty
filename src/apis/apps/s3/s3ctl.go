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
