/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package xkst

type KeystoneDomain struct {
	Id		string			`json:"id,omitempty"`
	Name		string			`json:"name,omitempty"`
}

type KeystoneUser struct {
	Id		string			`json:"id,omitempty"`
	Name		string			`json:"name,omitempty"`
	Password	string			`json:"password,omitempty"`
	OrigPassword	string			`json:"original_password,omitempty"`
	Domain		*KeystoneDomain		`json:"domain,omitempty"`
	DomainId	string			`json:"domain_id,omitempty"`
	DefProject	string			`json:"default_project_id,omitempty"`
	Description	string			`json:"description,omitempty"`
	Enabled		*bool			`json:"enabled,omitempty"`
}

type KeystonePassword struct {
	User		KeystoneUser		`json:"user"`
}

type KeystoneToken struct {
	Id		string			`json:"id"`
}

type KeystoneIdentity struct {
	Methods		[]string		`json:"methods"`
	Password	*KeystonePassword	`json:"password,omitempty"`
	Token		*KeystoneToken		`json:"token,omitempty"`
}

type KeystoneAuth struct {
	Identity	KeystoneIdentity	`json:"identity"`
}

type KeystoneAuthReq struct {
	Auth		KeystoneAuth		`json:"auth"`
}

type KeystoneProject struct {
	Id		string			`json:"id"`
	Name		string			`json:"name"`
	DomainId	string			`json:"domain_id,omitempty"`
	Domain		*KeystoneDomain		`json:"domain,omitempty"`
}

type KeystoneProjectAdd struct {
	Project		KeystoneProject		`json:"project"`
}

type KeystoneRole struct {
	Id		string			`json:"id"`
	Name		string			`json:"name"`
}

type KeystoneRoleAssignment struct {
	Role		KeystoneRole		`json:"role"`
}

type KeystoneRoleAssignments struct {
	Ass		[]*KeystoneRoleAssignment `json:"role_assignments"`
}

type KeystoneTokenData struct {
	User		KeystoneUser		`json:"user"`
	Project		KeystoneProject		`json:"project"`
	Roles		[]KeystoneRole		`json:"roles"`
	Expires		string			`json:"expires_at"`
}

type KeystoneAuthResp struct {
	Token		KeystoneTokenData	`json:"token"`
}

type KeystoneDomainsResp struct {
	Domains		[]KeystoneDomain	`json:"domains"`
}

type KeystoneRolesResp struct {
	Roles		[]KeystoneRole		`json:"roles"`
}

type KeystoneProjectsResp struct {
	Projects	[]KeystoneProject	`json:"projects"`
}

type KeystoneUsersResp struct {
	Users		[]KeystoneUser		`json:"users"`
}

type KeystoneUserResp struct {
	User		KeystoneUser		`json:"user"`
}
