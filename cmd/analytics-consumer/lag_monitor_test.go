package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

type testAnalyticsConsumerLagReader struct {
	lag int64
	err error
}

func (r testAnalyticsConsumerLagReader) Lag(
	context.Context,
) (int64, error) {
	return r.lag, r.err
}

func TestSampleAnalyticsConsumerLagRecordsLag(t *testing.T) {
	metrics, err := observability.NewConsumerMetrics()
	if err != nil {
		t.Fatalf("create consumer metrics: %v", err)
	}

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	sampleAnalyticsConsumerLag(
		context.Background(),
		logger,
		testAnalyticsConsumerLagReader{
			lag: 42,
		},
		metrics,
	)

	if got := testutil.ToFloat64(metrics.LagRecords); got != 42 {
		t.Fatalf(
			"expected lag gauge 42, got %v",
			got,
		)
	}

	if got := testutil.ToFloat64(
		metrics.LagCollectionFailuresTotal,
	); got != 0 {
		t.Fatalf(
			"expected lag collection failures 0, got %v",
			got,
		)
	}
}

func TestSampleAnalyticsConsumerLagRecordsFailure(t *testing.T) {
	metrics, err := observability.NewConsumerMetrics()
	if err != nil {
		t.Fatalf("create consumer metrics: %v", err)
	}

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	sampleAnalyticsConsumerLag(
		context.Background(),
		logger,
		testAnalyticsConsumerLagReader{
			err: errors.New("Kafka unavailable"),
		},
		metrics,
	)

	if got := testutil.ToFloat64(
		metrics.LagCollectionFailuresTotal,
	); got != 1 {
		t.Fatalf(
			"expected lag collection failures 1, got %v",
			got,
		)
	}
}
