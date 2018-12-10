/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swyapi

type UserInfo struct {
	ID	string		`json:"id"`
	UId	string		`json:"uid"`
	Name	string		`json:"name,omitempty"`
	Enabled	bool		`json:"enabled,omitempty"`
	Created	string		`json:"created,omitempty"`
	Roles	[]string	`json:"roles,omitempty"`
}

type Creds struct {
	Key	string		`json:"key"`
	Name	string		`json:"name"`
	Secret	string		`json:"secret,omitempty"`
}

type ModUser struct {
	Enabled	*bool		`json:"enabled,omitempty"`
}

type ChangePass struct {
	Password	string			`json:"password"`
	CPassword	string			`json:"current"`
}

type AddUser struct {
	UId	string		`json:"uid"`
	Pass	string		`json:"pass"`
	Name	string		`json:"name"`
	PlanId	string		`json:"planid"`
	PlanNm	string		`json:"plan_name"`
}

type PlanLimits struct {
	Id	string			`json:"id,omitempty" yaml:"-"`
	Name	string			`json:"name" yaml:"name"`
	Descr	string			`json:"description,omitempty" yaml:"description,omitempty"`
	Fn	*FunctionLimits		`json:"function,omitempty" yaml:"function,omitempty"`
	Pkg	*PackagesLimits		`json:"packages,omitempty" yaml:"packages,omitempty"`
	Repo	*ReposLimits		`json:"repos,omitempty" yaml:"repos,omitempty"`
	Mware	map[string]*MwareLimits	`json:"mware,omitempty" yaml:"mware,omitempty"`
	S3	*S3Limits		`json:"s3,omitempty" yaml:"s3,omitempty"`
}
