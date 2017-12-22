package swyapi

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
	LastCall	string			`json:"lastcall,omitempty"`
}

type FunctionInfo struct {
	Mware		[]string		`json:"mware"`
	State		string			`json:"state"`
	Version		string			`json:"version"`
	Code		FunctionCode		`json:"code"`
	Event		FunctionEvent		`json:"event"`
	URL		string			`json:"url"`
	Stats		FunctionStats		`json:"stats"`
	Size		FunctionSize		`json:"size"`
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

type MwareCinfo struct {
	Project		string			`json:"project"`
	MwId		string			`json:"id"`
}

type MwareCinfoResp struct {
	Envs		[][2]string		`json:"envs"`
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
}

type FunctionUpdate struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
	Code		string			`json:"code"`
	Size		*FunctionSize		`json:"size,omitempty"`
	Mware		*[]string		`json:"mware,omitempty"`
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
}

type ProjectItem struct {
	Project		string			`json:"project"`
}

type FunctionLogEntry struct {
	Event		string			`json:"event"`
	Ts		string			`json:"ts"`
	Text		string			`json:"text"`
}
