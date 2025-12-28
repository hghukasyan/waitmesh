package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	EventsIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "waitmesh_events_ingested_total",
		Help: "Events accepted by ingest path",
	}, []string{"result"})

	IngestLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "waitmesh_ingest_latency_seconds",
		Help:    "Ingest latency",
		Buckets: prometheus.DefBuckets,
	})

	TimersProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "waitmesh_timers_processed_total",
		Help: "Overdue timer rows processed",
	})

	FiringsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "waitmesh_firings_total",
		Help: "Logical firings",
	}, []string{"rule_type"})

	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "waitmesh_write_queue_depth",
		Help: "Approximate write queue depth",
	})
)

func Handler() http.Handler {
	return promhttp.Handler()
}
