//go:build integration

package clickhouse_test

// Request counts: one door (request_counts_in) feeding the request statistics, the hourly usage
// table and releases, exactly once per request; and migration 018 on an existing install.

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/ingest"
	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

type requestTotals struct {
	requests, errors, people uint64
	p50                      float64
}

func usageTotals(t *testing.T, c *clickhouse.Client, project, route string) requestTotals {
	t.Helper()
	var r requestTotals
	var qs []float32
	err := c.QueryRow(context.Background(), `SELECT sum(requests), sum(errors), uniqIfMerge(users), quantilesTDigestIfMerge(0.5, 0.95, 0.99)(latency)
		FROM request_usage_hourly WHERE project_id = ? AND http_route = ?`, project, route).Scan(&r.requests, &r.errors, &r.people, &qs)
	if err != nil {
		t.Fatalf("usage totals: %v", err)
	}
	if len(qs) > 0 && !math.IsNaN(float64(qs[0])) {
		r.p50 = float64(qs[0])
	}
	return r
}

func statsRequests(t *testing.T, c *clickhouse.Client, project, route string) (uint64, uint64) {
	t.Helper()
	var n, e uint64
	if err := c.QueryRow(context.Background(), "SELECT sum(requests), sum(errors) FROM request_stats_minutely WHERE project_id = ? AND http_route = ?", project, route).Scan(&n, &e); err != nil {
		t.Fatalf("stats: %v", err)
	}
	return n, e
}

func TestIntegrationRequestCounts(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.SampleDebug, cfg.SampleWarn = 0, 1, 1 // request lines are sampled away
	cfg.RateLimitRPS = 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	tn := ingest.Tenant{ProjectID: "p1"}
	minute := time.Now().UTC().Truncate(time.Hour).Add(5 * time.Minute)

	// The Python package's counts: one endpoint, two minutes, 1.4.0 seen only through traffic.
	counts := []model.RequestCount{
		{Minute: model.At(minute), Service: "shop", Env: "prod", Release: "1.4.0", Method: "POST", Route: "/orders", Requests: 3, Errors: 1, DurationsMS: []float32{10, 20, 30}, Users: []string{"u1", "u2"}},
		{Minute: model.At(minute.Add(time.Minute)), Service: "shop", Env: "prod", Release: "1.4.0", Method: "POST", Route: "/orders", Requests: 2, DurationsMS: []float32{40, 50}, Users: []string{"u2", "u3"}},
	}
	if n, bad, o := p.ProcessRequestCounts(ctx, tn, counts, 200); n != 2 || len(bad) != 0 || o != ingest.Accepted {
		t.Fatalf("counts: %d %v %v", n, bad, o)
	}
	// A request line (dropped by sampling, still counted) and an unlisted OpenTelemetry request.
	line := model.Event{Timestamp: model.At(minute), Service: "shop", Env: "prod", Release: "1.4.0", Level: "info", Message: "request", DurationMS: 60,
		HTTP: &model.HTTPInfo{Method: "POST", Route: "/orders", Status: 201}, User: &model.UserInfo{IDHash: "u4"}}
	if o := p.Process(ctx, tn, line, 100); o != ingest.DroppedSampled {
		t.Fatalf("line: %v", o)
	}
	span := line
	span.CountOnly, span.User, span.DurationMS = true, nil, 70
	if o := p.Process(ctx, tn, span, 100); o != ingest.Accepted {
		t.Fatalf("span: %v", o)
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	got := usageTotals(t, c, "p1", "/orders")
	if got.requests != 7 || got.errors != 1 || got.people != 4 || got.p50 < 39 || got.p50 > 41 {
		t.Fatalf("usage: %+v (want 7 requests, 1 failed, 4 people, median 40 ms)", got)
	}
	if n, e := statsRequests(t, c, "p1", "/orders"); n != 7 || e != 1 {
		t.Fatalf("request stats: %d requests %d failed", n, e)
	}
	stats, err := c.RequestStats(ctx, "p1", "", "", minute.Add(-time.Hour), minute.Add(time.Hour), 3600, 10)
	if err != nil || stats.Totals.Requests != 7 || stats.Totals.P50MS < 39 || stats.Totals.P50MS > 41 {
		t.Fatalf("dashboard request stats: %+v %v", stats.Totals, err)
	}
	var logged uint64
	if err := c.QueryRow(ctx, "SELECT count() FROM logs_raw WHERE project_id = 'p1'").Scan(&logged); err != nil || logged != 0 {
		t.Fatalf("counting lists nothing on Logs: %d %v", logged, err)
	}
	if fs, ok, err := c.ReleaseFirstSeen(ctx, "p1", "1.4.0"); err != nil || !ok || !fs.Equal(minute) {
		t.Fatalf("a release seen through traffic is known: %v %v %v", fs, ok, err)
	}
	if n, _ := statsRequests(t, c, "p2", "/orders"); n != 0 {
		t.Fatal("counts stay in their project")
	}
}

// Installs before 018 counted request rows of logs_raw with a view. 018 swaps that view for the
// door, fills the hourly table from the request rows still kept, and nothing is counted twice.
func TestIntegrationRequestCountsUpgrade(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	for _, stmt := range []string{
		"DROP VIEW request_counts_stats_mv",
		"DROP VIEW request_counts_usage_mv",
		"DROP VIEW releases_requests_mv",
		"DROP TABLE request_counts_in",
		"DROP TABLE request_usage_hourly",
		`CREATE MATERIALIZED VIEW request_stats_minutely_mv TO request_stats_minutely AS
		SELECT project_id, service, env, http_method, http_route, toDateTime(toStartOfMinute(ts), 'UTC') AS bucket,
			count() AS requests, countIf(http_status >= 500) AS errors,
			quantilesTDigestIfState(0.5, 0.95, 0.99)(duration_ms, duration_ms > 0) AS latency
		FROM logs_raw
		WHERE http_method != '' AND http_route != '' AND (duration_ms > 0 OR (exception_type != '' AND http_status >= 500))
		GROUP BY project_id, service, env, http_method, http_route, bucket`,
		"DELETE FROM schema_migrations WHERE name = '018_request_counts'",
	} {
		if err := c.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// Request lines from before the upgrade: in logs_raw and, through the old view, in the stats.
	now := time.Now().UTC().Truncate(time.Minute)
	for i, user := range []string{"a", "b", "a"} {
		ev := model.Event{EventID: "old-" + user + string(rune('0'+i)), Timestamp: model.At(now.Add(-10 * time.Minute)), ProjectID: "p1", Service: "shop", Env: "prod", Release: "1.3.0",
			Level: "info", Message: "request", DurationMS: 25, HTTP: &model.HTTPInfo{Method: "GET", Route: "/cart", Status: 200}, User: &model.UserInfo{IDHash: user}}
		if err := c.InsertLog(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := statsRequests(t, c, "p1", "/cart"); n != 3 {
		t.Fatalf("before: %d", n)
	}

	opts := clickhouse.MigrateOptions{Logf: func(string, ...any) {}}
	applied, err := c.Migrate(ctx, opts)
	if err != nil || len(applied) != 1 || applied[0] != "018_request_counts" {
		t.Fatalf("upgrade: %v %v", applied, err)
	}
	if got := usageTotals(t, c, "p1", "/cart"); got.requests != 3 || got.people != 2 {
		t.Fatalf("backfill: %+v", got)
	}
	var views uint64
	if err := c.QueryRow(ctx, "SELECT count() FROM system.tables WHERE database = currentDatabase() AND name = 'request_stats_minutely_mv'").Scan(&views); err != nil || views != 0 {
		t.Fatalf("the old view must be gone: %d %v", views, err)
	}

	// After the upgrade a request line is counted once, by ingest.
	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.RateLimitRPS = 1, 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	line := model.Event{Timestamp: model.At(now), Service: "shop", Env: "prod", Level: "info", Message: "request", DurationMS: 30,
		HTTP: &model.HTTPInfo{Method: "GET", Route: "/cart", Status: 200}}
	if o := p.Process(ctx, ingest.Tenant{ProjectID: "p1"}, line, 100); o != ingest.Accepted {
		t.Fatal(o)
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := statsRequests(t, c, "p1", "/cart"); n != 4 {
		t.Fatalf("after: %d requests (want 4: nothing counted twice)", n)
	}
	if applied, err := c.Migrate(ctx, opts); err != nil || len(applied) != 0 {
		t.Fatalf("second run must be a no-op: %v %v", applied, err)
	}
	if got := usageTotals(t, c, "p1", "/cart"); got.requests != 4 {
		t.Fatalf("the backfill runs once: %+v", got)
	}
}
