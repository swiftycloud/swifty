package main

import (
	"fmt"
	"bytes"
	"encoding/json"
	"net/http"
	"io/ioutil"
)

type KeystoneDomain struct {
	Id		string			`json:"id"`
	Name		string			`json:"name,omitempty"`
}

type KeystoneUser struct {
	Domain		KeystoneDomain		`json:"domain"`
	Name		string			`json:"name"`
	Password	string			`json:"password"`
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
	Domain		KeystoneDomain		`json:"domain"`
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

func reqKsToken(conf *YAMLConfKeystone, url string, in interface{}, out interface{}) (string, error) {
	clnt := &http.Client{}

	body, err := json.Marshal(in)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "http://" + conf.Addr + "/v3/" + url, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := clnt.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return "", fmt.Errorf("Bad responce from server: " + string(resp.Status))
	}

	if out == nil {
		return resp.Header.Get("X-Subject-Token"), nil
	}

	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	err = json.Unmarshal(body, out)
	if err != nil {
		return "", err
	}

	return "", nil
}

func KeystoneAuthWithPass(conf *YAMLConfKeystone, username, password string) (string, error) {
	token, err := reqKsToken(conf, "auth/tokens", &KeystoneAuthReq {
		Auth: KeystoneAuth{
			Identity: KeystoneIdentity{
				Methods: []string{"password"},
				Password: &KeystonePassword{
					User: KeystoneUser{
						Domain: KeystoneDomain {
							Id: conf.Domain,
						},
						Name: username,
						Password: password,
					}, }, }, },
				}, nil)
	if err != nil {
		log.Errorf("Error authenticating user: " + err.Error())
	}

	return token, err
}

func KeystoneVerify(conf *YAMLConfKeystone, token string) (string, int) {
	var out KeystoneAuthResp

	_, err := reqKsToken(conf, "auth/tokens", &KeystoneAuthReq {
		Auth: KeystoneAuth{
			Identity: KeystoneIdentity{
				Methods: []string{"token"},
				Token: &KeystoneToken{
					Id: token,
				},
			},},}, &out)
	if err != nil {
		log.Error("Error checking user token: " + err.Error())
		return "", http.StatusUnauthorized /* FIXME -- get status from keystone too */
	}

	/*
	 * Keystone project (formerly tennant) is the tennant in swifty terms --
	 * a set of swifty projects (each of which is a set of functions)
	 * available (for now) to a single user.
	 *
	 * Thus, when registering in keystone, there should appear a user of
	 * desired name, a porject of some corresponding name and a role
	 * tieing these two named "swifty.owner".
	 */

	/*
	 * For now we only have one role -- owner -- which allows the holder
	 * to do anything with everything.
	 */

	if len(out.Token.Roles) != 1 || out.Token.Roles[0].Name != "swifty.owner" {
		log.Error("Error in roles -- need a swifty.owner one")
		return "", http.StatusForbidden
	}

	return out.Token.Project.Name, 0
}
