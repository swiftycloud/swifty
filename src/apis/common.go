/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swyapi

import (
	"encoding/json"
)

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
	Then		json.RawMessage	`json:"then"`
}

type UserLogin struct {
	UserName	string			`json:"username,omitempty"`
	Password	string			`json:"password,omitempty"`
	CredsKey	string			`json:"cred_key,omitempty"`
	CredsSecret	string			`json:"cred_secret,omitempty"`
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
	Rate		uint	`json:"rate,omitempty" yaml:"rate,omitempty"`
	Burst		uint	`json:"burst,omitempty" yaml:"burst,omitempty"`
	Max		uint	`json:"max,omitempty" yaml:"max,omitempty"`
	GBS		float64	`json:"gbs,omitempty" yaml:"gbs,omitempty"`
	BytesOut	uint64	`json:"bytesout,omitempty" yaml:"bytesout,omitempty"`
}

type PackagesLimits struct {
	DiskSizeK	uint64	`json:"disk_size_kb" yaml:"disk_size_kb"` // KB
}

type ReposLimits struct {
	Number		uint32	`json:"number" yaml:"number"`
}

type MwareLimits struct {
	Number		uint32	`json:"number" yaml:"number"`
}

type S3Limits struct {
	SpaceMB		uint64	`json:"space_mb" yaml:"space_mb"`
	DownloadMB	uint64	`json:"download_mb yaml:"download_mb"`
}

type UserLimits struct {
	UId	string			`json:"-" bson:"uid"`
	PlanId	string			`json:"planid" bson:"planid"`
	PlanNm	string			`json:"plan_name" bson:"plan_name"`
	Fn	*FunctionLimits		`json:"function,omitempty" bson:"function,omitempty"`
	Pkg	*PackagesLimits		`json:"packages,omitempty" bson:"packages,omitempty"`
	Repo	*ReposLimits		`json:"repos,omitempty" bson:"repos,omitempty"`
	Mware	map[string]*MwareLimits	`json:"mware,omitempty" bson:"mware,omitempty"`
	S3	*S3Limits		`json:"s3,omitempty" bson:"s3,omitempty"`
}

type Package struct {
	Name	string		`json:"name"`
}
