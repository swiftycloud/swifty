package swyapi

type FunctionList struct {
	Project		string			`json:"project"`
}

type ProjectList struct {
}

type UserLogin struct {
	UserName	string			`json:"username"`
	Password	string			`json:"password"`
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
}

type RunCmd struct {
	Exe		string			`json:"exe"`
	Args		[]string		`json:"args,omitempty"`
}

type FunctionCode struct {
	Lang		string			`json:"lang"`
	Script		string			`json:"script,omitempty"`
	Function	string			`json:"function,omitempty"`
	Env		[]string		`json:"env"`
}

type FunctionSources struct {
	Type		string			`json:"type"`
	Repo		string			`json:"repo,omitempty"`
	Code		string			`json:"code,omitempty"`
}

type FunctionSize struct {
	Replicas	int			`json:"replicas"`
	Memory		string			`json:"memory"`
}

type FunctionEvent struct {
	Source		string			`json:"source"`
	CronTab		string			`json:"crontab"`
	MwareId		string			`json:"mwid"`
	MQueue		string			`json:"mqueue"`
}

type MwareItem struct {
	ID		string			`json:"id"`
	Type		string			`json:"type"`
}

type MwareAdd struct {
	Project		string			`json:"project"`
	Mware		[]MwareItem		`json:"mware"`
}

type MwareRemove struct {
	Project		string			`json:"project"`
	MwareIDs	[]string		`json:"mware"`
}

type MwareCinfo struct {
	Project		string			`json:"project"`
	MwId		string			`json:"mwid"`
}

type MwareCinfoResp struct {
	Envs		[]string		`json:"envs"`
}

type MwareGetItem struct {
	MwareItem
	JSettings	string			`json:"jsettings"`
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
	Args		[]string		`json:"args,omitempty"`
}

type FunctionRunResult struct {
	Code		int			`json:"code"`
	Stdout		string			`json:"stdout"`
	Stderr		string			`json:"stderr"`
}

type FunctionID struct {
	Project		string			`json:"project"`
	FuncName	string			`json:"name"`
}

type FunctionItem struct {
	FuncName	string			`json:"name"`
	State		string			`json:"state"`
}

type ProjectItem struct {
	Project		string			`json:"project"`
}

type FunctionLogEntry struct {
	Commit		string			`json:"commit"`
	Event		string			`json:"event"`
	Ts		string			`json:"ts"`
	Text		string			`json:"text"`
}
