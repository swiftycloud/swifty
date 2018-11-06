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

	gateRepos = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "swifty_gate_nr_repos",
			Help: "Number of repos registered",
		},
	)

	gateDeploys = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "swifty_gate_nr_deploys",
			Help: "Number of deployments registered",
		},
	)

	gateAccounts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swifty_gate_nr_accounts",
			Help: "Number of accounts registered",
		},
		[]string { "type" },
	)

	scalers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "swifty_gate_scalers",
			Help: "Number of active scalers running",
		},
	)

	portWaiters = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "swifty_gate_port_waiters",
			Help: "Number of wdog port waiters",
		},
	)

	srcGCs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "swifty_gate_srcgcs",
			Help: "Number of active source GCs",
		},
	)

	danglingEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_dangling_events",
			Help: "Number of events that do not have target fn",
		},
		[]string { "event" },
	)

	gateCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_function_calls",
			Help: "Number of functions invocations",
		},
		[]string { "event" },
	)

	gateCallErrs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_function_errors",
			Help: "Number of failed functions invocations",
		},
		[]string { "reason" },
	)

	gateBuilds = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_builds",
			Help: "Number of functions builds",
		},
		[]string { "lang", "result" },
	)

	contextRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_context_runs",
			Help: "Number of contexts of different types seen",
		},
		[]string { "description" },
	)

	repoPulls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_repo_pulls",
			Help: "Number of repository pull-s",
		},
		[]string { "reason" },
	)

	repoPllErrs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swifty_gate_repo_pull_errs",
			Help: "Number of failed repo pulls",
		},
	)

	repoScrapeErrs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swifty_gate_repo_scrape_errs",
			Help: "Number of repo scrapes finished with error",
		},
	)

	pkgScans = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swifty_gate_pkg_scans",
			Help: "Number of FS scans for packages",
		},
	)

	limitPullErrs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swifty_gate_limit_pull_err",
			Help: "Number of errors pulling tenant limits",
		},
	)

	wdogErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_gate_wdog_errs",
			Help: "Number of errors talking to wdog",
		},
		[]string { "code" },
	)

	statWrites = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swifty_gate_stat_writes",
			Help: "Number of stats bg flushes into DB",
		},
	)

	statWriteFails = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swifty_gate_stat_write_fails",
			Help: "Number of errors in bg stats flush",
		},
	)

	scaleOverruns = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swifty_gate_scale_overruns",
			Help: "How many times we refused to scale over configured limit",
		},
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
			Name: "swifty_gate_wdog_up_lat",
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

	scalerGoals = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "swifty_gate_scaler_goals",
			Help: "Goals that balancer put on scaler",
			Buckets: []float64{
				 1.05, /* They are ints, so +0.05 to make comparison always the way we want */
				 2.05,
				 4.05,
				 8.05,
				16.05,
				32.05,
				64.05,
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

	nrs, err = dbAccCount(ctx)
	if err != nil {
		return err
	}

	for at, nr := range(nrs) {
		gateAccounts.WithLabelValues(at).Set(float64(nr))
	}
	prometheus.MustRegister(gateAccounts)

	nr, err = dbRouterCount(ctx)
	if err != nil {
		return err
	}

	gateRouters.Set(float64(nr))
	prometheus.MustRegister(gateRouters)

	nr, err = dbRepoCount(ctx)
	if err != nil {
		return err
	}

	gateRepos.Set(float64(nr))
	prometheus.MustRegister(gateRepos)

	nr, err = dbDeployCount(ctx)
	if err != nil {
		return err
	}

	gateDeploys.Set(float64(nr))
	prometheus.MustRegister(gateDeploys)

	/* XXX: We can pick up the call-counts from the database, but ... */
	prometheus.MustRegister(gateCalls)
	prometheus.MustRegister(gateCallErrs)
	prometheus.MustRegister(gateBuilds)
	prometheus.MustRegister(gateCalLat)
	prometheus.MustRegister(wdogWaitLat)
	prometheus.MustRegister(scalerGoals)
	prometheus.MustRegister(contextRuns)
	prometheus.MustRegister(repoPulls)
	prometheus.MustRegister(repoPllErrs)
	prometheus.MustRegister(repoScrapeErrs)
	prometheus.MustRegister(pkgScans)
	prometheus.MustRegister(limitPullErrs)
	prometheus.MustRegister(statWrites)
	prometheus.MustRegister(scaleOverruns)
	prometheus.MustRegister(statWriteFails)
	prometheus.MustRegister(scalers)
	prometheus.MustRegister(portWaiters)
	prometheus.MustRegister(srcGCs)
	prometheus.MustRegister(danglingEvents)

	r := mux.NewRouter()
	r.Handle("/metrics", promhttp.Handler())

	psrv := &http.Server{Handler: r, Addr: conf.Daemon.Prometheus}
	go psrv.ListenAndServe()

	ctxlog(ctx).Debugf("Prometeus exporter started at %s", conf.Daemon.Prometheus)

	return nil
}
