//go:build integration

package clickhouse_test

// Request bodies on request lines, the 017 upgrade, and clearing a project's messages.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/ingest"
	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func TestIntegrationRequestBodiesAndClearLogs(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.SampleDebug, cfg.SampleWarn = 1, 1, 1
	cfg.RateLimitRPS = 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	now := time.Now().UTC().Truncate(time.Second)

	count := func(table, project string) uint64 {
		t.Helper()
		var n uint64
		if err := c.QueryRow(ctx, "SELECT count() FROM "+table+" WHERE project_id = ?", project).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	for _, id := range []string{"p1", "p2"} {
		if _, _, err := c.CreateProject(ctx, clickhouse.ProjectUpsert{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
		tn := ingest.Tenant{ProjectID: id}
		for i := 0; i < 3; i++ {
			ev := model.Event{
				EventID: id + "-req-" + string(rune('a'+i)), Timestamp: model.At(now.Add(-time.Duration(i) * time.Minute)),
				Service: "shop", Env: "prod", Level: "info", Logger: "pyloggle.flask", Message: "request", DurationMS: 12,
				HTTP: &model.HTTPInfo{Method: "POST", Route: "/orders", Status: 201, Body: `{"item":3,"password":"[REDACTED]"}`},
			}
			if o := p.Process(ctx, tn, ev, 100); o != ingest.Accepted {
				t.Fatalf("request line: %v", o)
			}
		}
		boom := model.Event{
			Timestamp: model.At(now), Service: "shop", Env: "prod", Level: "error", Logger: "pyloggle.flask", Message: "boom",
			HTTP:      &model.HTTPInfo{Method: "POST", Route: "/orders", Status: 500, Body: `{"item":4}`},
			Exception: &model.Exception{Type: "ValueError", Message: "boom", Frames: []model.Frame{{Filename: "a.py", Function: "f", Lineno: 1}}},
		}
		if o := p.Process(ctx, tn, boom, 100); o != ingest.Accepted {
			t.Fatalf("error: %v", o)
		}
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// One message comes back with its body; lists stay light.
	ev, ok, err := c.GetLog(ctx, "p1", "p1-req-a")
	if err != nil || !ok || ev.HTTPBody != `{"item":3,"password":"[REDACTED]"}` || ev.HTTPMethod != "POST" {
		t.Fatalf("get log: %+v %v %v", ev, ok, err)
	}
	if _, ok, _ := c.GetLog(ctx, "p2", "p1-req-a"); ok {
		t.Fatal("a message is only found in its own project")
	}
	list, err := c.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: "p1", From: now.Add(-time.Hour), To: now.Add(time.Minute), Limit: 10})
	if err != nil || len(list) != 4 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	for _, l := range list {
		if l.HTTPBody != "" {
			t.Fatalf("lists must not carry bodies: %+v", l)
		}
	}
	for _, table := range []string{"log_counts_minutely", "request_stats_minutely"} {
		if count(table, "p1") == 0 {
			t.Fatalf("%s has nothing for p1 before clearing", table)
		}
	}

	// Clearing p1's messages hides them at once, with the counts built from them.
	if err := c.ClearLogs(ctx, "p1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for _, table := range []string{"logs_raw", "log_counts_minutely", "request_stats_minutely"} {
		if n := count(table, "p1"); n != 0 {
			t.Fatalf("%s still holds %d p1 rows", table, n)
		}
		if count(table, "p2") == 0 {
			t.Fatalf("clearing p1 must leave p2's %s alone", table)
		}
	}
	// Issues and their occurrences (with the body) stay.
	for _, table := range []string{"error_occurrences", "issues_agg"} {
		if count(table, "p1") == 0 {
			t.Fatalf("clearing messages must keep %s", table)
		}
	}
	if err := c.RecomputeUsage(ctx, "p1"); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	usage, err := c.ProjectUsage(ctx, "p1", 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range usage {
		if d.Source == "logs" && d.Rows != 0 {
			t.Fatalf("usage still counts cleared messages: %+v", d)
		}
	}
	if err := c.ClearLogs(ctx, ""); err == nil {
		t.Fatal("clearing needs a project")
	}
}

// Installs from before 1.0 have logs_raw without http_body: 017 adds it, once.
func TestIntegrationLogsBodyUpgrade(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	// The state before 1.0: no column, and a usage view that does not read it.
	for _, stmt := range []string{
		"DROP VIEW project_usage_logs_mv",
		"CREATE MATERIALIZED VIEW project_usage_logs_mv TO project_usage_daily AS SELECT project_id, toDate(ts) AS day, 'logs' AS source, count() AS rows, sum(96 + length(message)) AS bytes_est FROM logs_raw GROUP BY project_id, day",
		"ALTER TABLE logs_raw DROP COLUMN http_body",
	} {
		if err := c.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Exec(ctx, "DELETE FROM schema_migrations WHERE name = '017_logs_http_body'"); err != nil {
		t.Fatal(err)
	}
	opts := clickhouse.MigrateOptions{Logf: func(string, ...any) {}}
	applied, err := c.Migrate(ctx, opts)
	if err != nil || len(applied) != 1 || applied[0] != "017_logs_http_body" {
		t.Fatalf("upgrade: %v %v", applied, err)
	}
	var n uint64
	if err := c.QueryRow(ctx, "SELECT count() FROM system.columns WHERE database = currentDatabase() AND table = 'logs_raw' AND name = 'http_body'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("http_body column: %d %v", n, err)
	}
	var view string
	if err := c.QueryRow(ctx, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = 'project_usage_logs_mv'").Scan(&view); err != nil {
		t.Fatalf("usage view: %v", err)
	}
	if !strings.Contains(view, "length(http_body)") {
		t.Fatalf("usage view must count bodies: %s", view)
	}
	if applied, err := c.Migrate(ctx, opts); err != nil || len(applied) != 0 {
		t.Fatalf("second run must be a no-op: %v %v", applied, err)
	}
}
