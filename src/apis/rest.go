package swyapi

import (
	"net/http"
	"fmt"
	"encoding/json"
	"../common/http"
	"../common/xrest"
)

type Client struct {
	proto	string
	token	string
	gaddr	string
	gport	string
	aaddr	string
	aport	string
	relay	string
	verb	bool
	direct	bool
	admd	bool
	user	string
	pass	string
	stok	func(tok string)
	onerr	func(err error)
}

func MakeClient(user, pass, addr, port string) *Client {
	ret := Client{}
	ret.proto = "https"
	ret.user = user
	ret.pass = pass
	ret.gaddr = addr
	ret.gport = port
	return &ret
}

func (cln *Client)Token(tok string) { cln.token = tok }
func (cln *Client)Relay(rly string) { cln.relay = rly }
func (cln *Client)Verbose() { cln.verb = true }
func (cln *Client)TokSaver(f func(tok string)) { cln.stok = f }
func (cln *Client)OnError(f func(err error)) { cln.onerr = f }
func (cln *Client)Admd(addr, port string) { cln.aaddr = addr; cln.aport = port }
func (cln *Client)NoTLS() { cln.proto = "http" }
func (cln *Client)Direct() { cln.direct = true }
func (cln *Client)ToAdmd(v bool) { cln.admd = v }

func (cln *Client)endpoint() string {
	var ep string

	if !cln.admd {
		ep = cln.gaddr + ":" + cln.gport
		if !cln.direct {
			ep += "/gate"
		}
	} else {
		if cln.aport == "" {
			panic("Admd not set for this command")
		}

		ah := cln.aaddr
		if ah == "" {
			ah = cln.gaddr
		}

		ep = ah + ":" + cln.aport
		if !cln.direct {
			ep += "/admd"
		}
	}

	return ep
}

func (cln *Client)req(method, url string, in interface{}, succ_code int, tmo uint) (*http.Response, error) {
	address := cln.proto + "://" + cln.endpoint() + "/v1/" + url

	h := make(map[string]string)
	if cln.token != "" {
		h["X-Auth-Token"] = cln.token
	}
	if cln.relay != "" {
		h["X-Relay-Tennant"] = cln.relay
	}

//	var crt []byte
//	if conf.TLS && conf.Certs != "" {
//		var err error
//
//		crt, err = ioutil.ReadFile(conf.Certs)
//		if err != nil {
//			return nil, fmt.Errorf("Error reading cert file: %s", err.Error())
//		}
//	}

	if cln.verb {
		fmt.Printf("[%s] %s\n", method, address)
		if in != nil {
			x, err := json.Marshal(in)
			if err == nil {
				fmt.Printf("`- body: %s\n", string(x))
			}
		}
	}

	return xhttp.Req(
			&xhttp.RestReq{
				Method:		method,
				Address:	address,
				Headers:	h,
				Success:	succ_code,
				Timeout:	tmo,
//				Certs:		crt,
			}, in)
}

func (cln *Client)Login() error {
	resp, err := cln.req("POST", "login", UserLogin {
			UserName: cln.user, Password: cln.pass,
		}, http.StatusOK, 0)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bad responce from server: " + string(resp.Status))
	}

	token := resp.Header.Get("X-Subject-Token")
	if token == "" {
		return fmt.Errorf("No auth token from server")
	}

//	var td UserToken
//	err = xhttp.RResp(resp, &td)
//	if err != nil {
//		return fmt.Errorf("Can't unmarshal login resp: %s", err.Error())
//	}

	cln.token = token
	if cln.stok != nil {
		cln.stok(token)
	}

	return nil
}

func (cln *Client)Req2(method, url string, in interface{}, succ_code int, tmo uint) (*http.Response, error) {
	first_attempt := true
again:
	resp, err := cln.req(method, url, in, succ_code, tmo)
	if err != nil {
		if resp == nil {
			if cln.onerr != nil {
				cln.onerr(err)
			}
			return nil, err
		}

		if (resp.StatusCode == http.StatusUnauthorized) && first_attempt {
			resp.Body.Close()
			first_attempt = false
			err := cln.Login()
			if err != nil {
				if cln.onerr != nil {
					cln.onerr(err)
				}
				return nil, err
			}
			goto again
		}

		if resp.StatusCode == http.StatusBadRequest {
			var gerr xrest.ReqErr

			err = xhttp.RResp(resp, &gerr)
			resp.Body.Close()

			if err == nil {
				err = fmt.Errorf("Operation failed (%d): %s", gerr.Code, gerr.Message)
			} else {
				err = fmt.Errorf("Operation failed with no details")
			}
		} else {
			err = fmt.Errorf("Bad responce: %s", string(resp.Status))
		}

		if cln.onerr != nil {
			cln.onerr(err)
		}
		return nil, err
	}

	return resp, nil
}

func (cln *Client)Req1(method, url string, succ int, in interface{}, out interface{}) error {
	resp, err := cln.Req2(method, url, in, succ, 30)
	if err != nil {
		return err
	}

	/* Here we have http.StatusOK */
	defer resp.Body.Close()

	if out != nil {
		err := xhttp.RResp(resp, out)
		if err != nil {
			if cln.onerr != nil {
				cln.onerr(err)
			}
			return err
		}

		if cln.verb {
			dat, _ := json.MarshalIndent(out, "|", "    ")
			fmt.Printf(" `-[%d]->\n|%s\n", resp.StatusCode, string(dat))
			fmt.Printf("---------------8<------------------------------------------\n")
		}
	}

	return nil
}
