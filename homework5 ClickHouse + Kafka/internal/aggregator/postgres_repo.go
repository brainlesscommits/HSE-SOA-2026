package aggregator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DailyAggregateRecord struct {
	MetricDate   string    `json:"metric_date"`
	MetricName   string    `json:"metric_name"`
	MetricValue  float64   `json:"metric_value"`
	CalculatedAt time.Time `json:"calculated_at"`
}

type DailyExportReport struct {
	MetricDate string                 `json:"metric_date"`
	ExportedAt time.Time              `json:"exported_at"`
	Metrics    []DailyAggregateRecord `json:"metrics"`
	TopMovies  []TopMovie             `json:"top_movies"`
}

type PostgresRepo struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewPostgresRepo(ctx context.Context, dsn string, logger *slog.Logger) (*PostgresRepo, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	var lastErr error
	for i := 0; i < 15; i++ {
		if err := pool.Ping(ctx); err == nil {
			return &PostgresRepo{pool: pool, logger: logger}, nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	pool.Close()
	return nil, fmt.Errorf("ping postgres failed: %w", lastErr)
}

func (r *PostgresRepo) Close() {
	r.pool.Close()
}

func (r *PostgresRepo) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *PostgresRepo) UpsertDailyAggregates(ctx context.Context, m *DailyMetrics, topMovies []TopMovie) error {
	const maxRetries = 5
	backoff := 300 * time.Millisecond
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := r.upsertOnce(ctx, m, topMovies)
		if err == nil {
			return nil
		}
		lastErr = err
		r.logger.Warn("postgres write failed, retrying",
			"attempt", attempt,
			"max_retries", maxRetries,
			"metric_date", m.MetricDate.Format("2006-01-02"),
			"error", err,
		)
		if attempt == maxRetries {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
		}
	}
	return fmt.Errorf("postgres upsert failed after %d attempts: %w", maxRetries, lastErr)
}

func (r *PostgresRepo) upsertOnce(ctx context.Context, m *DailyMetrics, topMovies []TopMovie) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	dateStr := m.MetricDate.Format("2006-01-02")

	metrics := []struct {
		name  string
		value float64
	}{
		{"dau", float64(m.DAU)},
		{"avg_watch_seconds", m.AvgWatchSeconds},
		{"view_started_count", float64(m.ViewStartedCount)},
		{"view_finished_count", float64(m.ViewFinishedCount)},
		{"view_conversion", m.ViewConversion},
		{"retention_d1", m.RetentionD1},
		{"retention_d7", m.RetentionD7},
		{"total_events", float64(m.TotalEvents)},
	}

	const upsertMetricSQL = `
		INSERT INTO daily_aggregates (metric_date, metric_name, metric_value, calculated_at)
		VALUES ($1::date, $2, $3, $4)
		ON CONFLICT (metric_date, metric_name)
		DO UPDATE SET
			metric_value = EXCLUDED.metric_value,
			calculated_at = EXCLUDED.calculated_at
	`
	for _, item := range metrics {
		if _, err := tx.Exec(ctx, upsertMetricSQL, dateStr, item.name, item.value, m.CalculatedAt); err != nil {
			return fmt.Errorf("upsert metric %s: %w", item.name, err)
		}
	}

	const upsertMovieSQL = `
		INSERT INTO top_movies_daily (metric_date, movie_id, views_count, finished_count, avg_progress_seconds, rank, calculated_at)
		VALUES ($1::date, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (metric_date, movie_id)
		DO UPDATE SET
			views_count = EXCLUDED.views_count,
			finished_count = EXCLUDED.finished_count,
			avg_progress_seconds = EXCLUDED.avg_progress_seconds,
			rank = EXCLUDED.rank,
			calculated_at = EXCLUDED.calculated_at
	`
	for _, mv := range topMovies {
		if _, err := tx.Exec(ctx, upsertMovieSQL,
			dateStr, mv.MovieID, int64(mv.ViewsCount), int64(mv.FinishedCount), mv.AvgProgressSeconds, int(mv.Rank), mv.CalculatedAt,
		); err != nil {
			return fmt.Errorf("upsert top movie %s: %w", mv.MovieID, err)
		}
	}

	return tx.Commit(ctx)
}

func (r *PostgresRepo) GetDailyReport(ctx context.Context, metricDate time.Time) (*DailyExportReport, error) {
	dateStr := metricDate.Format("2006-01-02")

	report := &DailyExportReport{
		MetricDate: dateStr,
		ExportedAt: time.Now().UTC(),
		Metrics:    make([]DailyAggregateRecord, 0),
		TopMovies:  make([]TopMovie, 0),
	}

	rows, err := r.pool.Query(ctx, `
		SELECT metric_name, metric_value, calculated_at
		FROM daily_aggregates
		WHERE metric_date = $1::date
		ORDER BY metric_name ASC
	`, dateStr)
	if err != nil {
		return nil, fmt.Errorf("query daily_aggregates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var rec DailyAggregateRecord
		rec.MetricDate = dateStr
		if err := rows.Scan(&rec.MetricName, &rec.MetricValue, &rec.CalculatedAt); err != nil {
			return nil, fmt.Errorf("scan daily_aggregate: %w", err)
		}
		report.Metrics = append(report.Metrics, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	movieRows, err := r.pool.Query(ctx, `
		SELECT movie_id, views_count, finished_count, avg_progress_seconds, rank, calculated_at
		FROM top_movies_daily
		WHERE metric_date = $1::date
		ORDER BY rank ASC
	`, dateStr)
	if err != nil {
		return nil, fmt.Errorf("query top_movies_daily: %w", err)
	}
	defer movieRows.Close()

	for movieRows.Next() {
		var tm TopMovie
		tm.MetricDate = metricDate
		var views, finished int64
		var rank int
		if err := movieRows.Scan(&tm.MovieID, &views, &finished, &tm.AvgProgressSeconds, &rank, &tm.CalculatedAt); err != nil {
			return nil, fmt.Errorf("scan top_movie: %w", err)
		}
		tm.ViewsCount = uint64(views)
		tm.FinishedCount = uint64(finished)
		tm.Rank = uint32(rank)
		report.TopMovies = append(report.TopMovies, tm)
	}
	if err := movieRows.Err(); err != nil {
		return nil, err
	}

	return report, nil
}
