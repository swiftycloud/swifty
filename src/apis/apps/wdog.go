package swyapi

type SwdFunctionDesc struct {
	PodToken	string		`json:"podtoken"`
	Dir		string		`json:"dir"`
	Timeout		uint64		`json:"timeout"`
}

type SwdFunctionRun struct {
	PodToken	string		`json:"podtoken"`
	Args		[]string	`json:"args"`
}

type SwdFunctionRunResult struct {
	Return		string		`json:"return"`
	Code		int		`json:"code"`
	Stdout		string		`json:"stdout"`
	Stderr		string		`json:"stderr"`
}
