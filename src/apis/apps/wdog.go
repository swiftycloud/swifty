package swyapi

type SwdFunctionRun struct {
	PodToken	string			`json:"podtoken"`
	Args		map[string]string	`json:"args"`
}

type SwdFunctionRunResult struct {
	Return		string		`json:"return"`
	Code		int		`json:"code"`
	Stdout		string		`json:"stdout"`
	Stderr		string		`json:"stderr"`
	Time		uint		`json:"time"` /* usec */
}
