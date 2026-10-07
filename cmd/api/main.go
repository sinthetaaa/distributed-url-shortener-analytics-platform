package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	urlCacheTTL                      = time.Hour
	redirectAnalyticsShutdownTimeout = 2 * time.Second
)

type config struct {
	Port        string
	DatabaseURL string
	RedisAddr   string
}

type databasePinger interface {
	Ping(context.Context) error
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	metrics, err := observability.NewMetrics()
	if err != nil {
		logger.Error(
			"failed to initialize Prometheus metrics",
			"error",
			err,
		)
		os.Exit(1)
	}

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	databaseCtx, databaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer databaseCancel()

	pool, err := pgxpool.New(databaseCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to create database connection pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(databaseCtx); err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}

	logger.Info("database connection established")

	redisClient := redis.NewClient(&redis.Options{
		Addr:                  cfg.RedisAddr,
		MaxRetries:            -1,
		DialerRetries:         1,
		DialerRetryTimeout:    10 * time.Millisecond,
		DialTimeout:           50 * time.Millisecond,
		ReadTimeout:           50 * time.Millisecond,
		WriteTimeout:          50 * time.Millisecond,
		ContextTimeoutEnabled: true,
	})
	defer redisClient.Close()

	redisCtx, redisCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer redisCancel()

	if err := redisClient.Ping(redisCtx).Err(); err != nil {
		logger.Warn(
			"redis unavailable at startup; continuing without cache",
			"error", err,
		)
	} else {
		logger.Info("redis connection established")
	}

	redirectRecorder, closeRedirectAnalytics := newProductionRedirectEventRecorder(logger)

	addr := ":" + cfg.Port

	queries := database.New(pool)
	cache := urlcache.NewURLCache(redisClient, urlCacheTTL)
	finder := newCachedURLFinder(logger, cache, queries)

	createURLLimiter := newRedisTokenBucketLimiter(
		logger,
		redisClient,
		createURLRateLimitCapacity,
		createURLRateLimitRefillPerSecond,
	)

	router := newRouterWithDependenciesAndMetrics(
		logger,
		pool,
		queries,
		finder,
		createURLLimiter,
		redirectRecorder,
		queries,
		metrics,
	)

	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	serverErrors := make(chan error, 1)

	go func() {
		logger.Info("starting ShortScale API", "addr", addr)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-shutdownSignal:
		logger.Info("shutdown signal received", "signal", sig.String())

	case err := <-serverErrors:
		logger.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	logger.Info("shutting down ShortScale API")

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	analyticsShutdownCtx, analyticsShutdownCancel := context.WithTimeout(
		context.Background(),
		redirectAnalyticsShutdownTimeout,
	)
	defer analyticsShutdownCancel()

	closeRedirectAnalytics(analyticsShutdownCtx)

	logger.Info("ShortScale API stopped")
}

func loadConfig() (config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		return config{}, fmt.Errorf("REDIS_ADDR is required")
	}

	return config{
		Port:        port,
		DatabaseURL: databaseURL,
		RedisAddr:   redisAddr,
	}, nil
}

func newRouter(
	logger *slog.Logger,
	database databasePinger,
	creator urlCreator,
	finder urlFinder,
) http.Handler {
	return newRouterWithRateLimiter(
		logger,
		database,
		creator,
		finder,
		newLocalTokenBucketLimiter(
			createURLRateLimitCapacity,
			createURLRateLimitRefillPerSecond,
		),
	)
}

func newRouterWithRateLimiter(
	logger *slog.Logger,
	database databasePinger,
	creator urlCreator,
	finder urlFinder,
	createURLLimiter requestRateLimiter,
) http.Handler {
	return newRouterWithRateLimiterAndRedirectEvents(
		logger,
		database,
		creator,
		finder,
		createURLLimiter,
		noopRedirectEventRecorder{},
	)
}

func newRouterWithRateLimiterAndRedirectEvents(
	logger *slog.Logger,
	database databasePinger,
	creator urlCreator,
	finder urlFinder,
	createURLLimiter requestRateLimiter,
	redirectRecorder redirectEventRecorder,
) http.Handler {
	return newRouterWithDependencies(
		logger,
		database,
		creator,
		finder,
		createURLLimiter,
		redirectRecorder,
		nil,
	)
}

func newRouterWithDependencies(
	logger *slog.Logger,
	database databasePinger,
	creator urlCreator,
	finder urlFinder,
	createURLLimiter requestRateLimiter,
	redirectRecorder redirectEventRecorder,
	analyticsReader redirectAnalyticsReader,
) http.Handler {
	return newRouterWithDependenciesAndMetrics(
		logger,
		database,
		creator,
		finder,
		createURLLimiter,
		redirectRecorder,
		analyticsReader,
		nil,
	)
}

func newRouterWithDependenciesAndMetrics(
	logger *slog.Logger,
	database databasePinger,
	creator urlCreator,
	finder urlFinder,
	createURLLimiter requestRateLimiter,
	redirectRecorder redirectEventRecorder,
	analyticsReader redirectAnalyticsReader,
	metrics *observability.Metrics,
) http.Handler {
	router := chi.NewRouter()

	if metrics != nil {
		router.Use(metrics.HTTPMiddleware)
		router.Handle("/metrics", metrics.Handler())
	}

	router.Get("/health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSONStatus(w, logger, http.StatusOK, "ok")
	})

	router.With(
		rateLimitMiddleware(logger, createURLLimiter),
	).Post(
		"/api/v1/urls",
		createURLHandler(logger, creator),
	)

	if analyticsReader != nil {
		router.Get(
			"/api/v1/urls/{shortCode}/analytics",
			redirectAnalyticsHandler(logger, analyticsReader),
		)
	}

	router.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := database.Ping(ctx); err != nil {
			logger.Error("database readiness check failed", "error", err)
			writeJSONStatus(w, logger, http.StatusServiceUnavailable, "unavailable")
			return
		}

		writeJSONStatus(w, logger, http.StatusOK, "ok")
	})

	router.Get(
		"/{shortCode}",
		redirectURLHandlerWithEvents(logger, finder, redirectRecorder),
	)

	return router
}

func writeJSONStatus(w http.ResponseWriter, logger *slog.Logger, statusCode int, status string) {
	writeJSON(w, logger, statusCode, map[string]string{
		"status": status,
	})
}
