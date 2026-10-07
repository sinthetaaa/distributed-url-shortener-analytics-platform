package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	analyticsDatabaseStartupTimeout = 5 * time.Second
	analyticsMetricsShutdownTimeout = 2 * time.Second
	defaultAnalyticsMetricsAddr     = ":9091"
	tracingShutdownTimeout          = 5 * time.Second
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	)

	tracing, tracingErr := observability.NewTracing(
		context.Background(),
		"shortscale-analytics-consumer",
	)
	if tracingErr != nil {
		logger.Warn(
			"failed to initialize OpenTelemetry tracing; continuing without tracing",
			"error",
			tracingErr,
		)
	} else if tracing.Enabled() {
		logger.Info(
			"OpenTelemetry tracing enabled",
			"service",
			"shortscale-analytics-consumer",
		)
	}

	metrics, err := observability.NewConsumerMetrics()
	if err != nil {
		logger.Error(
			"failed to initialize analytics consumer metrics",
			"error", err,
		)
		return 1
	}

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

	metricsAddr := strings.TrimSpace(
		os.Getenv("ANALYTICS_METRICS_ADDR"),
	)
	if metricsAddr == "" {
		metricsAddr = defaultAnalyticsMetricsAddr
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

	processor, err := analytics.NewPostgresRedirectEventProcessorWithMetrics(
		queries,
		metrics,
	)
	if err != nil {
		logger.Error(
			"failed to create redirect-event processor",
			"error", err,
		)
		return 1
	}

	consumer, err := analytics.NewKafkaRedirectEventConsumerWithMetrics(
		config,
		metrics,
	)
	if err != nil {
		logger.Error(
			"failed to create analytics consumer",
			"error", err,
		)
		return 1
	}
	defer consumer.Close()

	lagReader, err := analytics.NewKafkaConsumerLagReader(config)
	if err != nil {
		logger.Error(
			"failed to create analytics consumer lag reader",
			"error", err,
		)
		return 1
	}
	defer lagReader.Close()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	metricsServer := &http.Server{
		Addr:              metricsAddr,
		Handler:           metrics.Handler(),
		ReadHeaderTimeout: 2 * time.Second,
	}

	metricsErrors := make(chan error, 1)

	go func() {
		logger.Info(
			"starting analytics consumer metrics server",
			"addr", metricsAddr,
		)

		if err := metricsServer.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			metricsErrors <- err
		}
	}()

	go monitorAnalyticsConsumerLag(
		ctx,
		logger,
		lagReader,
		metrics,
	)

	logger.Info(
		"starting ShortScale analytics consumer",
		"brokers", config.Brokers,
		"topic", config.Topic,
		"group_id", config.GroupID,
		"client_id", config.ClientID,
		"reset_offset", config.ResetOffset,
	)

	consumerErrors := make(chan error, 1)

	go func() {
		consumerErrors <- consumer.Run(ctx, processor)
	}()

	exitCode := 0

	select {
	case err := <-consumerErrors:
		if err != nil {
			logger.Error(
				"analytics consumer stopped with error",
				"error", err,
			)
			exitCode = 1
		} else {
			logger.Info("ShortScale analytics consumer stopped")
		}

	case err := <-metricsErrors:
		logger.Error(
			"analytics consumer metrics server stopped with error",
			"error", err,
		)
		exitCode = 1
		stop()

		if consumerErr := <-consumerErrors; consumerErr != nil {
			logger.Error(
				"analytics consumer stopped with error",
				"error", consumerErr,
			)
		}

	case <-ctx.Done():
		if err := <-consumerErrors; err != nil {
			logger.Error(
				"analytics consumer stopped with error",
				"error", err,
			)
			exitCode = 1
		} else {
			logger.Info("ShortScale analytics consumer stopped")
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		analyticsMetricsShutdownTimeout,
	)
	defer shutdownCancel()

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error(
			"failed to shut down analytics consumer metrics server",
			"error", err,
		)
		exitCode = 1
	}

	tracingShutdownCtx, tracingShutdownCancel := context.WithTimeout(
		context.Background(),
		tracingShutdownTimeout,
	)
	defer tracingShutdownCancel()

	if err := tracing.Shutdown(tracingShutdownCtx); err != nil {
		logger.Warn(
			"failed to flush OpenTelemetry traces",
			"error",
			err,
		)
	}

	return exitCode
}
