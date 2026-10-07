package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShouldTraceHTTPRequest(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "metrics excluded",
			path: "/metrics",
			want: false,
		},
		{
			name: "liveness excluded",
			path: "/health/live",
			want: false,
		},
		{
			name: "readiness excluded",
			path: "/health/ready",
			want: false,
		},
		{
			name: "create URL traced",
			path: "/api/v1/urls",
			want: true,
		},
		{
			name: "redirect traced",
			path: "/abc123",
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				test.path,
				nil,
			)

			if got := shouldTraceHTTPRequest(request); got != test.want {
				t.Fatalf(
					"expected shouldTraceHTTPRequest(%q)=%v, got %v",
					test.path,
					test.want,
					got,
				)
			}
		})
	}
}

func TestHTTPServerSpanNameUsesBoundedMethodName(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		nil,
	)

	if got := httpServerSpanName("", request); got != "HTTP POST" {
		t.Fatalf(
			"expected bounded span name %q, got %q",
			"HTTP POST",
			got,
		)
	}
}
