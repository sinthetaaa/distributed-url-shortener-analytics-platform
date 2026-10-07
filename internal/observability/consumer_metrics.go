package observability

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ConsumerMetrics struct {
	Registry *prometheus.Registry

	ProcessedTotal   prometheus.Counter
	FailuresTotal    *prometheus.CounterVec
	PersistenceTotal *prometheus.CounterVec
}

func NewConsumerMetrics() (*ConsumerMetrics, error) {
	registry := prometheus.NewRegistry()

	metrics := &ConsumerMetrics{
		Registry: registry,

		ProcessedTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "analytics_consumer",
				Name:      "processed_total",
				Help:      "Total number of redirect analytics events processed and committed successfully.",
			},
		),

		FailuresTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "analytics_consumer",
				Name:      "failures_total",
				Help:      "Total number of analytics-consumer failures by processing stage.",
			},
			[]string{
				"stage",
			},
		),

		PersistenceTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "analytics_consumer",
				Name:      "persistence_total",
				Help:      "Total number of PostgreSQL redirect-event persistence attempts by result.",
			},
			[]string{
				"result",
			},
		),
	}

	collectorsToRegister := []prometheus.Collector{
		metrics.ProcessedTotal,
		metrics.FailuresTotal,
		metrics.PersistenceTotal,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(
			collectors.ProcessCollectorOpts{},
		),
	}

	for _, collector := range collectorsToRegister {
		if err := registry.Register(collector); err != nil {
			return nil, fmt.Errorf(
				"register analytics-consumer Prometheus collector: %w",
				err,
			)
		}
	}

	return metrics, nil
}

func (m *ConsumerMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(
		m.Registry,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	)
}
