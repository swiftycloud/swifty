package swyks

import (
	"net/http"
	"../../common"
	"../../apis/apps"
)

const (
	SwyAdminRole	string	= "swifty.admin"
	SwyUserRole	string	= "swifty.owner"
	SwyUIRole	string	= "swifty.ui"
)

type KeystoneDomain struct {
	Id		string			`json:"id,omitempty"`
	Name		string			`json:"name,omitempty"`
}

type KeystoneUser struct {
	Id		string			`json:"id,omitempty"`
	Name		string			`json:"name,omitempty"`
	Password	string			`json:"password,omitempty"`
	Domain		*KeystoneDomain		`json:"domain,omitempty"`
	DomainId	string			`json:"domain_id,omitempty"`
	DefProject	string			`json:"default_project_id,omitempty"`
	Description	string			`json:"description,omitempty"`
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

type KeystoneTokenData struct {
	Project		KeystoneProject		`json:"project"`
	Roles		[]KeystoneRole		`json:"roles"`
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

func KeystoneRoleHas(td *KeystoneTokenData, wrole string) bool {
	for _, role := range td.Roles {
		if role.Name == wrole {
			return true
		}
	}

	return false
}

type KeystoneReq struct {
	Type		string
	Addr		string
	URL		string
	Token		string
	Succ		int
}

func KeystoneMakeReq(ksreq *KeystoneReq, in interface{}, out interface{}) error {
	headers := make(map[string]string)
	if ksreq.Token != "" {
		headers["X-Auth-Token"] = ksreq.Token
	}

	resp, err := swy.HTTPMarshalAndPost(
			&swy.RestReq{
				Method:  ksreq.Type,
				Address: "http://" + ksreq.Addr + "/v3/" + ksreq.URL,
				Headers: headers,
				Success: ksreq.Succ,
			}, in)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	ksreq.Token = resp.Header.Get("X-Subject-Token")

	if out != nil {
		err = swy.HTTPReadAndUnmarshalResp(resp, out)
		if err != nil {
			return err
		}
	}

	return nil
}

func KeystoneGetTokenData(addr, token string) (*KeystoneTokenData, int) {
	var out KeystoneAuthResp

	req := KeystoneReq {
		Type:		"POST",
		Addr:		addr,
		URL:		"auth/tokens",
		Succ:		201,
	}

	err := KeystoneMakeReq(&req, &KeystoneAuthReq {
		Auth: KeystoneAuth{
			Identity: KeystoneIdentity{
				Methods: []string{"token"},
				Token: &KeystoneToken{
					Id: token,
				},
			},},}, &out)
	if err != nil {
		return nil, http.StatusUnauthorized /* FIXME -- get status from keystone too */
	}

	return &out.Token, 0
}

func KeystoneAuthWithPass(addr, domain string, up *swyapi.UserLogin) (string, error) {
	req := KeystoneReq {
		Type:		"POST",
		Addr:		addr,
		URL:		"auth/tokens",
		Succ:		201,
	}

	err := KeystoneMakeReq(&req, &KeystoneAuthReq {
		Auth: KeystoneAuth{
			Identity: KeystoneIdentity{
				Methods: []string{"password"},
				Password: &KeystonePassword{
					User: KeystoneUser{
						Domain: &KeystoneDomain {
							Name: domain,
						},
						Name: up.UserName,
						Password: up.Password,
					}, }, }, },
				}, nil)
	return req.Token, err
}
