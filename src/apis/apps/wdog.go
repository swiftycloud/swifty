package swyapi

type SwdFunctionDesc struct {
	PodToken	string		`json:"podtoken"`
	Run		[]string	`json:"run"`
	Dir		string		`json:"dir"`
	Stats		string		`json:"stats"`
	URLCall		bool		`json:"urlcall"`
}

type SwdStats struct {
	Called		uint64		`json:"called"`
}

type SwdFunctionRun struct {
	PodToken	string		`json:"podtoken"`
	Args		[]string	`json:"args"`
}

type SwdFunctionRunResult struct {
	Code		int		`json:"code"`
	Stdout		string		`json:"stdout"`
	Stderr		string		`json:"stderr"`
}
