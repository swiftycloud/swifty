package main

import (
	"github.com/gorilla/mux"
	"net/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	/*
	 * targets:        [b]ucket, [o]bject, [u]ploads
	 * actions: ls      +         +         +
	 *          put     +         +         +
	 *          del     +         +         +
	 *          acc     +         +
	 *            ?               get       ini fin lp
	 */
	apiCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swys3_api_calls",
			Help: "Number of API invocations",
		},
		[]string { "target", "action" },
	)
)

func PrometheusInit(conf *YAMLConf) error {
	prometheus.MustRegister(apiCalls)

	r := mux.NewRouter()
	r.Handle("/metrics", promhttp.Handler())

	psrv := &http.Server{Handler: r, Addr: conf.Daemon.Prometheus}
	go psrv.ListenAndServe()

	log.Debugf("Prometeus exporter started at %s", conf.Daemon.Prometheus)

	return nil
}
