package swy

import (
	"fmt"
	"bytes"
	"encoding/json"
	"net/http"
	"io/ioutil"
	"io"
)

type KeystoneDomain struct {
	Id		string			`json:"id,omitempty"`
	Name		string			`json:"name,omitempty"`
}

type KeystoneUser struct {
	Id		string			`json:"id,omitempty"`
	Name		string			`json:"name"`
	Password	string			`json:"password"`
	Domain		*KeystoneDomain		`json:"domain,omitempty"`
	DomainId	string			`json:"domain_id,omitempty"`
	DefProject	string			`json:"default_project_id,omitempty"`
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

func KeystoneRoleHas(resp *KeystoneAuthResp, name string) bool {
	for _, role := range resp.Token.Roles {
		if role.Name == name {
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
	var req_body io.Reader

	clnt := &http.Client{}

	if in != nil {
		bj, err := json.Marshal(in)
		if err != nil {
			return err
		}

		req_body = bytes.NewBuffer(bj)
	}

	req, err := http.NewRequest(ksreq.Type, "http://" + ksreq.Addr + "/v3/" + ksreq.URL, req_body)
	if err != nil {
		return err
	}

	if req_body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if ksreq.Token != "" {
		req.Header.Set("X-Auth-Token", ksreq.Token)
	}

	resp, err := clnt.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != ksreq.Succ {
		return fmt.Errorf("Bad responce from server: " + string(resp.Status))
	}

	ksreq.Token = resp.Header.Get("X-Subject-Token")

	if out != nil {
		resp_body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		err = json.Unmarshal(resp_body, out)
		if err != nil {
			return err
		}
	}

	return nil
}


func KeystoneVerify(addr, token, role string) (string, int) {
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
		return "", http.StatusUnauthorized /* FIXME -- get status from keystone too */
	}

	if !KeystoneRoleHas(&out, role) {
		return "", http.StatusForbidden
	}

	return out.Token.Project.Name, 0
}

func KeystoneAuthWithPass(addr, domain, user, pass string) (string, error) {
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
						Name: user,
						Password: pass,
					}, }, }, },
				}, nil)
	return req.Token, err
}
