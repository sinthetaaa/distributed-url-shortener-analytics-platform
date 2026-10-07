package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"
)

const (
	analyticsLagSampleInterval = 5 * time.Second
	analyticsLagSampleTimeout  = 2 * time.Second
)

type analyticsConsumerLagReader interface {
	Lag(context.Context) (int64, error)
}

func monitorAnalyticsConsumerLag(
	ctx context.Context,
	logger *slog.Logger,
	reader analyticsConsumerLagReader,
	metrics *observability.ConsumerMetrics,
) {
	sampleAnalyticsConsumerLag(
		ctx,
		logger,
		reader,
		metrics,
	)

	ticker := time.NewTicker(analyticsLagSampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			sampleAnalyticsConsumerLag(
				ctx,
				logger,
				reader,
				metrics,
			)
		}
	}
}

func sampleAnalyticsConsumerLag(
	ctx context.Context,
	logger *slog.Logger,
	reader analyticsConsumerLagReader,
	metrics *observability.ConsumerMetrics,
) {
	sampleCtx, cancel := context.WithTimeout(
		ctx,
		analyticsLagSampleTimeout,
	)
	defer cancel()

	lag, err := reader.Lag(sampleCtx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}

		metrics.LagCollectionFailuresTotal.Inc()

		logger.Warn(
			"failed to collect analytics consumer lag",
			"error", err,
		)

		return
	}

	metrics.LagRecords.Set(float64(lag))
}
