package aggregator

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type ExportFunc func(ctx context.Context, metricDate time.Time) (string, error)

type Service struct {
	chRepo *ClickHouseRepo
	pgRepo *PostgresRepo
	logger *slog.Logger
}

func NewService(chRepo *ClickHouseRepo, pgRepo *PostgresRepo, logger *slog.Logger) *Service {
	return &Service{
		chRepo: chRepo,
		pgRepo: pgRepo,
		logger: logger,
	}
}

type RunResult struct {
	MetricDate     string        `json:"metric_date"`
	RecordsScanned uint64        `json:"records_scanned"`
	DAU            uint64        `json:"dau"`
	AvgWatchSec    float64       `json:"avg_watch_seconds"`
	ViewConversion float64       `json:"view_conversion"`
	RetentionD1    float64       `json:"retention_d1"`
	RetentionD7    float64       `json:"retention_d7"`
	TopMoviesCount int           `json:"top_movies_count"`
	DurationMs     int64         `json:"duration_ms"`
	CalculatedAt   time.Time     `json:"calculated_at"`
}

func (s *Service) RunForDate(ctx context.Context, metricDate time.Time) (*RunResult, error) {
	metricDate = time.Date(metricDate.Year(), metricDate.Month(), metricDate.Day(), 0, 0, 0, 0, time.UTC)
	dateStr := metricDate.Format("2006-01-02")
	started := time.Now()

	s.logger.Info("aggregation cycle started", "metric_date", dateStr)

	metrics, err := s.chRepo.ComputeDailyMetrics(ctx, metricDate)
	if err != nil {
		return nil, fmt.Errorf("compute daily metrics for %s: %w", dateStr, err)
	}

	topMovies, err := s.chRepo.ComputeTopMovies(ctx, metricDate, 10)
	if err != nil {
		return nil, fmt.Errorf("compute top movies for %s: %w", dateStr, err)
	}

	deviceStats, err := s.chRepo.ComputeDeviceStats(ctx, metricDate)
	if err != nil {
		return nil, fmt.Errorf("compute device stats for %s: %w", dateStr, err)
	}

	cohorts, err := s.chRepo.ComputeRetentionCohorts(ctx, metricDate, 14)
	if err != nil {
		return nil, fmt.Errorf("compute retention cohorts up to %s: %w", dateStr, err)
	}

	if err := s.chRepo.SaveDailyMetrics(ctx, metrics); err != nil {
		return nil, fmt.Errorf("save clickhouse daily_metrics: %w", err)
	}
	if err := s.chRepo.SaveTopMovies(ctx, topMovies); err != nil {
		return nil, fmt.Errorf("save clickhouse daily_top_movies: %w", err)
	}
	if err := s.chRepo.SaveDeviceStats(ctx, deviceStats); err != nil {
		return nil, fmt.Errorf("save clickhouse daily_device_stats: %w", err)
	}
	if err := s.chRepo.SaveRetentionCohorts(ctx, cohorts); err != nil {
		return nil, fmt.Errorf("save clickhouse retention_cohorts: %w", err)
	}

	if err := s.pgRepo.UpsertDailyAggregates(ctx, metrics, topMovies); err != nil {
		return nil, fmt.Errorf("upsert postgres aggregates: %w", err)
	}

	duration := time.Since(started)
	s.logger.Info("aggregation cycle completed",
		"metric_date", dateStr,
		"records_processed", metrics.TotalEvents,
		"dau", metrics.DAU,
		"avg_watch_seconds", metrics.AvgWatchSeconds,
		"view_conversion", metrics.ViewConversion,
		"retention_d1", metrics.RetentionD1,
		"retention_d7", metrics.RetentionD7,
		"top_movies_count", len(topMovies),
		"cohorts_rows", len(cohorts),
		"duration_ms", duration.Milliseconds(),
	)

	return &RunResult{
		MetricDate:     dateStr,
		RecordsScanned: metrics.TotalEvents,
		DAU:            metrics.DAU,
		AvgWatchSec:    metrics.AvgWatchSeconds,
		ViewConversion: metrics.ViewConversion,
		RetentionD1:    metrics.RetentionD1,
		RetentionD7:    metrics.RetentionD7,
		TopMoviesCount: len(topMovies),
		DurationMs:     duration.Milliseconds(),
		CalculatedAt:   metrics.CalculatedAt,
	}, nil
}

func (s *Service) RunBackfill(ctx context.Context, days int, exportFn ExportFunc) {
	now := time.Now().UTC()
	for offset := days; offset >= 0; offset-- {
		if ctx.Err() != nil {
			return
		}
		target := now.AddDate(0, 0, -offset)
		if _, err := s.RunForDate(ctx, target); err != nil {
			s.logger.Error("backfill aggregation failed", "date", target.Format("2006-01-02"), "error", err)
			continue
		}
		if exportFn != nil {
			if _, err := exportFn(ctx, target); err != nil {
				s.logger.Error("backfill s3 export failed", "date", target.Format("2006-01-02"), "error", err)
			}
		}
	}
}
