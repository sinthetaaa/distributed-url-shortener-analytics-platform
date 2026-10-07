package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const unmatchedRoute = "unmatched"

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(
		m.Registry,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	)
}

func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()

		m.HTTPRequestsInFlight.Inc()

		responseWriter := chimiddleware.NewWrapResponseWriter(
			w,
			r.ProtoMajor,
		)

		defer func() {
			m.HTTPRequestsInFlight.Dec()

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = unmatchedRoute
			}

			statusCode := responseWriter.Status()
			if statusCode == 0 {
				statusCode = http.StatusOK
			}

			m.HTTPRequestsTotal.
				WithLabelValues(
					r.Method,
					route,
					strconv.Itoa(statusCode),
				).
				Inc()

			m.HTTPRequestDuration.
				WithLabelValues(
					r.Method,
					route,
				).
				Observe(time.Since(startedAt).Seconds())
		}()

		next.ServeHTTP(responseWriter, r)
	})
}
