package swyapi

type GateErr struct {
	Code		uint			`json:"code"`
	Message		string			`json:"message"`
}

type FunctionList struct {
	Project		string			`json:"project"`
}

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
	TimeW		uint64			`json:"timewdog,omitempty"`
	TimeG		uint64			`json:"timegate,omitempty"`
}

type FunctionWait struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Timeout		uint32			`json:"timeout"` /* msec */
	Version		string			`json:"version,omitempty"`
}

type FunctionInfo struct {
	Mware		[]string		`json:"mware"`
	S3Buckets	[]string		`json:"s3buckets"`
	State		string			`json:"state"`
	Version		string			`json:"version"`
	RdyVersions	[]string		`json:"rversions"`
	Code		FunctionCode		`json:"code"`
	Event		FunctionEvent		`json:"event"`
	URL		string			`json:"url"`
	Stats		FunctionStats		`json:"stats"`
	Size		FunctionSize		`json:"size"`
	UserData	string			`json:"userdata"`
}

type RunCmd struct {
	Exe		string			`json:"exe"`
	Args		[]string		`json:"args,omitempty"`
}

type FunctionCode struct {
	Lang		string			`json:"lang"`
	Env		[]string		`json:"env"`
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

type FunctionEvent struct {
	Source		string			`json:"source"`
	CronTab		string			`json:"crontab,omitempty"`
	MwareId		string			`json:"mwid,omitempty"`
	MQueue		string			`json:"mqueue,omitempty"`
	S3Bucket	string			`json:"s3bucket,omitempty"`
}

type MwareAdd struct {
	Project		string			`json:"project"`
	ID		string			`json:"id"`
	Type		string			`json:"type"`
}

type MwareRemove struct {
	Project		string			`json:"project"`
	ID		string			`json:"id"`
}

type MwareID struct {
	Project		string			`json:"project"`
	ID		string			`json:"id"`
}

type MwareInfo struct {
	MwareID					`json:",inline"`
	Type		string			`json:"type"`
	Envs		map[string]string	`json:"envs"`
}

type MwareItem struct {
	ID		string			`json:"id"`
	Type		string			`json:"type"`
}

type MwareList struct {
	Project		string			`json:"project"`
}

type FunctionAdd struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Sources		FunctionSources		`json:"sources"`
	Code		FunctionCode		`json:"code"`
	Event		FunctionEvent		`json:"event"`
	Size		FunctionSize		`json:"size"`
	Mware		[]string		`json:"mware"`
	S3Buckets	[]string		`json:"s3buckets"`
	UserData	string			`json:"userdata"`
}

type FunctionState struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	State		string			`json:"state"`
}

type FunctionUpdate struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Code		string			`json:"code"`
	Size		*FunctionSize		`json:"size,omitempty"`
	Mware		*[]string		`json:"mware,omitempty"`
	S3Buckets	*[]string		`json:"s3buckets,omitempty"`
	UserData	string			`json:"userdata"`
}

type FunctionRemove struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
}

type FunctionRun struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Args		map[string]string	`json:"args,omitempty"`
}

type FunctionRunResult struct {
	Code		int			`json:"code"`
	Return		string			`json:"return"`
	Stdout		string			`json:"stdout"`
	Stderr		string			`json:"stderr"`
}

type FunctionID struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
}

type FunctionXID struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Version		string			`json:"version"`
}

type FunctionItem struct {
	FuncName	string			`json:"name"`
	State		string			`json:"state"`
	Timeout		uint64			`json:"timeout"`
	LastCall	string			`json:"lastcall,omitempty"`
}

type ProjectItem struct {
	Project		string			`json:"project"`
}

type FunctionLogEntry struct {
	Event		string			`json:"event"`
	Ts		string			`json:"ts"`
	Text		string			`json:"text"`
}
