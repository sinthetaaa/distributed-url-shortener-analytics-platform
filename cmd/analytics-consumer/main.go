package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"
)

type loggingRedirectEventProcessor struct {
	logger *slog.Logger
}

func (p loggingRedirectEventProcessor) Process(
	_ context.Context,
	event analytics.RedirectEvent,
) error {
	p.logger.Info(
		"redirect analytics event consumed",
		"event_id", event.EventID,
		"event_type", event.EventType,
		"short_code", event.ShortCode,
		"occurred_at", event.OccurredAt,
	)

	return nil
}

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	)

	config, err := analytics.KafkaConsumerConfigFromEnv()
	if err != nil {
		logger.Error(
			"failed to load analytics consumer configuration",
			"error", err,
		)
		return 1
	}

	consumer, err := analytics.NewKafkaRedirectEventConsumer(config)
	if err != nil {
		logger.Error(
			"failed to create analytics consumer",
			"error", err,
		)
		return 1
	}
	defer consumer.Close()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	logger.Info(
		"starting ShortScale analytics consumer",
		"brokers", config.Brokers,
		"topic", config.Topic,
		"group_id", config.GroupID,
		"client_id", config.ClientID,
		"reset_offset", config.ResetOffset,
	)

	processor := loggingRedirectEventProcessor{
		logger: logger,
	}

	if err := consumer.Run(ctx, processor); err != nil {
		logger.Error(
			"analytics consumer stopped with error",
			"error", err,
		)
		return 1
	}

	logger.Info("ShortScale analytics consumer stopped")

	return 0
}
