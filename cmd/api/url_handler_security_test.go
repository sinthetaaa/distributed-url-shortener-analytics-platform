package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeCreateURLRequestSecurityBoundaries(t *testing.T) {
	t.Parallel()

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	tests := []struct {
		name       string
		body       string
		wantOK     bool
		wantStatus int
	}{
		{
			name:       "valid request",
			body:       `{"url":"https://example.com"}`,
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
		{
			name: "oversized request body",
			body: `{"url":"https://example.com/` +
				strings.Repeat("a", maxURLRequestBodyBytes) +
				`"}`,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "trailing JSON payload",
			body: `{"url":"https://example.com"}` +
				`{"url":"https://example.org"}`,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/urls",
				strings.NewReader(test.body),
			)

			recorder := httptest.NewRecorder()

			decoded, ok := decodeCreateURLRequest(
				recorder,
				request,
				logger,
			)

			if ok != test.wantOK {
				t.Fatalf(
					"decodeCreateURLRequest() ok = %v, want %v",
					ok,
					test.wantOK,
				)
			}

			if test.wantOK {
				if decoded.URL != "https://example.com" {
					t.Fatalf(
						"decoded URL = %q, want %q",
						decoded.URL,
						"https://example.com",
					)
				}

				return
			}

			if recorder.Code != test.wantStatus {
				t.Fatalf(
					"status = %d, want %d",
					recorder.Code,
					test.wantStatus,
				)
			}
		})
	}
}
