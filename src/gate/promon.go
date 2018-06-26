package main

import (
	"github.com/gorilla/mux"
	"net/http"
	"time"
	"context"
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

	wdogErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_wdog_errs",
			Help: "Number of errors talking to wdog",
		},
		[]string { "code" },
	)

	gateCalLat = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "swifty_gate_call_latency",
			Help: "Call latency added by gate-wdog interaction",
			Buckets: []float64{
				(500 * time.Microsecond).Seconds(), /* Ethernet latency ~200 usec */
				(  1 * time.Millisecond).Seconds(),
				(  2 * time.Millisecond).Seconds(),
				(  5 * time.Millisecond).Seconds(),
				( 10 * time.Millisecond).Seconds(), /* Internet ping time ~10 msec */
				(100 * time.Millisecond).Seconds(),
				(500 * time.Millisecond).Seconds(), /* More than 0.5 sec overhead is ... too bad */
			},
		},
	)
)

func PrometheusInit(ctx context.Context, conf *YAMLConf) error {
	nr, err := dbFuncCount()
	if err != nil {
		return err
	}

	gateFunctions.Set(float64(nr))
	prometheus.MustRegister(gateFunctions)

	nrs, err := dbMwareCount(ctx)
	if err != nil {
		return err
	}

	for mt, nr := range(nrs) {
		gateMwares.WithLabelValues(mt).Set(float64(nr))
	}
	prometheus.MustRegister(gateMwares)

	/* XXX: We can pick up the call-counts from the database, but ... */
	prometheus.MustRegister(gateCalls)
	prometheus.MustRegister(gateCalLat)

	r := mux.NewRouter()
	r.Handle("/metrics", promhttp.Handler())

	psrv := &http.Server{Handler: r, Addr: conf.Daemon.Prometheus}
	go psrv.ListenAndServe()

	glog.Debugf("Prometeus exporter started at %s", conf.Daemon.Prometheus)

	return nil
}
