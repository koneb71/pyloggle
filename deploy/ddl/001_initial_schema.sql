-- Initial schema for Pyloggle logging and error tracking
-- Run this on first setup

CREATE DATABASE IF NOT EXISTS pyloggle;

USE pyloggle;

-- Raw logs table - append-only, optimized for time-range queries
CREATE TABLE IF NOT EXISTS logs_raw (
    ts DateTime64(3, 'UTC'), -- timestamp with millisecond precision
    service LowCardinality(String),
    env LowCardinality(String),
    level LowCardinality(String), -- error, warn, info, debug
    logger String,
    message String,
    trace_id String, -- for distributed tracing
    span_id String,
    request_id String, -- correlates logs within a request
    http_remote_addr String, -- end-user/client address (may be IP or hostname)
    http_server_addr String, -- app/server address (may be IP or hostname)
    project_id String, -- multi-tenant support
    attrs Map(String, String), -- structured attributes (user_id, http_method, etc.)

    -- ClickHouse optimizations
    INDEX idx_service_env (service, env) TYPE minmax GRANULARITY 1,
    INDEX idx_level (level) TYPE set(10) GRANULARITY 1,
    INDEX idx_ts (ts) TYPE minmax GRANULARITY 1,
    INDEX idx_trace_id (trace_id) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_request_id (request_id) TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (service, env, ts, level)
TTL toDate(ts) + INTERVAL 90 DAY -- keep logs for 90 days by default
SETTINGS index_granularity = 8192;

-- Error occurrences table - normalized error data for issue tracking
CREATE TABLE IF NOT EXISTS error_occurrences (
    ts DateTime64(3, 'UTC'),
    issue_id String, -- computed hash for grouping
    service LowCardinality(String),
    env LowCardinality(String),
    project_id String,

    -- Exception details
    exception_type String,
    exception_message String,
    stack_fingerprint String, -- hash of exception_type + top in-app frames
    title String, -- e.g., "ValueError in handler (main.py:12)"

    -- Stack trace frames (filename, lineno, function, context, is_in_app)
    stack_frames Array(Tuple(
        filename String,
        lineno UInt32,
        function String,
        context String,
        is_in_app UInt8
    )),

    -- Request context
    trace_id String,
    span_id String,
    request_id String,
    http_method LowCardinality(String),
    http_route String,
    http_status UInt16,
    http_user_agent String,
    http_remote_addr String,
    http_server_addr String,

    -- User impact (hashed to avoid PII)
    user_id_hash String, -- hash(user_id) for counting unique users affected

    -- Additional context
    attrs Map(String, String), -- any extra structured data

    -- ClickHouse optimizations
    INDEX idx_issue_id (issue_id) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_service_env (service, env) TYPE minmax GRANULARITY 1,
    INDEX idx_ts (ts) TYPE minmax GRANULARITY 1,
    INDEX idx_stack_fingerprint (stack_fingerprint) TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (service, env, issue_id, ts)
TTL toDate(ts) + INTERVAL 180 DAY -- keep error occurrences longer
SETTINGS index_granularity = 8192;

-- Issues stats table - aggregated view of grouped errors
-- Note: lifecycle status is stored separately in issues_state.
CREATE TABLE IF NOT EXISTS issues_stats (
    issue_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    project_id String,

    -- Issue metadata
    title String, -- e.g., "ValueError in process_payment (payments.py:123)"
    first_seen DateTime64(3, 'UTC'),
    last_seen DateTime64(3, 'UTC'),

    -- Counters
    occurrence_count UInt64,
    affected_users UInt32, -- count of unique user_id_hash

    -- Grouping fingerprint
    stack_fingerprint String,
    exception_type String,
    top_frame_filename String,
    top_frame_lineno UInt32,
    top_frame_function String,

    -- ClickHouse optimizations
    INDEX idx_service_env (service, env) TYPE minmax GRANULARITY 1,
    INDEX idx_last_seen (last_seen) TYPE minmax GRANULARITY 1
)
ENGINE = ReplacingMergeTree(last_seen)
ORDER BY (service, env, issue_id)
-- ClickHouse 24.x expects TTL expressions to yield Date/DateTime (not DateTime64),
-- so cast `last_seen` to DateTime for TTL evaluation.
TTL toDateTime(last_seen) + INTERVAL 365 DAY -- keep issues for 1 year
SETTINGS index_granularity = 8192;

-- Issues state table - lifecycle state (open/resolved/ignored) with regression marker.
-- ReplacingMergeTree lets us "update" by inserting a newer row.
CREATE TABLE IF NOT EXISTS issues_state (
    issue_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    project_id String,
    status LowCardinality(String), -- open, resolved, ignored
    is_regressed UInt8,
    regressed_from LowCardinality(String), -- resolved/ignored/open
    updated_at DateTime64(3, 'UTC'),
    updated_by String, -- optional (user id/email)
    comment String
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (project_id, service, env, issue_id)
SETTINGS index_granularity = 8192;

-- Materialized view to maintain issues_stats table
CREATE MATERIALIZED VIEW IF NOT EXISTS issues_stats_mv
TO issues_stats
AS SELECT
    issue_id,
    service,
    env,
    project_id,
    any(title) as title,
    min(ts) as first_seen,
    max(ts) as last_seen,
    count() as occurrence_count,
    uniq(user_id_hash) as affected_users,
    any(stack_fingerprint) as stack_fingerprint,
    any(exception_type) as exception_type,
    any(arrayFirst(x -> x.5 = 1, stack_frames).1) as top_frame_filename,
    any(arrayFirst(x -> x.5 = 1, stack_frames).2) as top_frame_lineno,
    any(arrayFirst(x -> x.5 = 1, stack_frames).3) as top_frame_function
FROM error_occurrences
GROUP BY service, env, issue_id, project_id;

-- Issues view - user-facing grouped issues with lifecycle status.
-- We keep the underlying tables split (stats + state) but expose a single `issues` object
-- matching the MVP data model in the plan.
CREATE VIEW IF NOT EXISTS issues AS
SELECT
    s.issue_id,
    s.service,
    s.env,
    s.project_id,
    s.title,
    coalesce(st.status, 'open') as status,
    coalesce(st.updated_at, toDateTime64(0, 3, 'UTC')) as status_updated_at,
    s.first_seen,
    s.last_seen,
    s.occurrence_count,
    s.affected_users,
    s.exception_type,
    s.stack_fingerprint,
    s.top_frame_filename,
    s.top_frame_lineno,
    s.top_frame_function
FROM issues_stats s
LEFT JOIN (
    SELECT
        project_id, service, env, issue_id,
        argMax(status, ist.updated_at) as status,
        max(ist.updated_at) as updated_at
    FROM issues_state AS ist
    GROUP BY project_id, service, env, issue_id
) st
ON st.project_id = s.project_id
AND st.service = s.service
AND st.env = s.env
AND st.issue_id = s.issue_id;

-- Alert rules table
CREATE TABLE IF NOT EXISTS alert_rules (
    id String,
    project_id String,
    name String,
    description String,

    -- Alert conditions
    service LowCardinality(String),
    env LowCardinality(String),
    condition_type LowCardinality(String), -- error_rate, issue_count, regression
    threshold Float64,
    time_window_seconds UInt32,

    -- Notification settings
    enabled UInt8,
    notification_channels Array(String), -- email, webhook
    notification_emails Array(String),
    webhook_urls Array(String),

    -- Metadata
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC'),

    INDEX idx_project (project_id) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_enabled (enabled) TYPE minmax GRANULARITY 1
)
ENGINE = MergeTree()
ORDER BY (project_id, id)
SETTINGS index_granularity = 8192;

-- Saved searches table
CREATE TABLE IF NOT EXISTS saved_searches (
    id String,
    project_id String,
    user_id String, -- who created it
    name String,
    description String,
    query String, -- the search query/filter
    is_public UInt8, -- can others see it?

    -- Metadata
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC'),
    last_used_at DateTime64(3, 'UTC'),

    INDEX idx_project (project_id) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_user (user_id) TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree()
ORDER BY (project_id, user_id, id)
SETTINGS index_granularity = 8192;

-- Projects table for multi-tenancy
CREATE TABLE IF NOT EXISTS projects (
    id String,
    name String,
    description String,

    -- Quotas and limits
    max_events_per_second UInt32,
    max_storage_gb UInt32,

    -- API keys
    api_key_hash String, -- hash of the actual API key
    api_key_created_at DateTime64(3, 'UTC'),

    -- Metadata
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC'),

    INDEX idx_api_key (api_key_hash) TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree()
ORDER BY id
SETTINGS index_granularity = 8192;
