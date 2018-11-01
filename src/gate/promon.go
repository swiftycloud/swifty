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

	gateRouters = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "swifty_gate_nr_routers",
			Help: "Number of routers registered",
		},
	)

	gateDeploys = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "swifty_gate_nr_deploys",
			Help: "Number of deployments registered",
		},
	)

	gateCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_function_calls",
			Help: "Number of functions invocations",
		},
		[]string { "lang", "result" },
	)

	gateBuilds = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_builds",
			Help: "Number of functions builds",
		},
		[]string { "result" },
	)

	contextRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_context_runs",
			Help: "Number of contexts of different types seen",
		},
		[]string { "description" },
	)

	repoPulls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_repo_pulls",
			Help: "Number of repository pull-s",
		},
		[]string { "reason" },
	)

	repoPllErrs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swifty_repo_pull_errs",
			Help: "Number of failed repo pulls",
		},
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

	wdogWaitLat = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "swifty_wdog_up_lat",
			Help: "Time it takes gate to wait for wdog port open",
			Buckets: []float64{
				(  5 * time.Millisecond).Seconds(), /* Immediately */
				( 50 * time.Millisecond).Seconds(),
				(100 * time.Millisecond).Seconds(),
				(200 * time.Millisecond).Seconds(),
				(400 * time.Millisecond).Seconds(),
				(800 * time.Millisecond).Seconds(),
			},
		},
	)
)

func PrometheusInit(ctx context.Context) error {
	nr, err := dbFuncCount(ctx)
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

	nr, err = dbRouterCount(ctx)
	if err != nil {
		return err
	}

	gateRouters.Set(float64(nr))
	prometheus.MustRegister(gateRouters)

	nr, err = dbDeployCount(ctx)
	if err != nil {
		return err
	}

	gateDeploys.Set(float64(nr))
	prometheus.MustRegister(gateDeploys)

	/* XXX: We can pick up the call-counts from the database, but ... */
	prometheus.MustRegister(gateCalls)
	prometheus.MustRegister(gateBuilds)
	prometheus.MustRegister(gateCalLat)
	prometheus.MustRegister(wdogWaitLat)
	prometheus.MustRegister(contextRuns)
	prometheus.MustRegister(repoPulls)
	prometheus.MustRegister(repoPllErrs)

	r := mux.NewRouter()
	r.Handle("/metrics", promhttp.Handler())

	psrv := &http.Server{Handler: r, Addr: conf.Daemon.Prometheus}
	go psrv.ListenAndServe()

	ctxlog(ctx).Debugf("Prometeus exporter started at %s", conf.Daemon.Prometheus)

	return nil
}
