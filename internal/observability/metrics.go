package observability

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const namespace = "shortscale"

type Metrics struct {
	Registry *prometheus.Registry

	HTTPRequestsTotal    *prometheus.CounterVec
	HTTPRequestDuration  *prometheus.HistogramVec
	HTTPRequestsInFlight prometheus.Gauge

	CacheOperationsTotal    *prometheus.CounterVec
	RateLimitDecisionsTotal *prometheus.CounterVec
}

func NewMetrics() (*Metrics, error) {
	registry := prometheus.NewRegistry()

	metrics := &Metrics{
		Registry: registry,

		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total number of HTTP requests processed by the ShortScale API.",
			},
			[]string{
				"method",
				"route",
				"status",
			},
		),

		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request duration in seconds.",
				Buckets: []float64{
					0.001,
					0.0025,
					0.005,
					0.01,
					0.025,
					0.05,
					0.1,
					0.25,
					0.5,
					1,
					2.5,
					5,
				},
			},
			[]string{
				"method",
				"route",
			},
		),

		HTTPRequestsInFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "requests_in_flight",
				Help:      "Current number of HTTP requests being processed.",
			},
		),

		CacheOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "cache",
				Name:      "operations_total",
				Help:      "Total number of URL-cache operations by operation and result.",
			},
			[]string{
				"operation",
				"result",
			},
		),

		RateLimitDecisionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "rate_limit",
				Name:      "decisions_total",
				Help:      "Total number of distributed rate-limit decisions by result.",
			},
			[]string{
				"result",
			},
		),
	}

	metricCollectors := []prometheus.Collector{
		metrics.HTTPRequestsTotal,
		metrics.HTTPRequestDuration,
		metrics.HTTPRequestsInFlight,
		metrics.CacheOperationsTotal,
		metrics.RateLimitDecisionsTotal,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(
			collectors.ProcessCollectorOpts{},
		),
	}

	for _, collector := range metricCollectors {
		if err := registry.Register(collector); err != nil {
			return nil, fmt.Errorf(
				"register Prometheus collector: %w",
				err,
			)
		}
	}

	return metrics, nil
}
