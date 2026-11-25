package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cinema-analytics/internal/aggregator"
	"cinema-analytics/internal/exporter"
	"cinema-analytics/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	chAddr := getEnv("CLICKHOUSE_ADDR", "clickhouse:9000")
	chDB := getEnv("CLICKHOUSE_DB", "analytics")
	chUser := getEnv("CLICKHOUSE_USER", "default")
	chPass := getEnv("CLICKHOUSE_PASSWORD", "")

	pgDSN := getEnv("POSTGRES_DSN", "postgres://cinema:cinema@postgres:5432/cinema_analytics?sslmode=disable")

	s3Endpoint := getEnv("S3_ENDPOINT", "minio:9000")
	s3AccessKey := getEnv("S3_ACCESS_KEY", "minioadmin")
	s3SecretKey := getEnv("S3_SECRET_KEY", "minioadmin")
	s3Bucket := getEnv("S3_BUCKET", "movie-analytics")
	s3UseSSL := getEnvBool("S3_USE_SSL", false)

	aggInterval := getEnvDuration("AGGREGATION_INTERVAL", 1*time.Minute)
	exportInterval := getEnvDuration("EXPORT_INTERVAL", 5*time.Minute)
	backfillDays := getEnvInt("BACKFILL_DAYS", 8)
	httpAddr := getEnv("HTTP_ADDR", ":8081")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	chRepo, err := aggregator.NewClickHouseRepo(ctx, chAddr, chDB, chUser, chPass)
	if err != nil {
		logger.Error("failed to connect to ClickHouse", "error", err)
		os.Exit(1)
	}
	defer chRepo.Close()

	pgRepo, err := aggregator.NewPostgresRepo(ctx, pgDSN, logger)
	if err != nil {
		logger.Error("failed to connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer pgRepo.Close()

	s3Exp, err := exporter.NewS3Exporter(ctx, s3Endpoint, s3AccessKey, s3SecretKey, s3Bucket, s3UseSSL, pgRepo, logger)
	if err != nil {
		logger.Error("failed to initialize S3 exporter", "error", err)
		os.Exit(1)
	}

	svc := aggregator.NewService(chRepo, pgRepo, logger)

	go func() {

		select {
		case <-ctx.Done():
			return
		case <-time.After(12 * time.Second):
		}

		logger.Info("running initial backfill aggregation and S3 export", "days", backfillDays)
		svc.RunBackfill(ctx, backfillDays, s3Exp.ExportDate)

		ticker := time.NewTicker(aggInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				today := time.Now().UTC()
				if _, err := svc.RunForDate(ctx, today); err != nil {
					logger.Error("scheduled aggregation failed", "error", err)
				}
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(exportInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().UTC()
				for _, target := range []time.Time{now.AddDate(0, 0, -1), now} {
					if _, err := s3Exp.ExportDate(ctx, target); err != nil {
						logger.Error("scheduled S3 export failed, will retry on next tick",
							"date", target.Format("2006-01-02"),
							"error", err,
						)
					}
				}
			}
		}
	}()

	mux := http.NewServeMux()
	handler := httpapi.NewAggregationHandler(svc, s3Exp, logger)
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("aggregation-service HTTP server listening", "addr", httpAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down aggregation-service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
