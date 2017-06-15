package swyapi

type SwdFunctionDesc struct {
	PodToken	string		`json:"podtoken"`
	Run		[]string	`json:"run"`
	Dir		string		`json:"dir"`
	URLCall		bool		`json:"urlcall"`
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
