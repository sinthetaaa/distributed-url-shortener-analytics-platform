package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHTTPMiddlewareUsesStableRoutePatterns(t *testing.T) {
	t.Parallel()

	metrics, err := NewMetrics()
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	router := chi.NewRouter()
	router.Use(metrics.HTTPMiddleware)

	router.Get(
		"/things/{id}",
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	)

	for _, path := range []string{
		"/things/alpha",
		"/things/beta",
	} {
		request := httptest.NewRequest(
			http.MethodGet,
			path,
			nil,
		)
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		if response.Code != http.StatusNoContent {
			t.Fatalf(
				"expected status %d for %s, got %d",
				http.StatusNoContent,
				path,
				response.Code,
			)
		}
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/does-not-exist",
		nil,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf(
			"expected unmatched status %d, got %d",
			http.StatusNotFound,
			response.Code,
		)
	}

	routeCount := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.
			WithLabelValues(
				http.MethodGet,
				"/things/{id}",
				"204",
			),
	)

	if routeCount != 2 {
		t.Fatalf(
			"expected route counter 2, got %v",
			routeCount,
		)
	}

	unmatchedCount := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.
			WithLabelValues(
				http.MethodGet,
				unmatchedRoute,
				"404",
			),
	)

	if unmatchedCount != 1 {
		t.Fatalf(
			"expected unmatched counter 1, got %v",
			unmatchedCount,
		)
	}

	inFlight := testutil.ToFloat64(
		metrics.HTTPRequestsInFlight,
	)

	if inFlight != 0 {
		t.Fatalf(
			"expected in-flight gauge 0 after requests, got %v",
			inFlight,
		)
	}

	durationSeries := testutil.CollectAndCount(
		metrics.HTTPRequestDuration,
	)

	if durationSeries != 2 {
		t.Fatalf(
			"expected 2 duration series, got %d",
			durationSeries,
		)
	}
}

func TestMetricsHandlerExposesRegistry(t *testing.T) {
	t.Parallel()

	metrics, err := NewMetrics()
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	metrics.HTTPRequestsTotal.
		WithLabelValues(
			http.MethodGet,
			"/health/live",
			"200",
		).
		Inc()

	metrics.HTTPRequestDuration.
		WithLabelValues(
			http.MethodGet,
			"/health/live",
		).
		Observe(0.01)

	request := httptest.NewRequest(
		http.MethodGet,
		"/metrics",
		nil,
	)
	response := httptest.NewRecorder()

	metrics.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}

	body := response.Body.String()

	for _, metricName := range []string{
		"shortscale_http_requests_total",
		"shortscale_http_request_duration_seconds",
		"shortscale_http_requests_in_flight",
		"go_goroutines",
		"process_cpu_seconds_total",
	} {
		if !strings.Contains(body, metricName) {
			t.Fatalf(
				"expected metrics output to contain %q",
				metricName,
			)
		}
	}
}
