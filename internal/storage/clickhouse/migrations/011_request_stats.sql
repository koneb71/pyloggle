-- Per-minute request statistics, fed from the request rows the SDKs and OTLP server spans
-- produce (a route, a method and a latency), plus the unhandled-exception rows of requests
-- that never reached the after-request hook (status 500, no latency sample).
CREATE TABLE IF NOT EXISTS request_stats_minutely (
    project_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    http_method LowCardinality(String),
    http_route String,
    bucket DateTime('UTC'),
    requests SimpleAggregateFunction(sum, UInt64),
    errors SimpleAggregateFunction(sum, UInt64),
    latency AggregateFunction(quantilesTDigestIf(0.5, 0.95, 0.99), Float32, UInt8)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(bucket)
ORDER BY (project_id, service, env, http_method, http_route, bucket)
TTL bucket + INTERVAL ${RETENTION_STATS_DAYS} DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- One-time backfill, guarded so a re-run is a no-op; runs before the view exists.
INSERT INTO request_stats_minutely
SELECT
    project_id, service, env, http_method, http_route,
    toDateTime(toStartOfMinute(ts), 'UTC') AS bucket,
    count() AS requests,
    countIf(http_status >= 500) AS errors,
    quantilesTDigestIfState(0.5, 0.95, 0.99)(duration_ms, duration_ms > 0) AS latency
FROM logs_raw
WHERE http_method != '' AND http_route != ''
  AND (duration_ms > 0 OR (exception_type != '' AND http_status >= 500))
  AND (SELECT count() FROM request_stats_minutely) = 0
GROUP BY project_id, service, env, http_method, http_route, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS request_stats_minutely_mv TO request_stats_minutely AS
SELECT
    project_id, service, env, http_method, http_route,
    toDateTime(toStartOfMinute(ts), 'UTC') AS bucket,
    count() AS requests,
    countIf(http_status >= 500) AS errors,
    quantilesTDigestIfState(0.5, 0.95, 0.99)(duration_ms, duration_ms > 0) AS latency
FROM logs_raw
WHERE http_method != '' AND http_route != ''
  AND (duration_ms > 0 OR (exception_type != '' AND http_status >= 500))
GROUP BY project_id, service, env, http_method, http_route, bucket;
