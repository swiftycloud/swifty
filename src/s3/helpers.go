package main

import (
	"crypto/sha256"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const S3TimeStampMax = int64(0x7fffffffffffffff)

func current_timestamp() int64 {
	return time.Now().Unix()
}

func base64_encode(s []byte) string {
	return base64.StdEncoding.EncodeToString(s)
}

func base64_decode(s string) []byte {
	d, _ := base64.StdEncoding.DecodeString(s)
	return d
}

func md5sum(s []byte) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func sha256sum(s []byte) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func urlParam(m url.Values, param string) (string, bool) {
	if v, ok := m[param]; ok {
		if len(v) > 0 {
			return v[0], true
		} else {
			return "", true
		}
	}
	return "", false
}

func getURLParam(r *http.Request, param string) (string, bool) {
	return urlParam(r.URL.Query(), param)
}

func urlValue(m url.Values, param string) (string) {
	val, _ := urlParam(m, param)
	return val
}

func getURLValue(r *http.Request, param string) (string) {
	val, _ := getURLParam(r, param)
	return val
}

func getURLBool(r *http.Request, param string) (bool) {
	val, _ := getURLParam(r, param)
	if strings.ToLower(val) == "true" { return true }
	return false
}

func HTTPMarshalXMLAndWrite(w http.ResponseWriter, status int, data interface{}) error {
	xdata, err := xml.Marshal(data)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)
	w.Write(xdata)
	return nil
}

func HTTPMarshalXMLAndWriteOK(w http.ResponseWriter, data interface{}) error {
	return HTTPMarshalXMLAndWrite(w, http.StatusOK, data)
}

func HTTPRespXML(w http.ResponseWriter, data interface{}) {
	err := HTTPMarshalXMLAndWrite(w, http.StatusOK, data)
	if err != nil {
		HTTPRespError(w, S3ErrInternalError, err.Error())
	}
}
