package main

import (
	"log/slog"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"
)

// redirectEventRecorder is the request-path handoff for redirect analytics.
// Implementations must return quickly and must not make redirect availability
// depend on analytics delivery.
type redirectEventRecorder interface {
	Record(analytics.RedirectEvent)
}

type noopRedirectEventRecorder struct{}

func (noopRedirectEventRecorder) Record(analytics.RedirectEvent) {}

func recordRedirectEvent(
	logger *slog.Logger,
	recorder redirectEventRecorder,
	shortCode string,
) {
	event, err := analytics.NewRedirectEvent(shortCode)
	if err != nil {
		logger.Error(
			"failed to create redirect analytics event",
			"short_code", shortCode,
			"error", err,
		)
		return
	}

	recorder.Record(event)
}
