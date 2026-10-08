package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	authpkg "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/auth"
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
	tracingShutdownTimeout           = 5 * time.Second
)

type config struct {
	Port         string
	DatabaseURL  string
	RedisAddr    string
	CookieSecure bool
}

type databasePinger interface {
	Ping(context.Context) error
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	tracing, tracingErr := observability.NewTracing(
		context.Background(),
		"shortscale-api",
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
			"shortscale-api",
		)
	}

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
	defer func() {
		if err := redisClient.Close(); err != nil {
			logger.Warn(
				"failed to close redis client",
				"error",
				err,
			)
		}
	}()

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

	redirectRecorder, closeRedirectAnalytics := newProductionRedirectEventRecorderWithMetrics(
		logger,
		metrics,
	)

	addr := ":" + cfg.Port

	queries := database.New(pool)
	authService := authpkg.NewService(queries)

	cache := newObservedRedirectCache(
		urlcache.NewURLCache(redisClient, urlCacheTTL),
		metrics,
	)
	finder := newCachedURLFinder(logger, cache, queries)

	createURLLimiter := newRedisTokenBucketLimiterWithMetrics(
		logger,
		redisClient,
		createURLRateLimitCapacity,
		createURLRateLimitRefillPerSecond,
		metrics,
	)

	router := newRouterWithDependenciesMetricsAndAuth(
		logger,
		pool,
		queries,
		finder,
		createURLLimiter,
		redirectRecorder,
		queries,
		metrics,
		authService,
		cfg.CookieSecure,
	)

	tracedHandler := newTracedHTTPHandler(router)

	server := &http.Server{
		Addr:    addr,
		Handler: tracedHandler,
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

	cookieSecure := false

	if raw := os.Getenv("AUTH_COOKIE_SECURE"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return config{}, fmt.Errorf(
				"AUTH_COOKIE_SECURE must be true or false: %w",
				err,
			)
		}

		cookieSecure = parsed
	}

	return config{
		Port:         port,
		DatabaseURL:  databaseURL,
		RedisAddr:    redisAddr,
		CookieSecure: cookieSecure,
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
	return newRouterWithDependenciesMetricsAndAuth(
		logger,
		database,
		creator,
		finder,
		createURLLimiter,
		redirectRecorder,
		analyticsReader,
		metrics,
		nil,
		false,
	)
}

func newRouterWithDependenciesMetricsAndAuth(
	logger *slog.Logger,
	database databasePinger,
	creator urlCreator,
	finder urlFinder,
	createURLLimiter requestRateLimiter,
	redirectRecorder redirectEventRecorder,
	analyticsReader redirectAnalyticsReader,
	metrics *observability.Metrics,
	authService authenticationService,
	cookieSecure bool,
) http.Handler {
	router := chi.NewRouter()

	if metrics != nil {
		router.Use(metrics.HTTPMiddleware)
		router.Handle("/metrics", metrics.Handler())
	}

	router.Get("/health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSONStatus(w, logger, http.StatusOK, "ok")
	})

	if authService != nil {
		router.Post(
			"/api/v1/auth/register",
			registerHandler(logger, authService),
		)

		router.Post(
			"/api/v1/auth/login",
			loginHandler(
				logger,
				authService,
				cookieSecure,
			),
		)

		router.Post(
			"/api/v1/auth/logout",
			logoutHandler(
				logger,
				authService,
				cookieSecure,
			),
		)

		router.With(
			authenticationMiddleware(
				logger,
				authService,
				cookieSecure,
			),
		).Get(
			"/api/v1/auth/me",
			meHandler(logger),
		)
	}

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
