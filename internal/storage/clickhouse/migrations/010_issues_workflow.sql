-- Issue workflow: snoozes, resolve-in-release, an append-only activity log and release first-seen.
ALTER TABLE issues_state ADD COLUMN IF NOT EXISTS snooze_until DateTime64(3, 'UTC') DEFAULT toDateTime64(0, 3, 'UTC');

ALTER TABLE issues_state ADD COLUMN IF NOT EXISTS snooze_count UInt64 DEFAULT 0;

ALTER TABLE issues_state ADD COLUMN IF NOT EXISTS resolved_in_release String DEFAULT '';

CREATE TABLE IF NOT EXISTS issues_activity (
    ts DateTime64(3, 'UTC'),
    project_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    issue_id String,
    id String,
    kind LowCardinality(String),
    actor String,
    from_value String,
    to_value String,
    comment String CODEC(ZSTD(3)),
    release LowCardinality(String),
    INDEX idx_issue issue_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (project_id, issue_id, ts)
TTL toDate(ts) + INTERVAL 730 DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

CREATE TABLE IF NOT EXISTS releases (
    project_id String,
    release LowCardinality(String),
    first_seen SimpleAggregateFunction(min, DateTime64(3, 'UTC'))
)
ENGINE = AggregatingMergeTree()
ORDER BY (project_id, release)
SETTINGS index_granularity = 8192;

INSERT INTO releases
SELECT project_id, release, min(first_seen) AS first_seen
FROM (
    SELECT project_id, release, min(ts) AS first_seen FROM error_occurrences WHERE release != '' GROUP BY project_id, release
    UNION ALL
    SELECT project_id, release, min(ts) AS first_seen FROM logs_raw WHERE release != '' GROUP BY project_id, release
)
WHERE (SELECT count() FROM releases) = 0
GROUP BY project_id, release;

CREATE MATERIALIZED VIEW IF NOT EXISTS releases_errors_mv TO releases AS
SELECT project_id, release, min(ts) AS first_seen
FROM error_occurrences
WHERE release != ''
GROUP BY project_id, release;

CREATE MATERIALIZED VIEW IF NOT EXISTS releases_logs_mv TO releases AS
SELECT project_id, release, min(ts) AS first_seen
FROM logs_raw
WHERE release != ''
GROUP BY project_id, release;
