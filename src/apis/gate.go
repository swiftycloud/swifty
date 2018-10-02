package swyapi

const (
	GateGenErr	uint = 1	// Unclassified error
	GateBadRequest	uint = 2	// Error parsing request data
	GateBadResp	uint = 3	// Error generating responce
	GateDbError	uint = 4	// Error requesting database (except NotFound)
	GateDuplicate	uint = 5	// ID duplication
	GateNotFound	uint = 6	// No resource found
	GateFsError	uint = 7	// Error accessing file(s)
	GateNotAvail	uint = 8	// Operation not available on selected object
)

type ProjectList struct {
}

type ProjectDel struct {
	Project		string			`json:"project"`
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

type TenantStatsFn struct {
	Called		uint64			`json:"called"`
	GBS		float64			`json:"gbs"`
	BytesOut	uint64			`json:"bytesout"`
	Till		string			`json:"till,omitempty"`
	From		string			`json:"from,omitempty"`
}

type TenantStatsMware struct {
	Count		int			`json:"count"`
	DU		*uint64			`json:"disk_usage,omitempty"` /* in ... KB */
}

type TenantStatsResp struct {
	Stats		[]TenantStatsFn			`json:"stats"`
	Mware		map[string]*TenantStatsMware	`json:"mware,omitempty"`
}

type FunctionInfo struct {
	Name		string			`json:"name,omitempty"`
	Project		string			`json:"project,omitempty"`
	Labels		[]string		`json:"labels,omitempty"`
	State		string			`json:"state"`
	Version		string			`json:"version"`
	RdyVersions	[]string		`json:"rversions,omitempty"`
	Code		*FunctionCode		`json:"code,omitempty"`
	URL		string			`json:"url,omitempty"`
	Stats		[]FunctionStats		`json:"stats,omitempty"`
	Size		*FunctionSize		`json:"size,omitempty"`
	AuthCtx		string			`json:"authctx,omitempty"`
	UserData	string			`json:"userdata,omitempty"`
	Id		string			`json:"id"`
}

type FunctionMdat struct {
	Cookie		string			`json:"cookie"`
	RL		[]uint			`json:"rl"`
	BR		[]uint			`json:"br"`
	Hosts		[]string		`json:"hosts,omitempty"`
}

//type RunCmd struct {
//	Exe		string			`json:"exe"`
//	Args		[]interface{}		`json:"args,omitempty"`
//}

type FunctionCode struct {
	Lang		string			`json:"lang"`
	Env		[]string		`json:"env,omitempty"`
}

type FunctionSources struct {
	Type		string			`json:"type"`
	Repo		string			`json:"repo,omitempty"`
	Code		string			`json:"code,omitempty"`
	Sync		bool			`json:"sync"`
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
	AccID		string			`json:"accid"`
}

type FunctionAdd struct {
	Name		string			`json:"name"`
	Project		string			`json:"project,omitempty"`
	Sources		FunctionSources		`json:"sources"`
	Code		FunctionCode		`json:"code"`
	Size		FunctionSize		`json:"size"`
	Mware		[]string		`json:"mware,omitempty"`
	S3Buckets	[]string		`json:"s3buckets,omitempty"`
	Accounts	[]string		`json:"accounts,omitempty"`
	UserData	string			`json:"userdata,omitempty"`
	AuthCtx		string			`json:"authctx,omitempty"`

	Events		[]FunctionEvent		`json:"-" yaml:"events"` /* Deploy only */
}

type FunctionUpdate struct {
	UserData	*string			`json:"userdata,omitempty"`
	State		string			`json:"state,omitempty"`
}

type ProjectItem struct {
	Project		string			`json:"project"`
}

type FunctionLogEntry struct {
	Event		string			`json:"event"`
	Ts		string			`json:"ts"`
	Text		string			`json:"text"`
}

type DeployDescription struct {
	Functions	[]*FunctionAdd		`yaml:"functions"`
	Mwares		[]*MwareAdd		`yaml:"mwares"`
	Routers		[]*RouterAdd		`yaml:"routers"`
}

type DeploySource struct {
	Type		string			`json:"type"`
	Descr		string			`json:"desc,omitempty"`
	Repo		string			`json:"repo,omitempty"`
}

type DeployStart struct {
	Name		string			`json:"name"`
	Project		string			`json:"project,omitempty"`
	From		DeploySource		`json:"from"`
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
	Type		string			`json:"type"`
	URL		string			`json:"url"`
	AccID		string			`json:"account_id,omitempty"`
	UserData	string			`json:"userdata,omitempty"`
	Pull		string			`json:"pulling,omitempty"`
}

type RepoUpdate struct {
	Pull		*string			`json:"pulling,omitempty"`
}

type RepoInfo struct {
	ID		string			`json:"id"`
	Type		string			`json:"type"`
	URL		string			`json:"url"`
	State		string			`json:"state"`
	Commit		string			`json:"commit"`
	UserData	string			`json:"userdata,omitempty"`
	AccID		string			`json:"account_id,omitempty"`
	Pull		string			`json:"pulling,omitempty"`
	Desc		bool			`json:"desc"`
}

type RepoEntry struct {
	Name		string			`json:"name" yaml:"name"`
	Path		string			`json:"path" yaml:"path"`
	Description	string			`json:"desc" yaml:"desc"`
	Lang		string			`json:"lang,omitempty" yaml:"lang,omitempty"`
}

type RepoDesc struct {
	Description	string			`json:"desc" yaml:"desc"`
	Entries		[]*RepoEntry		`json:"files" yaml:"files"`
}

type RepoFile struct {
	Label		string		`json:"label"`
	Path		string		`json:"path"`
	Type		string		`json:"type"`
	Lang		*string		`json:"lang,omitempty"`
	Children	*[]*RepoFile	`json:"children,omitempty"`
}

type LangInfo struct {
	Version		string			`json:"version"`
	Packages	[]string		`json:"packages"`
}

type RouterEntry struct {
	Method		string		`json:"method"`
	Path		string		`json:"path"`
	Call		string		`json:"call"`
	Key		string		`json:"key"`
}

type RouterAdd struct {
	Name		string		`json:"name"`
	Project		string		`json:"project"`
	Table		[]*RouterEntry	`json:"table"`
}

type RouterInfo struct {
	Id		string		`json:"id"`
	Name		string		`json:"name"`
	Project		string		`json:"project"`
	Labels		[]string	`json:"labels,omitempty"`
	TLen		int		`json:"table_len"`
	URL		string		`json:"url"`
}
