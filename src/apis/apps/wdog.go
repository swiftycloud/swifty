package swyapi

type SwdFunctionDesc struct {
	PodToken	string		`json:"podtoken"`
	Dir		string		`json:"dir"`
	Stats		string		`json:"stats"`
	Timeout		uint64		`json:"timeout"`
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
