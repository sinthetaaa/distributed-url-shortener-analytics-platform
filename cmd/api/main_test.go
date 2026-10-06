package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestRouter() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newRouter(logger)
}

func TestHealthLive(t *testing.T) {
	router := newTestRouter()

	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	expectedBody := `{"status":"ok"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestUnknownRoute(t *testing.T) {
	router := newTestRouter()

	request := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestHealthLiveMethodNotAllowed(t *testing.T) {
	router := newTestRouter()

	request := httptest.NewRequest(http.MethodPost, "/health/live", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, recorder.Code)
	}
}
