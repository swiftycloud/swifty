/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"github.com/gorilla/mux"
	"net/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	KiB	= float64(1024)
	MiB	= float64(1024 * KiB)
	GiB	= float64(1024 * MiB)
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

	ioSize = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "swys3_api_latencies",
			Help: "Sizes of objects",
			Buckets: []float64{
				float64(KiB),
				float64(512 * KiB),
				float64(MiB),
				float64(2 * MiB),
				float64(4 * MiB),
				float64(8 * MiB),
				float64(16 * MiB),
			},
		},
	)

	fsckReqs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "swys3_fsck_reqs",
			Help: "Number of fsck claims",
		},
	)
)

func PrometheusInit(conf *YAMLConf) error {
	prometheus.MustRegister(apiCalls)
	prometheus.MustRegister(ioSize)
	prometheus.MustRegister(fsckReqs)

	r := mux.NewRouter()
	r.Handle("/metrics", promhttp.Handler())

	psrv := &http.Server{Handler: r, Addr: conf.Daemon.Prometheus}
	go psrv.ListenAndServe()

	log.Debugf("Prometeus exporter started at %s", conf.Daemon.Prometheus)

	return nil
}
