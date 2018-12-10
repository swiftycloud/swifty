/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package xkst

import (
	"sync"
	"time"
	"net/http"
	"swifty/common/http"
	"swifty/apis"
)

var TokenCacheExpires time.Duration = 60 * time.Second

func HasRole(td *KeystoneTokenData, wroles ...string) bool {
	for _, role := range td.Roles {
		for _, wrole := range wroles {
			if role.Name == wrole {
				return true
			}
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
	lock	sync.Mutex
}

type KeystoneReq struct {
	Type		string
	URL		string
	Succ		int
	Headers		map[string]string
	NoTok		bool
	CToken		string

	outToken	string
}

func tryRefreshToken(kc *KsClient, token string) error {
	var err error

	kc.lock.Lock()
	defer kc.lock.Unlock()

	/* We might have raced with another updater */
	if kc.Token == token {
		token, _, err = KeystoneAuthWithPass(kc.addr, kc.domain,
				&swyapi.UserLogin{ UserName: kc.user, Password: kc.pass })
		if err == nil {
			kc.Token = token
		}
	}

	return err
}

func (kc *KsClient)MakeReq(ksreq *KeystoneReq, in interface{}, out interface{}) error {
	err, _ := kc.MakeReq2(ksreq, in, out, nil)
	return err
}

func (kc *KsClient)MakeReq2(ksreq *KeystoneReq, in interface{}, out interface{}, flog func(string)) (error, int) {
	var cToken string
	headers := make(map[string]string)
retry:
	if ksreq.CToken != "" {
		if flog != nil {
			flog("Use provided token")
		}
		headers["X-Auth-Token"] = ksreq.CToken
	} else if kc.Token != "" && !ksreq.NoTok {
		if flog != nil {
			flog("Use my token")
		}
		cToken = kc.Token
		headers["X-Auth-Token"] = cToken
	}

	for h, hv := range(ksreq.Headers) {
		headers[h] = hv
	}

	resp, err := xhttp.Req(
			&xhttp.RestReq{
				Method:  ksreq.Type,
				Address: "http://" + kc.addr + "/v3/" + ksreq.URL,
				Headers: headers,
				Success: ksreq.Succ,
			}, in)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized && cToken != "" {
			/* Token has expired. Refresh one */
			err = tryRefreshToken(kc, cToken)
			if err == nil {
				goto retry
			}
		}

		code := -1
		if resp != nil {
			code = resp.StatusCode
		}
		return err, code
	}

	defer resp.Body.Close()
	ksreq.outToken = resp.Header.Get("X-Subject-Token")

	if out != nil {
		err = xhttp.RResp(resp, out)
		if err != nil {
			return err, -1
		}
	}

	return nil, 0
}

var tdCache sync.Map

func KeystoneGetTokenData(addr, token string) (*KeystoneTokenData, int) {
	var out KeystoneAuthResp

	v, ok := tdCache.Load(token)
	if ok {
		return v.(*KeystoneTokenData), 0
	}

	kc := &KsClient { addr: addr, }

	req := KeystoneReq {
		Type:		"GET",
		URL:		"auth/tokens",
		Succ:		200,
		Headers:	map[string]string{
			/* XXX -- each service should rather login to KS itself */
			"X-Auth-Token": token,
			"X-Subject-Token": token,
		},
	}

	err, code := kc.MakeReq2(&req, nil, &out, nil)
	if err != nil {
		switch code {
		case http.StatusUnauthorized, http.StatusNotFound:
			return nil, http.StatusUnauthorized
		default:
			return nil, http.StatusInternalServerError
		}
	}

	v, loaded := tdCache.LoadOrStore(token, &out.Token)
	if !loaded {
		time.AfterFunc(TokenCacheExpires, func() { tdCache.Delete(token) })
	}

	return v.(*KeystoneTokenData), 0
}

func KeystoneAuthWithPass(addr, domain string, up *swyapi.UserLogin) (string, string, error) {
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
				Methods: []string{"password"},
				Password: &KeystonePassword{
					User: KeystoneUser{
						Domain: &KeystoneDomain {
							Name: domain,
						},
						Name: up.UserName,
						Password: up.Password,
					}, }, }, },
				}, &out)

	return req.outToken, out.Token.Expires, err
}

func KeystoneAuthWithAC(addr, domain string, up *swyapi.UserLogin) (string, string, error) {
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
				Methods: []string{"application_credential"},
				AC: &KeystoneApplictionCredentials{
					Id: up.CredsKey,
					Secret: up.CredsSecret,
				},},},}, &out)

	return req.outToken, out.Token.Expires, err
}

func KeystoneConnect(addr, domain string, up *swyapi.UserLogin) (*KsClient, error) {
	token, _, err := KeystoneAuthWithPass(addr, domain, up)
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
