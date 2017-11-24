package main

import (
	"encoding/xml"
	"net/http"
	"strings"
)

func member(auth, start_with, end_with string) string {
	var start, stop int

	start = strings.Index(auth, start_with)
	if start > 0 {
		start += 11
		stop = strings.Index(auth[start:], end_with)
		if stop > 0 {
			stop += start
			return auth[start:stop]
		}
	}

	return ""
}

func HTTPMarshalXMLAndWrite(w http.ResponseWriter, data interface{}) error {
	xdata, err := xml.Marshal(data)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	//w.Header().Set("X-Amz-Date", "20171124T152411Z")
	//w.Header().Set("date", "20171124T152411Z")
	w.WriteHeader(http.StatusOK)
	w.Write(xdata)
	return nil
}
