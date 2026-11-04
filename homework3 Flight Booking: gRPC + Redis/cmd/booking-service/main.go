package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flightbooking/internal/booking"
	"flightbooking/internal/booking/flightclient"
	"flightbooking/internal/booking/migrations"
	"flightbooking/internal/platform"
)

type config struct {
	HTTPAddr    string
	DatabaseURL string
	Flight      flightclient.Config
}

func loadConfig() (config, error) {
	var env platform.Env
	cfg := config{
		HTTPAddr:    env.String("HTTP_ADDR", ":8080"),
		DatabaseURL: env.Required("DATABASE_URL"),
		Flight: flightclient.Config{
			Addr:   env.String("FLIGHT_SERVICE_ADDR", "localhost:50051"),
			APIKey: env.Required("FLIGHT_SERVICE_API_KEY"),
			Retry: flightclient.RetryConfig{
				MaxAttempts:    env.Int("RETRY_MAX_ATTEMPTS", 3),
				InitialBackoff: env.Duration("RETRY_INITIAL_BACKOFF", 100*time.Millisecond),
				AttemptTimeout: env.Duration("RETRY_ATTEMPT_TIMEOUT", 2*time.Second),
			},
			Breaker: flightclient.BreakerConfig{
				FailureThreshold: env.Int("CB_FAILURE_THRESHOLD", 5),
				Window:           env.Duration("CB_WINDOW", 30*time.Second),
				OpenTimeout:      env.Duration("CB_OPEN_TIMEOUT", 15*time.Second),
			},
		},
	}
	return cfg, env.Err()
}

func main() {
	log := platform.NewLogger("booking-service")
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := platform.ConnectPostgres(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := platform.Migrate(ctx, pool, migrations.FS, log); err != nil {
		return err
	}

	flights, err := flightclient.New(cfg.Flight, log)
	if err != nil {
		return err
	}
	defer flights.Close()
	log.Info("flight service client configured", "addr", cfg.Flight.Addr,
		"retry", cfg.Flight.Retry, "circuit_breaker", cfg.Flight.Breaker)

	svc := booking.NewService(booking.NewRepository(pool), flights, log)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           booking.NewHandler(svc, log),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("HTTP server listening", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
