package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5/pgxpool"
)

const analyticsDatabaseStartupTimeout = 5 * time.Second

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

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required")
		return 1
	}

	databaseCtx, databaseCancel := context.WithTimeout(
		context.Background(),
		analyticsDatabaseStartupTimeout,
	)
	defer databaseCancel()

	pool, err := pgxpool.New(databaseCtx, databaseURL)
	if err != nil {
		logger.Error(
			"failed to create analytics database connection pool",
			"error", err,
		)
		return 1
	}
	defer pool.Close()

	if err := pool.Ping(databaseCtx); err != nil {
		logger.Error(
			"failed to connect analytics consumer to database",
			"error", err,
		)
		return 1
	}

	logger.Info("analytics database connection established")

	queries := database.New(pool)

	processor, err := analytics.NewPostgresRedirectEventProcessor(
		queries,
	)
	if err != nil {
		logger.Error(
			"failed to create redirect-event processor",
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
