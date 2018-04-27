package swyapi

type GateErr struct {
	Code		uint			`json:"code"`
	Message		string			`json:"message"`
}

type ProjectList struct {
}

type ProjectDel struct {
	Project		string			`json:"project"`
}

type TenantStatsReq struct {
	Periods		int			`json:"periods"`
}

type FunctionStats struct {
	Called		uint64			`json:"called"`
	Timeouts	uint64			`json:"timeouts"`
	Errors		uint64			`json:"errors"`
	LastCall	string			`json:"lastcall,omitempty"`
	Time		uint64			`json:"time"`
	GBS		float64			`json:"gbs"`
	BytesOut	uint64			`json:"bytesout"`
	Till		string			`json:"till,omitempty"`
	From		string			`json:"from,omitempty"`
}

type FunctionStatsResp struct {
	Stats		[]FunctionStats		`json:"stats"`
}

type TenantStats struct {
	Called		uint64			`json:"called"`
	GBS		float64			`json:"gbs"`
	BytesOut	uint64			`json:"bytesout"`
	Till		string			`json:"till,omitempty"`
	From		string			`json:"from,omitempty"`
}

type TenantStatsResp struct {
	Stats		[]TenantStats		`json:"stats"`
}

type FunctionWait struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Timeout		uint32			`json:"timeout"` /* msec */
	Version		string			`json:"version,omitempty"`
}

type FunctionInfo struct {
	Name		string			`json:"name,omitempty"`
	Mware		[]string		`json:"mware,omitempty"`
	S3Buckets	[]string		`json:"s3buckets,omitempty"`
	State		string			`json:"state"`
	Version		string			`json:"version"`
	RdyVersions	[]string		`json:"rversions"`
	Code		FunctionCode		`json:"code"`
	URL		string			`json:"url,omitempty"`
	Stats		[]FunctionStats		`json:"stats"`
	Size		FunctionSize		`json:"size"`
	AuthCtx		string			`json:"authctx,omitempty"`
	UserData	string			`json:"userdata,omitempty"`
	Id		string			`json:"id"`
}

type RunCmd struct {
	Exe		string			`json:"exe"`
	Args		[]string		`json:"args,omitempty"`
}

type FunctionCode struct {
	Lang		string			`json:"lang"`
	Env		[]string		`json:"env,omitempty"`
}

type FunctionSources struct {
	Type		string			`json:"type"`
	Repo		string			`json:"repo,omitempty"`
	Code		string			`json:"code,omitempty"`
}

type FunctionSize struct {
	Memory		uint64			`json:"memory"`
	Timeout		uint64			`json:"timeout"` /* msec */
	Rate		uint			`json:"rate,omitempty"`
	Burst		uint			`json:"burst,omitempty"`
}

type FunctionEventCron struct {
	Tab		string			`json:"tab"`
	Args		map[string]string	`json:"args"`
}

type FunctionEventS3 struct {
	Bucket		string			`json:"bucket"`
	Ops		string			`json:"ops,omitempty"`
	Pattern		string			`json:"pattern,omitempty"`
}

type FunctionEvent struct {
	Id		string			`json:"id,omitempty"`
	Name		string			`json:"name"`
	Source		string			`json:"source"`
	Cron		*FunctionEventCron	`json:"cron,omitempty"`
	S3		*FunctionEventS3	`json:"s3,omitempty"`
}

type MwareAdd struct {
	Name		string			`json:"name"`
	Type		string			`json:"type"`
	UserData	string			`json:"userdata,omitempty"`
}

type MwareAddD struct {
	Project		string			`json:"project"`
	ID		string			`json:"id"`
	Type		string			`json:"type"`
	UserData	string			`json:"userdata,omitempty"`
}

type MwareInfo struct {
	ID		string			`json:"id,omitempty"`
	Name		string			`json:"name,omitempty"`
	Type		string			`json:"type"`
	UserData	string			`json:"userdata,omitempty"`
	DU		*uint64			`json:"disk_usage,omitempty"` /* in ... KB */
}

func (i *MwareInfo)SetDU(bytes uint64) {
	kb := bytes >> 10
	i.DU = &kb
}

type S3Access struct {
	Bucket		string			`json:"bucket"`
	Lifetime	uint32			`json:"lifetime"` /* seconds */
	Access		[]string		`json:"access"`
}

type S3Creds struct {
	Endpoint	string			`json:"endpoint"`
	Key		string			`json:"key"`
	Secret		string			`json:"secret"`
	Expires		uint32			`json:"expires"` /* in seconds */
}

type FunctionAddD struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Sources		FunctionSources		`json:"sources"`
	Code		FunctionCode		`json:"code"`
	Size		FunctionSize		`json:"size"`
	Mware		[]string		`json:"mware,omitempty"`
	S3Buckets	[]string		`json:"s3buckets,omitempty"`
	UserData	string			`json:"userdata,omitempty"`
	AuthCtx		string			`json:"authctx,omitempty"`
}

type FunctionAdd struct {
	Name		string			`json:"name"`
	Sources		FunctionSources		`json:"sources"`
	Code		FunctionCode		`json:"code"`
	Size		FunctionSize		`json:"size"`
	Mware		[]string		`json:"mware,omitempty"`
	S3Buckets	[]string		`json:"s3buckets,omitempty"`
	UserData	string			`json:"userdata,omitempty"`
	AuthCtx		string			`json:"authctx,omitempty"`
}

type FunctionUpdate struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Code		string			`json:"code"`
	Size		*FunctionSize		`json:"size,omitempty"`
	Mware		*[]string		`json:"mware,omitempty"`
	S3Buckets	*[]string		`json:"s3buckets,omitempty"`
}

type FunctionRun struct {
	Args		map[string]string	`json:"args,omitempty"`
}

type FunctionRunResult struct {
	Code		int			`json:"code"`
	Return		string			`json:"return"`
	Stdout		string			`json:"stdout"`
	Stderr		string			`json:"stderr"`
}

type FunctionXID struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Version		string			`json:"version"`
}

type ProjectItem struct {
	Project		string			`json:"project"`
}

type FunctionLogEntry struct {
	Event		string			`json:"event"`
	Ts		string			`json:"ts"`
	Text		string			`json:"text"`
}

type DeployId struct {
	Project		string			`json:"project"`
	Name		string			`json:"name"`
}

type DeployItem struct {
	Function	*FunctionAddD		`json:"function,omitempty"`
	Mware		*MwareAddD		`json:"mware,omitempty"`
}

type DeployStart struct {
	Project		string			`json:"project"`
	Name		string			`json:"name"`
	Items		[]DeployItem		`json:"items"`
}

type DeployItemInfo struct {
	Type		string			`json:"type"`
	Name		string			`json:"name"`
	State		string			`json:"state"`
}

type DeployInfo struct {
	State		string			`json:"state"`
	Items		[]*DeployItemInfo	`json:"items"`
}
