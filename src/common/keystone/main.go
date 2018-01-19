package swyks

import (
	"net/http"
	"../http"
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

type KsClient struct {
	addr	string
	domain	string
	user	string
	pass	string
	Token	string
}

type KeystoneReq struct {
	Type		string
	URL		string
	Succ		int

	outToken	string
}

func (kc *KsClient)MakeReq(ksreq *KeystoneReq, in interface{}, out interface{}) error {
	headers := make(map[string]string)
	if kc.Token != "" {
		headers["X-Auth-Token"] = kc.Token
	}

	resp, err := swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Method:  ksreq.Type,
				Address: "http://" + kc.addr + "/v3/" + ksreq.URL,
				Headers: headers,
				Success: ksreq.Succ,
			}, in)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	ksreq.outToken = resp.Header.Get("X-Subject-Token")

	if out != nil {
		err = swyhttp.ReadAndUnmarshalResp(resp, out)
		if err != nil {
			return err
		}
	}

	return nil
}

func KeystoneGetTokenData(addr, token string) (*KeystoneTokenData, int) {
	var out KeystoneAuthResp

	kc := &KsClient { addr: addr, }

	req := KeystoneReq {
		Type:		"POST",
		URL:		"auth/tokens",
		Succ:		201,
	}

	err := kc.MakeReq(&req, &KeystoneAuthReq {
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
	kc := &KsClient { addr: addr, }

	req := KeystoneReq {
		Type:		"POST",
		URL:		"auth/tokens",
		Succ:		201,
	}

	err := kc.MakeReq(&req, &KeystoneAuthReq {
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

	return req.outToken, err
}

func KeystoneConnect(addr, domain string, up *swyapi.UserLogin) (*KsClient, error) {
	token, err := KeystoneAuthWithPass(addr, domain, up)
	if err != nil {
		return nil, err
	}

	return &KsClient {
		addr:		addr,
		domain:		domain,
		user:		up.UserName,
		pass:		up.Password,
		Token:		token,
	}, nil
}
