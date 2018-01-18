package swyhttp

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"bytes"
	"time"
	"fmt"
	"io"
)

type RestReq struct {
	Method		string
	Address		string
	Timeout		uint
	Headers		map[string]string
	Success		int
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

func ReadAndUnmarshalReq(r *http.Request, data interface{}) error {
	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("\tCan't parse request: %s", err.Error())
	}

	err = json.Unmarshal(body, data)
	if err != nil {
		return fmt.Errorf("\tUnmarshal error: %s", err.Error())
	}

	return nil
}

func ReadAndUnmarshalResp(r *http.Response, data interface{}) error {
	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("\tCan't parse request: %s", err.Error())
	}

	err = json.Unmarshal(body, data)
	if err != nil {
		return fmt.Errorf("\tUnmarshal error: %s", err.Error())
	}

	return nil
}

func MarshalAndWrite(w http.ResponseWriter, data interface{}) error {
	jdata, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("\tMarshal error: %s", err.Error())
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(jdata)

	return nil
}

func MarshalAndPost(req *RestReq, data interface{}) (*http.Response, error) {
	if req.Timeout == 0 {
		req.Timeout = 15
	}

	var c = &http.Client{
		Timeout: time.Duration(req.Timeout) * time.Second,
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
