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
}

type FunctionInfo struct {
	Mware		[]string		`json:"mware"`
	State		string			`json:"state"`
	Commit		string			`json:"commit"`
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
	Timeout		uint64			`json:"timeout"`
}

type FunctionEvent struct {
	Source		string			`json:"source"`
	CronTab		string			`json:"crontab"`
	MwareId		string			`json:"mwid"`
	MQueue		string			`json:"mqueue"`
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
	Code		int			`json:"code"`
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
	Commit		string			`json:"commit"`
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
