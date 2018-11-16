/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swyapi

const (
	AdminRole	string	= "swifty.admin"
	UserRole	string	= "swifty.owner"
	UIRole		string	= "swifty.ui"
	MonitorRole	string	= "swifty.monitor"
	NobodyRole	string	= "swifty.nobody"
)

type LangInfo struct {
	Version		string			`json:"version"`
	Packages	[]string		`json:"packages"`
}

type WdogFunctionRunResult struct {
	Return		string		`json:"return"`
	Code		int		`json:"code"`
	Stdout		string		`json:"stdout"`
	Stderr		string		`json:"stderr"`
	Time		uint		`json:"time"` /* usec */
}

type UserLogin struct {
	UserName	string			`json:"username"`
	Password	string			`json:"password"`
}

type UserToken struct {
	Endpoint	string			`json:"endpoint"`
	Expires		string			`json:"expires,omitempty"`
}

type PgRequest struct {
        Token   string  `json:"token"`
        User    string  `json:"user"`
        Pass    string  `json:"pass,omitempty"`
        DbName  string  `json:"dbname"`
}

type FunctionLimits struct {
	Rate		uint	`json:"rate,omitempty",bson:"rate,omitempty"`
	Burst		uint	`json:"burst,omitempty",bson:"burst,omitempty"`
	MaxInProj	uint	`json:"maxinproj,omitempty",bson:"maxinproj,omitempty"`
	GBS		float64	`json:"gbs,omitempty",bson:"gbs,omitempty"`
	BytesOut	uint64	`json:"bytesout,omitempty",bson:"bytesout,omitempty"`
}

type PackagesLimits struct {
	DiskSizeK	uint64	`json:"disk_size_kb"` // KB
}

type ReposLimits struct {
	Number		uint32	`json:"number"`
}

type UserLimits struct {
	UId	string			`json:"-",bson:"uid"`
	PlanId	string			`json:"planid",bson:"planid"`
	Fn	*FunctionLimits		`json:"function,omitempty",bson:"function,omitempty"`
	Pkg	*PackagesLimits		`json:"packages,omitempty",bson:"packages,omitempty"`
	Repo	*ReposLimits		`json:"repos,omitempty",bson:"repos,omitempty"`
}

type Package struct {
	Name	string		`json:"name"`
}
