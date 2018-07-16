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

type FunctionInfo struct {
	Name		string			`json:"name,omitempty"`
	Project		string			`json:"project,omitempty"`
	Labels		[]string		`json:"labels,omitempty"`
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

type FunctionMdat struct {
	RL		[]uint			`json:"rl"`
	BR		[]uint			`json:"br"`
}

//type RunCmd struct {
//	Exe		string			`json:"exe"`
//	Args		[]interface{}		`json:"args,omitempty"`
//}

type FunctionCode struct {
	Lang		string			`json:"lang"`
	Env		[]string		`json:"env,omitempty"`
}

type FunctionSwage struct {
	Template	string			`json:"template"`
	Params		map[string]string	`json:"params"`
}

type FunctionSources struct {
	Type		string			`json:"type"`
	Repo		string			`json:"repo,omitempty"`
	Code		string			`json:"code,omitempty"`
	Swage		*FunctionSwage		`json:"swage,omitempty"`
}

type FunctionSize struct {
	Memory		uint64			`json:"memory"`
	Timeout		uint64			`json:"timeout"` /* msec */
	Rate		uint			`json:"rate,omitempty"`
	Burst		uint			`json:"burst,omitempty"`
}

type FunctionWait struct {
	Timeout		uint			`json:"timeout"`
	Version		string			`json:"version,omitempty"`
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
	URL		string			`json:"url,omitempty"`
}

type MwareAdd struct {
	Name		string			`json:"name"`
	Project		string			`json:"project,omitempty"`
	Type		string			`json:"type"`
	UserData	string			`json:"userdata,omitempty"`
}

type MwareInfo struct {
	ID		string			`json:"id"`
	Labels		[]string		`json:"labels,omitempty"`
	Name		string			`json:"name"`
	Project		string			`json:"project,omitempty"`
	Type		string			`json:"type"`
	UserData	string			`json:"userdata,omitempty"`
	DU		*uint64			`json:"disk_usage,omitempty"` /* in ... KB */
}

func (i *MwareInfo)SetDU(bytes uint64) {
	kb := bytes >> 10
	i.DU = &kb
}

type S3Access struct {
	Project		string			`json:"project"`
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

type FunctionAdd struct {
	Name		string			`json:"name"`
	Project		string			`json:"project,omitempty"`
	Sources		FunctionSources		`json:"sources"`
	Code		FunctionCode		`json:"code"`
	Size		FunctionSize		`json:"size"`
	Mware		[]string		`json:"mware,omitempty"`
	S3Buckets	[]string		`json:"s3buckets,omitempty"`
	UserData	string			`json:"userdata,omitempty"`
	AuthCtx		string			`json:"authctx,omitempty"`

	Url		bool			`json:"-"`
}

type FunctionUpdate struct {
	UserData	*string			`json:"userdata,omitempty"`
	State		string			`json:"state,omitempty"`
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

type ProjectItem struct {
	Project		string			`json:"project"`
}

type FunctionLogEntry struct {
	Event		string			`json:"event"`
	Ts		string			`json:"ts"`
	Text		string			`json:"text"`
}

type DeployItem struct {
	Function	*FunctionAdd		`json:"function,omitempty"`
	Mware		*MwareAdd		`json:"mware,omitempty"`
}

type DeployStart struct {
	Name		string			`json:"name"`
	Project		string			`json:"project,omitempty"`
	Items		[]*DeployItem		`json:"items"`
}

type DeployItemInfo struct {
	Type		string			`json:"type"`
	Name		string			`json:"name"`
	State		string			`json:"state,omitempty"`
}

type DeployInfo struct {
	Id		string			`json:"id,omitempty"`
	Name		string			`json:"name"`
	Project		string			`json:"project"`
	Labels		[]string		`json:"labels,omitempty"`
	State		string			`json:"state"`
	Items		[]*DeployItemInfo	`json:"items"`
}

type AuthInfo struct {
	Id		string			`json:"id"`
	Name		string			`json:"name"`
}

type AuthAdd struct {
	Name		string			`json:"name"`
	Type		string			`json:"type"`
}

type RepoAdd struct {
	URL		string			`json:"url"`
	UserData	string			`json:"userdata,omitempty"`
}

type RepoInfo struct {
	ID		string			`json:"id"`
	URL		string			`json:"url"`
	State		string			`json:"state"`
	Commit		string			`json:"commit"`
	UserData	string			`json:"userdata,omitempty"`
}
