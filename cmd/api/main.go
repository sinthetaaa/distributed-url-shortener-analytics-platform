package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type config struct {
	Port        string
	DatabaseURL string
}

type databasePinger interface {
	Ping(context.Context) error
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

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

	addr := ":" + cfg.Port

	router := newRouter(logger, pool)

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("shutting down ShortScale API")

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
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

	return config{
		Port:        port,
		DatabaseURL: databaseURL,
	}, nil
}

func newRouter(logger *slog.Logger, database databasePinger) http.Handler {
	router := chi.NewRouter()

	router.Get("/health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSONStatus(w, logger, http.StatusOK, "ok")
	})

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

	return router
}

func writeJSONStatus(w http.ResponseWriter, logger *slog.Logger, statusCode int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": status,
	}); err != nil {
		logger.Error("failed to encode health response", "error", err)
	}
}
