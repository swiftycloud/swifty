package xhttp

import (
	"encoding/json"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"strconv"
	"errors"
	"bytes"
	"time"
	"fmt"
	"io"
)

const (
	StatusTimeoutOccurred = 524
)

type RestReq struct {
	Method		string
	Address		string
	Timeout		uint
	Headers		map[string]string
	Success		int
	Certs		[]byte
}

func SetCORS(w http.ResponseWriter, methods []string, headers []string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ","))
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(headers, ","))
	w.Header().Set("Access-Control-Expose-Headers", strings.Join(headers, ","))
}

func HandleCORS(w http.ResponseWriter, r *http.Request, methods []string, headers []string) bool {
	SetCORS(w, methods, headers)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return true
	}

	return false
}

func RReq(r *http.Request, data interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(data)
}

func RResp(r *http.Response, data interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(data)
}

func Respond(w http.ResponseWriter, data interface{}) error {
	return Respond2(w, data, http.StatusOK)
}

func Respond2(w http.ResponseWriter, data interface{}, status int) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

func Req(req *RestReq, data interface{}) (*http.Response, error) {
	if req.Timeout == 0 {
		req.Timeout = 15
	}

	var c = &http.Client{
		Timeout: time.Duration(req.Timeout) * time.Second,
	}

	if req.Certs != nil {
		cpool := x509.NewCertPool()
		cpool.AppendCertsFromPEM(req.Certs)
		c.Transport = &http.Transport{ TLSClientConfig: &tls.Config{ RootCAs: cpool }}
	}

	var req_body io.Reader

	if data != nil {
		jdata, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("\tMarshal error: %s", err.Error())
		}

		req_body = bytes.NewBuffer(jdata)
	}

	if req.Method == "" {
		req.Method = "POST"
	}

	r, err := http.NewRequest(req.Method, req.Address, req_body)
	if err != nil {
		return nil, fmt.Errorf("\thttp.NewRequest error: %s", err.Error())
	}

	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	for hk, hv := range req.Headers {
		r.Header.Set(hk, hv)
	}

	rsp, err := c.Do(r)
	if err != nil {
		return nil, fmt.Errorf("\thttp.Do() error %s", err.Error())
	}

	if req.Success == 0 {
		req.Success = http.StatusOK
	}

	if rsp.StatusCode != req.Success {
		err = fmt.Errorf("\tResponse is not OK: %d", rsp.StatusCode)
		return rsp, err
	}

	return rsp, nil
}

type YAMLConfHTTPS struct {
	Cert		string			`yaml:"cert"`
	Key		string			`yaml:"key"`
}

func isLocal(addr string) bool {
	/* XXX -- not 100% nice */
	return strings.HasPrefix(addr, "localhost:") ||
		strings.HasPrefix(addr, "127.0.0.1:") ||
		strings.HasPrefix(addr, "::1:")
}

func ListenAndServe(srv *http.Server, https *YAMLConfHTTPS, devel bool, log func(string)) error {
	if https != nil {
		log("Going https")
		return srv.ListenAndServeTLS(https.Cert, https.Key)
	}

	if devel || isLocal(srv.Addr) {
		log("Going plain http")
		return srv.ListenAndServe()
	}

	return errors.New("Can't go non-https in production mode")
}

func ReqAtoi(q url.Values, n string, def int) (int, error) {
	aux := q.Get(n)
	val := def
	if aux != "" {
		var err error
		val, err = strconv.Atoi(aux)
		if err != nil {
			return def, err
		}
	}

	return val, nil
}

func ReadFromURL(url string) ([]byte, error) {
	resp, err := http.DefaultClient.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)

}
