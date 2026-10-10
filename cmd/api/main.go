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
	apiMetricsShutdownTimeout        = 2 * time.Second
	redirectAnalyticsShutdownTimeout = 2 * time.Second
	tracingShutdownTimeout           = 5 * time.Second
	defaultAPIMetricsAddr            = ":9090"
)

type config struct {
	Port         string
	DatabaseURL  string
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

	redisOptions, err := redisOptionsFromEnv()
	if err != nil {
		logger.Error(
			"failed to load Redis configuration",
			"error",
			err,
		)
		os.Exit(1)
	}

	redisClient := redis.NewClient(redisOptions)
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

	metricsAddr := os.Getenv("API_METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = defaultAPIMetricsAddr
	}

	metricsServer := &http.Server{
		Addr:              metricsAddr,
		Handler:           metrics.Handler(),
		ReadHeaderTimeout: 2 * time.Second,
	}

	go func() {
		logger.Info(
			"starting ShortScale API metrics server",
			"addr",
			metricsAddr,
		)

		if err := metricsServer.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			logger.Warn(
				"API metrics server stopped with error; continuing without metrics endpoint",
				"error",
				err,
			)
		}
	}()

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

	metricsShutdownCtx, metricsShutdownCancel := context.WithTimeout(
		context.Background(),
		apiMetricsShutdownTimeout,
	)
	defer metricsShutdownCancel()

	if err := metricsServer.Shutdown(metricsShutdownCtx); err != nil {
		logger.Warn(
			"failed to shut down API metrics server",
			"error",
			err,
		)
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
	}

	router.Get("/health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSONStatus(w, logger, http.StatusOK, "ok")
	})

	if authService != nil {
		authLimiter := newLocalTokenBucketLimiter(
			authRateLimitCapacity,
			authRateLimitRefillPerSecond,
		)

		router.With(
			rateLimitMiddleware(
				logger,
				authLimiter,
			),
		).Post(
			"/api/v1/auth/register",
			registerHandler(logger, authService),
		)

		router.With(
			rateLimitMiddleware(
				logger,
				authLimiter,
			),
		).Post(
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

	if authService != nil {
		ownedCreator, ok := creator.(ownedURLCreator)
		if !ok {
			panic("authenticated router requires an owned URL creator")
		}

		userLister, ok := creator.(userURLLister)
		if !ok {
			panic("authenticated router requires a user URL lister")
		}

		authMiddleware := authenticationMiddleware(
			logger,
			authService,
			cookieSecure,
		)

		router.With(
			authMiddleware,
			rateLimitMiddleware(
				logger,
				createURLLimiter,
			),
		).Post(
			"/api/v1/urls",
			createOwnedURLHandler(
				logger,
				ownedCreator,
			),
		)

		router.With(
			authMiddleware,
		).Get(
			"/api/v1/urls",
			listUserURLsHandler(
				logger,
				userLister,
			),
		)
	} else {
		router.With(
			rateLimitMiddleware(
				logger,
				createURLLimiter,
			),
		).Post(
			"/api/v1/urls",
			createURLHandler(logger, creator),
		)
	}

	if analyticsReader != nil {
		if authService != nil {
			ownerFinder, ok := analyticsReader.(userURLFinder)
			if !ok {
				panic(
					"authenticated analytics requires a user URL finder",
				)
			}

			router.With(
				authenticationMiddleware(
					logger,
					authService,
					cookieSecure,
				),
				analyticsOwnershipMiddleware(
					logger,
					ownerFinder,
				),
			).Get(
				"/api/v1/urls/{shortCode}/analytics",
				redirectAnalyticsHandler(
					logger,
					analyticsReader,
				),
			)
		} else {
			router.Get(
				"/api/v1/urls/{shortCode}/analytics",
				redirectAnalyticsHandler(
					logger,
					analyticsReader,
				),
			)
		}
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
