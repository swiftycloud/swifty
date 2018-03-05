package main

import (
	"github.com/gorilla/mux"
	"net/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	gateFunctions = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "swifty_gate_nr_functions",
			Help: "Number of functions registered",
		},
	)

	gateMwares = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swifty_gate_nr_mwares",
			Help: "Number of middleware instances",
		},
		[]string { "type" },
	)

	gateCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_function_calls",
			Help: "Number of functions invocations",
		},
		[]string { "result" },
	)
)

func PrometheusInit(conf *YAMLConf) error {
	nr, err := dbFuncCount()
	if err != nil {
		return err
	}

	gateFunctions.Set(float64(nr))
	prometheus.MustRegister(gateFunctions)

	nrs, err := dbMwareCount()
	if err != nil {
		return err
	}

	for mt, nr := range(nrs) {
		gateMwares.WithLabelValues(mt).Set(float64(nr))
	}
	prometheus.MustRegister(gateMwares)

	/* XXX: We can pick up the call-counts from the database, but ... */
	prometheus.MustRegister(gateCalls)

	r := mux.NewRouter()
	r.Handle("/metrics", promhttp.Handler())

	psrv := &http.Server{Handler: r, Addr: conf.Daemon.Prometheus}
	go psrv.ListenAndServe()

	glog.Debugf("Prometeus exporter started at %s", conf.Daemon.Prometheus)

	return nil
}
