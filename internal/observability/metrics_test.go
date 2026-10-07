package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewMetricsRegistersHTTPMetrics(t *testing.T) {
	t.Parallel()

	metrics, err := NewMetrics()
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	metrics.HTTPRequestsTotal.
		WithLabelValues(
			"GET",
			"/{shortCode}",
			"302",
		).
		Inc()

	metrics.HTTPRequestDuration.
		WithLabelValues(
			"GET",
			"/{shortCode}",
		).
		Observe(0.015)

	metrics.HTTPRequestsInFlight.Set(2)

	requestCount := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.
			WithLabelValues(
				"GET",
				"/{shortCode}",
				"302",
			),
	)

	if requestCount != 1 {
		t.Fatalf(
			"expected request counter 1, got %v",
			requestCount,
		)
	}

	inFlight := testutil.ToFloat64(
		metrics.HTTPRequestsInFlight,
	)

	if inFlight != 2 {
		t.Fatalf(
			"expected in-flight gauge 2, got %v",
			inFlight,
		)
	}

	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	expectedFamilies := map[string]bool{
		"shortscale_http_requests_total":           false,
		"shortscale_http_request_duration_seconds": false,
		"shortscale_http_requests_in_flight":       false,
	}

	for _, family := range families {
		if family.Name == nil {
			continue
		}

		if _, exists := expectedFamilies[*family.Name]; exists {
			expectedFamilies[*family.Name] = true
		}
	}

	for name, found := range expectedFamilies {
		if !found {
			t.Fatalf(
				"expected metric family %q to be registered",
				name,
			)
		}
	}
}

func TestNewMetricsUsesIndependentRegistries(t *testing.T) {
	t.Parallel()

	first, err := NewMetrics()
	if err != nil {
		t.Fatalf("create first metrics registry: %v", err)
	}

	second, err := NewMetrics()
	if err != nil {
		t.Fatalf("create second metrics registry: %v", err)
	}

	first.HTTPRequestsTotal.
		WithLabelValues(
			"GET",
			"/health/live",
			"200",
		).
		Inc()

	firstValue := testutil.ToFloat64(
		first.HTTPRequestsTotal.
			WithLabelValues(
				"GET",
				"/health/live",
				"200",
			),
	)

	secondValue := testutil.ToFloat64(
		second.HTTPRequestsTotal.
			WithLabelValues(
				"GET",
				"/health/live",
				"200",
			),
	)

	if firstValue != 1 {
		t.Fatalf(
			"expected first registry counter 1, got %v",
			firstValue,
		)
	}

	if secondValue != 0 {
		t.Fatalf(
			"expected second registry counter 0, got %v",
			secondValue,
		)
	}
}
