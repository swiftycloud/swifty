package main

import (
	"encoding/xml"
	"net/http"
)

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
