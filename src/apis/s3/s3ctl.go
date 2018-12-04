/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swys3api

type KeyGen struct {
	Namespace		string		`json:"namespace,omitempty"`
	Bucket			string		`json:"bucket,omitempty"`
	Lifetime		uint32		`json:"lifetime,omitempty"`
}

type KeyGenResult struct {
	AccessKeyID		string		`json:"access-key-id"`
	AccessKeySecret		string		`json:"access-key-secret"`
	AccID			string		`json:"accid"`
}

type KeyDel struct {
	AccessKeyID		string		`json:"access-key-id"`
}

type Subscribe struct {
	Namespace		string		`json:"namespace"`
	Bucket			string		`json:"bucket"`
	Ops			string		`json:"ops"`
	Queue			string		`json:"queue"`
}

type Event struct {
	Namespace		string		`json:"namespace"`
	Bucket			string		`json:"bucket"`
	Object			string		`json:"object,omitempty"`
	Op			string		`json:"op"`
}

type AcctStats struct {
	CntObjects		int64		`json:"cnt-objects"`
	CntBytes		int64		`json:"cnt-bytes"`
	OutBytes		int64		`json:"out-bytes"`
	OutBytesWeb		int64		`json:"out-bytes-web"`

	Lim			*AcctLimits	`json:"limits,omitempty"`
}

type AcctLimits struct {
}
