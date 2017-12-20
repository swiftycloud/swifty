package main

import (
	"encoding/xml"
	"net/http"
)

func getURLParam(r *http.Request, param string) (string, bool) {
	if v, ok := r.URL.Query()[param]; ok {
		if len(v) > 0 {
			return v[0], true
		} else {
			return "", true
		}
	}
	return "", false
}

func getURLValue(r *http.Request, param string) (string) {
	val, _ := getURLParam(r, param)
	return val
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
