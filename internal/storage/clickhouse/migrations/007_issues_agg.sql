-- Issue aggregates v2.
-- v1 kept issue stats in a ReplacingMergeTree fed by a per-insert GROUP BY, so counts only ever
-- reflected the most recent batch. v2 uses AggregatingMergeTree state columns (merged at read
-- time), plus hourly per-issue and minutely per-level count tables for sparklines and dashboards.

CREATE TABLE IF NOT EXISTS issues_agg (
    project_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    issue_id String,
    first_seen SimpleAggregateFunction(min, DateTime64(3, 'UTC')),
    last_seen SimpleAggregateFunction(max, DateTime64(3, 'UTC')),
    occurrence_count SimpleAggregateFunction(sum, UInt64),
    affected_users AggregateFunction(uniqIf, String, UInt8),
    title AggregateFunction(argMax, String, DateTime64(3, 'UTC')),
    culprit AggregateFunction(argMax, String, DateTime64(3, 'UTC')),
    last_message AggregateFunction(argMax, String, DateTime64(3, 'UTC')),
    last_release AggregateFunction(argMax, String, DateTime64(3, 'UTC')),
    exception_type SimpleAggregateFunction(anyLast, String),
    exception_module SimpleAggregateFunction(anyLast, String),
    level SimpleAggregateFunction(anyLast, String),
    stack_fingerprint SimpleAggregateFunction(anyLast, String)
)
ENGINE = AggregatingMergeTree()
ORDER BY (project_id, service, env, issue_id)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS issue_counts_hourly (
    project_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    issue_id String,
    bucket DateTime('UTC'),
    cnt SimpleAggregateFunction(sum, UInt64),
    users AggregateFunction(uniqIf, String, UInt8)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(bucket)
ORDER BY (project_id, service, env, issue_id, bucket)
TTL bucket + INTERVAL ${RETENTION_STATS_DAYS} DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

CREATE TABLE IF NOT EXISTS log_counts_minutely (
    project_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    level LowCardinality(String),
    bucket DateTime('UTC'),
    cnt UInt64
)
ENGINE = SummingMergeTree(cnt)
PARTITION BY toYYYYMM(bucket)
ORDER BY (project_id, service, env, level, bucket)
TTL bucket + INTERVAL 90 DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

ALTER TABLE issues_state ADD COLUMN IF NOT EXISTS assignee String;

-- Stop feeding the v1 stats table (it is dropped by a later migration once nothing reads it).
DROP VIEW IF EXISTS issues_stats_mv;
DROP VIEW IF EXISTS issues;

-- One-time backfills, guarded so a re-run is a no-op. They run BEFORE the views are created so
-- rows are not counted twice.
INSERT INTO issues_agg
SELECT
    project_id, service, env, issue_id,
    min(ts), max(ts), count(),
    uniqIfState(user_id_hash, user_id_hash != ''),
    argMaxState(title, ts), argMaxState(culprit, ts), argMaxState(exception_message, ts), argMaxState(release, ts),
    anyLast(exception_type), anyLast(exception_module), anyLast(level), anyLast(stack_fingerprint)
FROM error_occurrences
WHERE (SELECT count() FROM issues_agg) = 0
GROUP BY project_id, service, env, issue_id;

INSERT INTO issue_counts_hourly
SELECT
    project_id, service, env, issue_id,
    toDateTime(toStartOfHour(ts), 'UTC'),
    count(),
    uniqIfState(user_id_hash, user_id_hash != '')
FROM error_occurrences
WHERE (SELECT count() FROM issue_counts_hourly) = 0
GROUP BY project_id, service, env, issue_id, toDateTime(toStartOfHour(ts), 'UTC');

INSERT INTO log_counts_minutely
SELECT
    project_id, service, env,
    multiIf(level = 'warning', 'warn', level = 'critical', 'fatal', level),
    toDateTime(toStartOfMinute(ts), 'UTC'),
    count()
FROM logs_raw
WHERE (SELECT count() FROM log_counts_minutely) = 0
GROUP BY project_id, service, env, multiIf(level = 'warning', 'warn', level = 'critical', 'fatal', level), toDateTime(toStartOfMinute(ts), 'UTC');

CREATE MATERIALIZED VIEW IF NOT EXISTS issues_agg_mv TO issues_agg AS
SELECT
    project_id, service, env, issue_id,
    min(ts) AS first_seen,
    max(ts) AS last_seen,
    count() AS occurrence_count,
    uniqIfState(user_id_hash, user_id_hash != '') AS affected_users,
    argMaxState(title, ts) AS title,
    argMaxState(culprit, ts) AS culprit,
    argMaxState(exception_message, ts) AS last_message,
    argMaxState(release, ts) AS last_release,
    anyLast(exception_type) AS exception_type,
    anyLast(exception_module) AS exception_module,
    anyLast(level) AS level,
    anyLast(stack_fingerprint) AS stack_fingerprint
FROM error_occurrences
GROUP BY project_id, service, env, issue_id;

CREATE MATERIALIZED VIEW IF NOT EXISTS issue_counts_hourly_mv TO issue_counts_hourly AS
SELECT
    project_id, service, env, issue_id,
    toDateTime(toStartOfHour(ts), 'UTC') AS bucket,
    count() AS cnt,
    uniqIfState(user_id_hash, user_id_hash != '') AS users
FROM error_occurrences
GROUP BY project_id, service, env, issue_id, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS log_counts_minutely_mv TO log_counts_minutely AS
SELECT
    project_id, service, env, level,
    toDateTime(toStartOfMinute(ts), 'UTC') AS bucket,
    count() AS cnt
FROM logs_raw
GROUP BY project_id, service, env, level, bucket;

-- Read-side convenience view with merged states (the Go code inlines the same query).
CREATE VIEW IF NOT EXISTS issues AS
SELECT
    project_id, service, env, issue_id,
    argMaxMerge(title) AS title,
    argMaxMerge(culprit) AS culprit,
    argMaxMerge(last_message) AS last_message,
    argMaxMerge(last_release) AS last_release,
    min(first_seen) AS first_seen,
    max(last_seen) AS last_seen,
    sum(occurrence_count) AS occurrence_count,
    uniqIfMerge(affected_users) AS affected_users,
    anyLast(exception_type) AS exception_type,
    anyLast(exception_module) AS exception_module,
    anyLast(level) AS level,
    anyLast(stack_fingerprint) AS stack_fingerprint
FROM issues_agg
GROUP BY project_id, service, env, issue_id;
