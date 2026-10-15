package clickhouse

import (
	"context"
	"fmt"

	"github.com/koneb71/pyloggle/internal/model"
)

// Request counts. Every request is counted once, through one door: request_counts_in, a Null
// table that keeps nothing and only feeds the views below. The Python package sends a count per
// endpoint and minute (POST /v1/ingest/requests); ingest turns request lines, crashes and
// OpenTelemetry server spans into counts of one. Listing a request on Logs is a separate choice.
//
// A count carries its response times and its people as arrays. arrayReduce turns them into the
// same aggregate states that per-row inserts produce, and -MergeState folds those per minute, so
// request_stats_minutely and request_usage_hourly cannot tell a count of 37 from 37 lines.

const requestCountsInSQL = `CREATE TABLE IF NOT EXISTS request_counts_in (
    project_id String,
    minute DateTime('UTC'),
    service LowCardinality(String),
    env LowCardinality(String),
    release LowCardinality(String),
    http_method LowCardinality(String),
    http_route String,
    requests UInt32,
    errors UInt32,
    durations_ms Array(Float32),
    users Array(String)
)
ENGINE = Null`

// request_usage_hourly answers Analytics → Usage and the request charts that need a version or
// people: traffic, failures, response times and how many people, per endpoint, version and hour.
// People are an estimate (uniq), never a list of who they were.
const requestUsageHourlySQL = `CREATE TABLE IF NOT EXISTS request_usage_hourly (
    project_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    release LowCardinality(String),
    http_method LowCardinality(String),
    http_route String,
    hour DateTime('UTC'),
    requests SimpleAggregateFunction(sum, UInt64),
    errors SimpleAggregateFunction(sum, UInt64),
    latency AggregateFunction(quantilesTDigestIf(0.5, 0.95, 0.99), Float32, UInt8),
    users AggregateFunction(uniqIf, String, UInt8)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, service, env, release, http_method, http_route, hour)
TTL hour + INTERVAL %d DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1`

const (
	latencyFromCounts = `quantilesTDigestIfMergeState(0.5, 0.95, 0.99)(arrayReduce('quantilesTDigestIfState(0.5, 0.95, 0.99)', durations_ms, arrayMap(d -> toUInt8(d > 0), durations_ms)))`
	usersFromCounts   = `uniqIfMergeState(arrayReduce('uniqIfState', users, arrayMap(u -> toUInt8(u != ''), users)))`
)

var requestCountViews = []string{
	`CREATE MATERIALIZED VIEW IF NOT EXISTS request_counts_stats_mv TO request_stats_minutely AS
SELECT project_id, service, env, http_method, http_route, minute AS bucket,
    sum(requests) AS requests, sum(errors) AS errors, ` + latencyFromCounts + ` AS latency
FROM request_counts_in
GROUP BY project_id, service, env, http_method, http_route, bucket`,
	`CREATE MATERIALIZED VIEW IF NOT EXISTS request_counts_usage_mv TO request_usage_hourly AS
SELECT project_id, service, env, release, http_method, http_route, toStartOfHour(minute) AS hour,
    sum(requests) AS requests, sum(errors) AS errors, ` + latencyFromCounts + ` AS latency, ` + usersFromCounts + ` AS users
FROM request_counts_in
GROUP BY project_id, service, env, release, http_method, http_route, hour`,
	// A release seen only through its traffic is still known (resolve-in-release relies on it).
	`CREATE MATERIALIZED VIEW IF NOT EXISTS releases_requests_mv TO releases AS
SELECT project_id, release, toDateTime64(min(minute), 3, 'UTC') AS first_seen
FROM request_counts_in
WHERE release != ''
GROUP BY project_id, release`,
}

// requestRowsSQL selects the request rows of logs_raw: the same rule as request_stats_minutely's
// first view and model.CountOf.
const requestRowsSQL = `http_method != '' AND http_route != ''
  AND (duration_ms > 0 OR (exception_type != '' AND http_status >= 500))`

// createRequestCounts is migration 018. The funnel and its views come first; then the old view
// that counted request rows of logs_raw goes (ingest counts them now, before sampling); last,
// request_usage_hourly is filled from the request rows still kept, once.
func createRequestCounts(ctx context.Context, c *Client, opts MigrateOptions) error {
	opts = opts.withDefaults()
	stmts := []string{requestCountsInSQL, fmt.Sprintf(requestUsageHourlySQL, opts.RetentionStatsDays)}
	stmts = append(stmts, requestCountViews...)
	stmts = append(stmts, "DROP VIEW IF EXISTS request_stats_minutely_mv")
	for _, stmt := range stmts {
		if err := c.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%.60s: %w", stmt, err)
		}
	}
	var n uint64
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM request_usage_hourly").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return c.Exec(ctx, `INSERT INTO request_usage_hourly
SELECT project_id, service, env, release, http_method, http_route, toStartOfHour(ts) AS hour,
    count() AS requests, countIf(http_status >= 500) AS errors,
    quantilesTDigestIfState(0.5, 0.95, 0.99)(toFloat32(duration_ms), toUInt8(duration_ms > 0)) AS latency,
    uniqIfState(user_id_hash, toUInt8(user_id_hash != '')) AS users
FROM logs_raw
WHERE `+requestRowsSQL+`
GROUP BY project_id, service, env, release, http_method, http_route, hour`)
}

// InsertRequestCounts queues counts for request_counts_in. Counts are small and never block
// ingest: when the queue is full they are dropped (and counted in the queue's stats).
func (c *Client) InsertRequestCounts(ctx context.Context, counts []model.RequestCount) error {
	for _, rc := range counts {
		if err := c.counts.enqueue(ctx, rc, 0); err != nil && err != errQueueFull {
			return err
		}
	}
	return nil
}

// EnqueueRequestCounts makes *Client usable as an ingest sink.
func (c *Client) EnqueueRequestCounts(ctx context.Context, counts []model.RequestCount) error {
	return c.InsertRequestCounts(ctx, counts)
}

func (c *Client) flushRequestCounts(ctx context.Context, counts []model.RequestCount) error {
	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO request_counts_in (
			project_id, minute, service, env, release, http_method, http_route,
			requests, errors, durations_ms, users
		)
	`)
	if err != nil {
		return err
	}
	for i := range counts {
		rc := &counts[i]
		durations, users := rc.DurationsMS, rc.Users
		if durations == nil {
			durations = []float32{}
		}
		if users == nil {
			users = []string{}
		}
		if err := batch.Append(
			rc.ProjectID, rc.Minute.UTC(), rc.Service, rc.Env, rc.Release, rc.Method, rc.Route,
			rc.Requests, rc.Errors, durations, users,
		); err != nil {
			_ = batch.Abort()
			return err
		}
	}
	return batch.Send()
}
