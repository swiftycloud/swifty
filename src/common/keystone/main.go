package xkst

import (
	"sync"
	"time"
	"net/http"
	"../http"
	"../../apis"
)

const (
	SwyAdminRole	string	= "swifty.admin"
	SwyUserRole	string	= "swifty.owner"
	SwyUIRole	string	= "swifty.ui"
	KsTokenCacheExpires time.Duration = 60 * time.Second
)

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
	lock	sync.Mutex
}

type KeystoneReq struct {
	Type		string
	URL		string
	Succ		int
	Headers		map[string]string
	NoTok		bool

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
	var cToken string
	headers := make(map[string]string)
retry:
	if kc.Token != "" && !ksreq.NoTok {
		cToken = kc.Token
		headers["X-Auth-Token"] = cToken
	}

	for h, hv := range(ksreq.Headers) {
		headers[h] = hv
	}

	resp, err := xhttp.MarshalAndPost(
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
		return err
	}

	defer resp.Body.Close()
	ksreq.outToken = resp.Header.Get("X-Subject-Token")

	if out != nil {
		err = xhttp.ReadAndUnmarshalResp(resp, out)
		if err != nil {
			return err
		}
	}

	return nil
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

	err := kc.MakeReq(&req, nil, &out)
	if err != nil {
		return nil, http.StatusUnauthorized /* FIXME -- get status from keystone too */
	}

	v, loaded := tdCache.LoadOrStore(token, &out.Token)
	if !loaded {
		time.AfterFunc(KsTokenCacheExpires, func() { tdCache.Delete(token) })
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
