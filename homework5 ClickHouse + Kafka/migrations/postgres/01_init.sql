CREATE TABLE IF NOT EXISTS daily_aggregates (
    metric_date DATE NOT NULL,
    metric_name TEXT NOT NULL,
    metric_value DOUBLE PRECISION NOT NULL,
    calculated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (metric_date, metric_name)
);

CREATE INDEX IF NOT EXISTS idx_daily_aggregates_date ON daily_aggregates (metric_date);

CREATE TABLE IF NOT EXISTS top_movies_daily (
    metric_date DATE NOT NULL,
    movie_id TEXT NOT NULL,
    views_count BIGINT NOT NULL,
    finished_count BIGINT NOT NULL,
    avg_progress_seconds DOUBLE PRECISION NOT NULL,
    rank INT NOT NULL,
    calculated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (metric_date, movie_id)
);

CREATE INDEX IF NOT EXISTS idx_top_movies_daily_date_rank ON top_movies_daily (metric_date, rank);
