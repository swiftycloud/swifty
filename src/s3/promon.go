package main

import (
	"github.com/gorilla/mux"
	"net/http"
	// "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func PrometheusInit(conf *YAMLConf) error {
	r := mux.NewRouter()
	r.Handle("/metrics", promhttp.Handler())

	psrv := &http.Server{Handler: r, Addr: conf.Daemon.Prometheus}
	go psrv.ListenAndServe()

	log.Debugf("Prometeus exporter started at %s", conf.Daemon.Prometheus)

	return nil
}
