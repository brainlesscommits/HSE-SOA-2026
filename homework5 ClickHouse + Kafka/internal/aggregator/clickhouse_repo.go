package aggregator

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type DailyMetrics struct {
	MetricDate        time.Time
	DAU               uint64
	AvgWatchSeconds   float64
	ViewStartedCount  uint64
	ViewFinishedCount uint64
	ViewConversion    float64
	RetentionD1       float64
	RetentionD7       float64
	TotalEvents       uint64
	CalculatedAt      time.Time
}

type TopMovie struct {
	MetricDate         time.Time `json:"metric_date"`
	MovieID            string    `json:"movie_id"`
	ViewsCount         uint64    `json:"views_count"`
	FinishedCount      uint64    `json:"finished_count"`
	AvgProgressSeconds float64   `json:"avg_progress_seconds"`
	Rank               uint32    `json:"rank"`
	CalculatedAt       time.Time `json:"calculated_at"`
}

type CohortRow struct {
	CohortDate    time.Time
	DayNumber     uint8
	CohortSize    uint64
	RetainedUsers uint64
	RetentionRate float64
	CalculatedAt  time.Time
}

type DeviceStat struct {
	MetricDate   time.Time
	DeviceType   string
	EventsCount  uint64
	UniqueUsers  uint64
	CalculatedAt time.Time
}

type ClickHouseRepo struct {
	conn driver.Conn
}

func NewClickHouseRepo(ctx context.Context, addr, db, user, password string) (*ClickHouseRepo, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: db,
			Username: user,
			Password: password,
		},
		DialTimeout:     10 * time.Second,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}

	var lastErr error
	for i := 0; i < 15; i++ {
		if err := conn.Ping(ctx); err == nil {
			return &ClickHouseRepo{conn: conn}, nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("ping clickhouse failed: %w", lastErr)
}

func (r *ClickHouseRepo) Close() error {
	return r.conn.Close()
}

func (r *ClickHouseRepo) Ping(ctx context.Context) error {
	return r.conn.Ping(ctx)
}

func (r *ClickHouseRepo) ComputeDailyMetrics(ctx context.Context, metricDate time.Time) (*DailyMetrics, error) {
	dateStr := metricDate.Format("2006-01-02")
	now := time.Now().UTC().Truncate(time.Second)

	const coreQuery = `
		SELECT
			uniqExact(user_id) AS dau,
			ifNull(avgIf(progress_seconds, event_type = 'VIEW_FINISHED'), 0.0) AS avg_watch_seconds,
			countIf(event_type = 'VIEW_STARTED') AS view_started_count,
			countIf(event_type = 'VIEW_FINISHED') AS view_finished_count,
			if(countIf(event_type = 'VIEW_STARTED') > 0,
			   countIf(event_type = 'VIEW_FINISHED') / countIf(event_type = 'VIEW_STARTED'),
			   0.0) AS view_conversion,
			count() AS total_events
		FROM analytics.movie_events
		WHERE event_date = toDate(?)
	`

	m := &DailyMetrics{
		MetricDate:   metricDate,
		CalculatedAt: now,
	}

	row := r.conn.QueryRow(ctx, coreQuery, dateStr)
	if err := row.Scan(
		&m.DAU,
		&m.AvgWatchSeconds,
		&m.ViewStartedCount,
		&m.ViewFinishedCount,
		&m.ViewConversion,
		&m.TotalEvents,
	); err != nil {
		return nil, fmt.Errorf("query core daily metrics: %w", err)
	}
	if math.IsNaN(m.AvgWatchSeconds) || math.IsInf(m.AvgWatchSeconds, 0) {
		m.AvgWatchSeconds = 0
	}
	if math.IsNaN(m.ViewConversion) || math.IsInf(m.ViewConversion, 0) {
		m.ViewConversion = 0
	}

	retD1, err := r.computeRetentionForOffset(ctx, metricDate, 1)
	if err != nil {
		return nil, fmt.Errorf("compute retention D1: %w", err)
	}
	retD7, err := r.computeRetentionForOffset(ctx, metricDate, 7)
	if err != nil {
		return nil, fmt.Errorf("compute retention D7: %w", err)
	}
	m.RetentionD1 = retD1
	m.RetentionD7 = retD7

	return m, nil
}

func (r *ClickHouseRepo) computeRetentionForOffset(ctx context.Context, metricDate time.Time, dayOffset int) (float64, error) {
	cohortDateStr := metricDate.AddDate(0, 0, -dayOffset).Format("2006-01-02")
	targetDateStr := metricDate.Format("2006-01-02")

	const q = `
		WITH first_views AS (
			SELECT
				user_id,
				min(event_date) AS cohort_date
			FROM analytics.movie_events
			WHERE event_type = 'VIEW_STARTED'
			GROUP BY user_id
		),
		cohort_users AS (
			SELECT user_id
			FROM first_views
			WHERE cohort_date = toDate(?)
		),
		returned_users AS (
			SELECT DISTINCT user_id
			FROM analytics.movie_events
			WHERE event_date = toDate(?)
			  AND user_id IN (SELECT user_id FROM cohort_users)
		)
		SELECT
			if((SELECT count() FROM cohort_users) > 0,
			   (SELECT count() FROM returned_users) / (SELECT count() FROM cohort_users),
			   0.0) AS retention_rate
	`

	var rate float64
	if err := r.conn.QueryRow(ctx, q, cohortDateStr, targetDateStr).Scan(&rate); err != nil {
		return 0, err
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, nil
	}
	return rate, nil
}

func (r *ClickHouseRepo) ComputeTopMovies(ctx context.Context, metricDate time.Time, limit int) ([]TopMovie, error) {
	dateStr := metricDate.Format("2006-01-02")
	now := time.Now().UTC().Truncate(time.Second)

	const q = `
		SELECT
			movie_id,
			views_count,
			finished_count,
			avg_progress,
			toUInt32(row_number() OVER (ORDER BY views_count DESC, finished_count DESC, movie_id ASC)) AS rank
		FROM (
			SELECT
				movie_id,
				countIf(event_type = 'VIEW_STARTED') AS views_count,
				countIf(event_type = 'VIEW_FINISHED') AS finished_count,
				ifNull(avgIf(progress_seconds, event_type = 'VIEW_FINISHED'), 0.0) AS avg_progress
			FROM analytics.movie_events
			WHERE event_date = toDate(?)
			GROUP BY movie_id
			HAVING views_count > 0
		)
		ORDER BY rank ASC
		LIMIT ?
	`

	rows, err := r.conn.Query(ctx, q, dateStr, limit)
	if err != nil {
		return nil, fmt.Errorf("query top movies: %w", err)
	}
	defer rows.Close()

	var result []TopMovie
	for rows.Next() {
		var tm TopMovie
		tm.MetricDate = metricDate
		tm.CalculatedAt = now
		if err := rows.Scan(&tm.MovieID, &tm.ViewsCount, &tm.FinishedCount, &tm.AvgProgressSeconds, &tm.Rank); err != nil {
			return nil, fmt.Errorf("scan top movie: %w", err)
		}
		if math.IsNaN(tm.AvgProgressSeconds) {
			tm.AvgProgressSeconds = 0
		}
		result = append(result, tm)
	}
	return result, rows.Err()
}

func (r *ClickHouseRepo) ComputeDeviceStats(ctx context.Context, metricDate time.Time) ([]DeviceStat, error) {
	dateStr := metricDate.Format("2006-01-02")
	now := time.Now().UTC().Truncate(time.Second)

	const q = `
		SELECT
			toString(device_type) AS device_type,
			count() AS events_count,
			uniqExact(user_id) AS unique_users
		FROM analytics.movie_events
		WHERE event_date = toDate(?)
		GROUP BY device_type
	`

	rows, err := r.conn.Query(ctx, q, dateStr)
	if err != nil {
		return nil, fmt.Errorf("query device stats: %w", err)
	}
	defer rows.Close()

	var result []DeviceStat
	for rows.Next() {
		var ds DeviceStat
		ds.MetricDate = metricDate
		ds.CalculatedAt = now
		if err := rows.Scan(&ds.DeviceType, &ds.EventsCount, &ds.UniqueUsers); err != nil {
			return nil, fmt.Errorf("scan device stat: %w", err)
		}
		result = append(result, ds)
	}
	return result, rows.Err()
}

func (r *ClickHouseRepo) ComputeRetentionCohorts(ctx context.Context, upToDate time.Time, lookbackDays int) ([]CohortRow, error) {
	startDateStr := upToDate.AddDate(0, 0, -lookbackDays).Format("2006-01-02")
	endDateStr := upToDate.Format("2006-01-02")
	now := time.Now().UTC().Truncate(time.Second)

	const q = `
		WITH first_views AS (
			SELECT
				user_id,
				min(event_date) AS cohort_date
			FROM analytics.movie_events
			WHERE event_type = 'VIEW_STARTED'
			GROUP BY user_id
			HAVING cohort_date >= toDate(?) AND cohort_date <= toDate(?)
		),
		cohort_sizes AS (
			SELECT
				cohort_date,
				uniqExact(user_id) AS cohort_size
			FROM first_views
			GROUP BY cohort_date
		),
		user_activity AS (
			SELECT DISTINCT
				user_id,
				event_date
			FROM analytics.movie_events
			WHERE event_date >= toDate(?) AND event_date <= toDate(?)
		)
		SELECT
			fv.cohort_date,
			toUInt8(dateDiff('day', fv.cohort_date, ua.event_date)) AS day_number,
			any(cs.cohort_size) AS cohort_size,
			uniqExact(ua.user_id) AS retained_users,
			if(any(cs.cohort_size) > 0, uniqExact(ua.user_id) / any(cs.cohort_size), 0.0) AS retention_rate
		FROM first_views fv
		INNER JOIN user_activity ua ON fv.user_id = ua.user_id
		INNER JOIN cohort_sizes cs ON fv.cohort_date = cs.cohort_date
		WHERE dateDiff('day', fv.cohort_date, ua.event_date) BETWEEN 0 AND 7
		GROUP BY fv.cohort_date, day_number
		ORDER BY fv.cohort_date ASC, day_number ASC
	`

	rows, err := r.conn.Query(ctx, q, startDateStr, endDateStr, startDateStr, endDateStr)
	if err != nil {
		return nil, fmt.Errorf("query retention cohorts: %w", err)
	}
	defer rows.Close()

	var result []CohortRow
	for rows.Next() {
		var cr CohortRow
		cr.CalculatedAt = now
		if err := rows.Scan(&cr.CohortDate, &cr.DayNumber, &cr.CohortSize, &cr.RetainedUsers, &cr.RetentionRate); err != nil {
			return nil, fmt.Errorf("scan cohort row: %w", err)
		}
		if math.IsNaN(cr.RetentionRate) {
			cr.RetentionRate = 0
		}
		result = append(result, cr)
	}
	return result, rows.Err()
}

func (r *ClickHouseRepo) SaveDailyMetrics(ctx context.Context, m *DailyMetrics) error {
	const q = `
		INSERT INTO analytics.daily_metrics
			(metric_date, dau, avg_watch_seconds, view_started_count, view_finished_count, view_conversion, retention_d1, retention_d7, total_events, calculated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	return r.conn.Exec(ctx, q,
		m.MetricDate,
		m.DAU,
		m.AvgWatchSeconds,
		m.ViewStartedCount,
		m.ViewFinishedCount,
		m.ViewConversion,
		m.RetentionD1,
		m.RetentionD7,
		m.TotalEvents,
		m.CalculatedAt,
	)
}

func (r *ClickHouseRepo) SaveTopMovies(ctx context.Context, movies []TopMovie) error {
	if len(movies) == 0 {
		return nil
	}
	batch, err := r.conn.PrepareBatch(ctx, `
		INSERT INTO analytics.daily_top_movies
			(metric_date, movie_id, views_count, finished_count, avg_progress_seconds, rank, calculated_at)
	`)
	if err != nil {
		return err
	}
	for _, m := range movies {
		if err := batch.Append(m.MetricDate, m.MovieID, m.ViewsCount, m.FinishedCount, m.AvgProgressSeconds, m.Rank, m.CalculatedAt); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (r *ClickHouseRepo) SaveDeviceStats(ctx context.Context, stats []DeviceStat) error {
	if len(stats) == 0 {
		return nil
	}
	batch, err := r.conn.PrepareBatch(ctx, `
		INSERT INTO analytics.daily_device_stats
			(metric_date, device_type, events_count, unique_users, calculated_at)
	`)
	if err != nil {
		return err
	}
	for _, s := range stats {
		if err := batch.Append(s.MetricDate, s.DeviceType, s.EventsCount, s.UniqueUsers, s.CalculatedAt); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (r *ClickHouseRepo) SaveRetentionCohorts(ctx context.Context, cohorts []CohortRow) error {
	if len(cohorts) == 0 {
		return nil
	}
	batch, err := r.conn.PrepareBatch(ctx, `
		INSERT INTO analytics.retention_cohorts
			(cohort_date, day_number, cohort_size, retained_users, retention_rate, calculated_at)
	`)
	if err != nil {
		return err
	}
	for _, c := range cohorts {
		if err := batch.Append(c.CohortDate, c.DayNumber, c.CohortSize, c.RetainedUsers, c.RetentionRate, c.CalculatedAt); err != nil {
			return err
		}
	}
	return batch.Send()
}
