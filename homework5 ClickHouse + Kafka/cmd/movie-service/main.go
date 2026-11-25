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

	"cinema-analytics/internal/generator"
	"cinema-analytics/internal/httpapi"
	"cinema-analytics/internal/producer"
	"cinema-analytics/internal/schema"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	brokers := strings.Split(getEnv("KAFKA_BROKERS", "kafka-1:9092,kafka-2:9092"), ",")
	topic := getEnv("KAFKA_TOPIC", "movie-events")
	registryURL := getEnv("SCHEMA_REGISTRY_URL", "http://schema-registry:8081")
	schemaPath := getEnv("AVRO_SCHEMA_PATH", "/app/schemas/movie_event.avsc")
	httpAddr := getEnv("HTTP_ADDR", ":8080")
	genEnabled := getEnvBool("GENERATOR_ENABLED", true)
	genInterval := getEnvDuration("GENERATOR_INTERVAL", 3*time.Second)
	seedDays := getEnvInt("SEED_HISTORICAL_DAYS", 8)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("initializing Avro encoder via Schema Registry", "url", registryURL, "topic", topic)
	encoder, err := schema.NewEncoder(ctx, registryURL, topic+"-value", schemaPath)
	if err != nil {
		logger.Error("failed to initialize schema encoder", "error", err)
		os.Exit(1)
	}
	logger.Info("schema encoder ready", "schema_id", encoder.SchemaID())

	kafkaProd := producer.NewKafkaProducer(brokers, topic, encoder, logger)
	defer kafkaProd.Close()

	if genEnabled {
		gen := generator.NewGenerator(kafkaProd, logger, genInterval)
		go func() {

			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			gen.SeedHistoricalData(ctx, seedDays)
			gen.Run(ctx)
		}()
	}

	mux := http.NewServeMux()
	handler := httpapi.NewMovieHandler(kafkaProd, logger)
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("movie-service HTTP server listening", "addr", httpAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down movie-service...")

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
