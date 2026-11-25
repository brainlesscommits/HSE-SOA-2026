CREATE DATABASE IF NOT EXISTS analytics;

CREATE TABLE IF NOT EXISTS analytics.movie_events_kafka (
    event_id String,
    user_id String,
    movie_id String,
    event_type String,
    timestamp DateTime64(3, 'UTC'),
    device_type String,
    session_id String,
    progress_seconds Int32
) ENGINE = Kafka
SETTINGS
    kafka_broker_list = 'kafka-1:9092,kafka-2:9092',
    kafka_topic_list = 'movie-events',
    kafka_group_name = 'clickhouse_movie_events_consumer',
    kafka_format = 'AvroConfluent',
    format_avro_schema_registry_url = 'http://schema-registry:8081',
    kafka_num_consumers = 1,
    kafka_max_block_size = 1048576;

CREATE TABLE IF NOT EXISTS analytics.movie_events (
    event_id UUID,
    user_id String,
    movie_id String,
    event_type Enum8(
        'VIEW_STARTED' = 1,
        'VIEW_FINISHED' = 2,
        'VIEW_PAUSED' = 3,
        'VIEW_RESUMED' = 4,
        'LIKED' = 5,
        'SEARCHED' = 6
    ),
    timestamp DateTime64(3, 'UTC'),
    event_date Date MATERIALIZED toDate(timestamp),
    device_type Enum8(
        'MOBILE' = 1,
        'DESKTOP' = 2,
        'TV' = 3,
        'TABLET' = 4
    ),
    session_id String,
    progress_seconds Int32
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (toDate(timestamp), event_type, user_id, timestamp, event_id);

CREATE MATERIALIZED VIEW IF NOT EXISTS analytics.movie_events_mv
TO analytics.movie_events AS
SELECT
    toUUID(event_id) AS event_id,
    user_id,
    movie_id,
    event_type,
    timestamp,
    device_type,
    session_id,
    progress_seconds
FROM analytics.movie_events_kafka;

CREATE TABLE IF NOT EXISTS analytics.daily_metrics (
    metric_date Date,
    dau UInt64,
    avg_watch_seconds Float64,
    view_started_count UInt64,
    view_finished_count UInt64,
    view_conversion Float64,
    retention_d1 Float64,
    retention_d7 Float64,
    total_events UInt64,
    calculated_at DateTime('UTC')
) ENGINE = ReplacingMergeTree(calculated_at)
PARTITION BY toYYYYMM(metric_date)
ORDER BY (metric_date);

CREATE TABLE IF NOT EXISTS analytics.daily_top_movies (
    metric_date Date,
    movie_id String,
    views_count UInt64,
    finished_count UInt64,
    avg_progress_seconds Float64,
    rank UInt32,
    calculated_at DateTime('UTC')
) ENGINE = ReplacingMergeTree(calculated_at)
PARTITION BY toYYYYMM(metric_date)
ORDER BY (metric_date, movie_id);

CREATE TABLE IF NOT EXISTS analytics.retention_cohorts (
    cohort_date Date,
    day_number UInt8,
    cohort_size UInt64,
    retained_users UInt64,
    retention_rate Float64,
    calculated_at DateTime('UTC')
) ENGINE = ReplacingMergeTree(calculated_at)
PARTITION BY toYYYYMM(cohort_date)
ORDER BY (cohort_date, day_number);

CREATE TABLE IF NOT EXISTS analytics.daily_device_stats (
    metric_date Date,
    device_type String,
    events_count UInt64,
    unique_users UInt64,
    calculated_at DateTime('UTC')
) ENGINE = ReplacingMergeTree(calculated_at)
PARTITION BY toYYYYMM(metric_date)
ORDER BY (metric_date, device_type);
