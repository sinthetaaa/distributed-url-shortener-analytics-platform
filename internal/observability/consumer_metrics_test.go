package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewConsumerMetricsRegistersExpectedMetrics(t *testing.T) {
	t.Parallel()

	metrics, err := NewConsumerMetrics()
	if err != nil {
		t.Fatalf("create consumer metrics: %v", err)
	}

	metrics.ProcessedTotal.Inc()
	metrics.FailuresTotal.WithLabelValues("decode").Inc()
	metrics.PersistenceTotal.WithLabelValues("inserted").Inc()

	if got := testutil.ToFloat64(metrics.ProcessedTotal); got != 1 {
		t.Fatalf("expected processed counter 1, got %v", got)
	}

	if got := testutil.ToFloat64(
		metrics.FailuresTotal.WithLabelValues("decode"),
	); got != 1 {
		t.Fatalf("expected decode failure counter 1, got %v", got)
	}

	if got := testutil.ToFloat64(
		metrics.PersistenceTotal.WithLabelValues("inserted"),
	); got != 1 {
		t.Fatalf("expected inserted persistence counter 1, got %v", got)
	}
}

func TestConsumerMetricsHandlerExposesRegistry(t *testing.T) {
	t.Parallel()

	metrics, err := NewConsumerMetrics()
	if err != nil {
		t.Fatalf("create consumer metrics: %v", err)
	}

	metrics.ProcessedTotal.Inc()
	metrics.FailuresTotal.WithLabelValues("commit").Inc()
	metrics.PersistenceTotal.WithLabelValues("duplicate").Inc()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	metrics.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", rec.Code)
	}

	body := rec.Body.String()

	for _, name := range []string{
		"shortscale_analytics_consumer_processed_total",
		"shortscale_analytics_consumer_failures_total",
		"shortscale_analytics_consumer_persistence_total",
		"go_goroutines",
		"process_cpu_seconds_total",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("expected metrics response to contain %q", name)
		}
	}
}
