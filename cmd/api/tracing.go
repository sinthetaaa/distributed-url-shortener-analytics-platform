package main

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func newTracedHTTPHandler(handler http.Handler) http.Handler {
	return otelhttp.NewHandler(
		handler,
		"shortscale.http",
		otelhttp.WithFilter(shouldTraceHTTPRequest),
		otelhttp.WithSpanNameFormatter(httpServerSpanName),
	)
}

func shouldTraceHTTPRequest(r *http.Request) bool {
	switch r.URL.Path {
	case "/metrics", "/health/live", "/health/ready":
		return false
	default:
		return true
	}
}

func httpServerSpanName(
	_ string,
	r *http.Request,
) string {
	return "HTTP " + r.Method
}
