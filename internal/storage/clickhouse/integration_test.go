//go:build integration

package clickhouse

// Integration tests against a real ClickHouse. Run with:
//
//	PYLOGGLE_TEST_CLICKHOUSE_DSN='clickhouse://pyloggle:pyloggle@localhost:9000/default' \
//	  go test -tags integration ./internal/storage/clickhouse/ -run Integration -v
//
// Each test creates its own database and drops it afterwards.

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"pyloggle/internal/api"
	"strings"
	"testing"
	"time"
)

func integrationClient(t *testing.T) (*Client, func()) {
	t.Helper()
	dsn := os.Getenv("PYLOGGLE_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("PYLOGGLE_TEST_CLICKHOUSE_DSN not set")
	}
	ctx := context.Background()
	admin, err := New(dsn, 100, 10, time.Second)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := fmt.Sprintf("pyloggle_test_%d", rand.Int63())
	if err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
		t.Fatalf("create db: %v", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + db
	c, err := New(u.String(), 100, 10, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("connect %s: %v", db, err)
	}
	cleanup := func() {
		_ = c.Close()
		_ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+db)
		_ = admin.Close()
	}
	return c, cleanup
}

// applyV1Schema reproduces a deployment created by the old docker initdb DDL (001-005 only).
func applyV1Schema(t *testing.T, c *Client) {
	t.Helper()
	ctx := context.Background()
	for _, f := range []string{"001_initial_schema.sql", "002_alerting.sql", "003_alerting_telegram.sql", "004_saved_searches_pins.sql", "005_ips.sql"} {
		raw, err := migrationsFS.ReadFile("migrations/" + f)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range splitStatements(string(raw)) {
			if strings.HasPrefix(strings.ToUpper(stmt), "CREATE DATABASE") {
				continue
			}
			if err := c.Exec(ctx, stmt); err != nil {
				t.Fatalf("v1 %s: %v\n%s", f, err, firstLine(stmt))
			}
		}
	}
}

func count(t *testing.T, c *Client, q string, args ...any) uint64 {
	t.Helper()
	var n uint64
	if err := c.conn.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func engineFull(t *testing.T, c *Client, table string) string {
	t.Helper()
	var s string
	if err := c.conn.QueryRow(context.Background(), "SELECT engine_full FROM system.tables WHERE database = currentDatabase() AND name = ?", table).Scan(&s); err != nil {
		t.Fatalf("engine_full %s: %v", table, err)
	}
	return s
}

func seedV1(t *testing.T, c *Client, now time.Time) {
	t.Helper()
	ctx := context.Background()
	old := now.Add(-45 * 24 * time.Hour)
	mid := now.Add(-10 * 24 * time.Hour)
	logs := []struct {
		ts    time.Time
		level string
		msg   string
	}{
		{old, "warning", "ancient warning"},
		{mid, "info", "ten days ago"},
		{now, "critical", "right now"},
	}
	for _, l := range logs {
		if err := c.conn.Exec(ctx, "INSERT INTO logs_raw (ts, service, env, level, logger, message, project_id, attrs) VALUES (?, 'svc', 'dev', ?, 'app', ?, 'p', ?)",
			l.ts, l.level, l.msg, map[string]string{"k": "v"}); err != nil {
			t.Fatalf("seed logs: %v", err)
		}
	}
	occ := []struct {
		ts    time.Time
		issue string
		user  string
	}{
		{old, "issueA", "u1"},
		{now, "issueA", "u2"},
		{now.Add(-40 * 24 * time.Hour), "issueB", "u1"},
	}
	for _, o := range occ {
		if err := c.conn.Exec(ctx, `INSERT INTO error_occurrences (ts, issue_id, service, env, project_id, exception_type, exception_message, stack_fingerprint, title, stack_frames, trace_id, span_id, request_id, http_method, http_route, http_status, http_user_agent, http_remote_addr, http_server_addr, user_id_hash, attrs)
			VALUES (?, ?, 'svc', 'dev', 'p', 'ValueError', 'boom', 'fp', 'ValueError in x', [], '', '', '', 'GET', '/x', 500, '', '', '', ?, ?)`,
			o.ts, o.issue, o.user, map[string]string{}); err != nil {
			t.Fatalf("seed occurrences: %v", err)
		}
	}
}

func TestIntegrationUpgradeFromV1Rebuild(t *testing.T) {
	c, cleanup := integrationClient(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	applyV1Schema(t, c)
	seedV1(t, c, now)

	opts := MigrateOptions{Rebuild: true, RetentionLogsDays: 30, RetentionErrorsDays: 30, Logf: t.Logf}
	applied, err := c.Migrate(ctx, opts)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(applied) != len(migrations) {
		t.Fatalf("expected all %d migrations applied on an initdb deployment, got %v", len(migrations), applied)
	}

	// Only the retention window survived the rebuild, levels were normalized, partitions are daily.
	if n := count(t, c, "SELECT count() FROM logs_raw"); n != 2 {
		t.Fatalf("logs_raw rows after rebuild: %d (want 2: the 45-day-old row is gone)", n)
	}
	if n := count(t, c, "SELECT count() FROM logs_raw WHERE level = 'fatal'"); n != 1 {
		t.Fatalf("critical must be stored as fatal after the copy")
	}
	ef := engineFull(t, c, "logs_raw")
	for _, want := range []string{"PARTITION BY toDate(ts)", "toIntervalDay(30)", "ttl_only_drop_parts = 1", "ORDER BY (project_id, service, env, ts)"} {
		if !strings.Contains(ef, want) {
			t.Fatalf("logs_raw engine missing %q: %s", want, ef)
		}
	}
	cols, _ := c.tableColumns(ctx, "error_occurrences")
	for _, col := range []string{"event_id", "frames_json", "breadcrumbs_json", "http_headers", "culprit", "fingerprint_version"} {
		if !cols[col] {
			t.Fatalf("error_occurrences lacks v2 column %s", col)
		}
	}

	// Aggregates were backfilled from the copied window: issueA has one recent occurrence, issueB expired.
	if n := count(t, c, "SELECT occurrence_count FROM issues WHERE issue_id = 'issueA'"); n != 1 {
		t.Fatalf("issueA occurrence_count after backfill: %d want 1", n)
	}
	if n := count(t, c, "SELECT affected_users FROM issues WHERE issue_id = 'issueA'"); n != 1 {
		t.Fatalf("issueA affected_users: %d want 1", n)
	}
	if n := count(t, c, "SELECT count() FROM issues WHERE issue_id = 'issueB'"); n != 0 {
		t.Fatalf("issueB should have expired with the rebuild")
	}

	// The MV keeps the aggregates correct for new rows (lifetime totals, not last-batch totals).
	for i, user := range []string{"u2", "u3", ""} {
		if err := c.conn.Exec(ctx, `INSERT INTO error_occurrences (ts, event_id, issue_id, service, env, project_id, level, exception_type, exception_message, stack_fingerprint, title, culprit, release, user_id_hash)
			VALUES (?, ?, 'issueA', 'svc', 'dev', 'p', 'error', 'ValueError', 'boom again', 'fp', 'ValueError: boom again', 'app.x', 'v2', ?)`,
			now.Add(time.Duration(i+1)*time.Second), fmt.Sprintf("ev%d", i), user); err != nil {
			t.Fatalf("insert v2 occurrence: %v", err)
		}
	}
	if n := count(t, c, "SELECT occurrence_count FROM issues WHERE issue_id = 'issueA'"); n != 4 {
		t.Fatalf("issueA occurrence_count after inserts: %d want 4", n)
	}
	if n := count(t, c, "SELECT affected_users FROM issues WHERE issue_id = 'issueA'"); n != 2 {
		t.Fatalf("issueA affected_users (u2,u3; empty ignored): %d want 2", n)
	}
	var title, release string
	if err := c.conn.QueryRow(ctx, "SELECT title, last_release FROM issues WHERE issue_id = 'issueA'").Scan(&title, &release); err != nil {
		t.Fatal(err)
	}
	if title != "ValueError: boom again" || release != "v2" {
		t.Fatalf("argMax title/release not the latest: %q %q", title, release)
	}
	if n := count(t, c, "SELECT sum(cnt) FROM issue_counts_hourly WHERE issue_id = 'issueA'"); n != 4 {
		t.Fatalf("hourly counts: %d want 4", n)
	}
	if n := count(t, c, "SELECT sum(cnt) FROM log_counts_minutely"); n != 2 {
		t.Fatalf("minutely log counts: %d want 2", n)
	}

	// Idempotent.
	applied, err = c.Migrate(ctx, opts)
	if err != nil || len(applied) != 0 {
		t.Fatalf("second migrate: applied=%v err=%v", applied, err)
	}

	// Retention changes are applied through MODIFY TTL, once.
	opts.RetentionLogsDays = 14
	if err := c.ApplyRetention(ctx, opts); err != nil {
		t.Fatalf("retention: %v", err)
	}
	if ef := engineFull(t, c, "logs_raw"); !strings.Contains(ef, "toIntervalDay(14)") {
		t.Fatalf("TTL not updated: %s", ef)
	}
	if v, _ := c.GetSetting(ctx, "retention_days.logs_raw"); v != "14" {
		t.Fatalf("setting not recorded: %q", v)
	}
	// Rows older than the new TTL disappear once TTL merges run.
	if err := c.Exec(ctx, "OPTIMIZE TABLE logs_raw FINAL"); err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if n := count(t, c, "SELECT count() FROM logs_raw"); n != 2 {
		t.Fatalf("rows within 14 days must survive: %d", n)
	}
}

func TestIntegrationUpgradeFromV1InPlace(t *testing.T) {
	c, cleanup := integrationClient(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC()

	applyV1Schema(t, c)
	seedV1(t, c, now)

	opts := MigrateOptions{Rebuild: false, RetentionLogsDays: 30, RetentionErrorsDays: 30, Logf: t.Logf}
	if _, err := c.Migrate(ctx, opts); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Columns were added and the TTL applied in place (ClickHouse materializes a modified TTL right
	// away, so the 45-day-old row is expired); partitions stay monthly.
	if n := count(t, c, "SELECT count() FROM logs_raw WHERE ts >= now() - INTERVAL 30 DAY"); n != 2 {
		t.Fatalf("rows inside the retention window must survive the in-place upgrade: %d", n)
	}
	if n := count(t, c, "SELECT count() FROM logs_raw"); n > 3 {
		t.Fatalf("unexpected row count after in-place upgrade: %d", n)
	}
	cols, _ := c.tableColumns(ctx, "logs_raw")
	for _, col := range []string{"event_id", "issue_id", "http_url", "tags", "duration_ms"} {
		if !cols[col] {
			t.Fatalf("logs_raw lacks %s after in-place upgrade", col)
		}
	}
	ef := engineFull(t, c, "logs_raw")
	if !strings.Contains(ef, "PARTITION BY toYYYYMM(ts)") || !strings.Contains(ef, "toIntervalDay(30)") {
		t.Fatalf("in-place engine: %s", ef)
	}
	// issueB's only occurrence was 40 days old and expired with the new TTL; issueA keeps its
	// recent occurrence and is backfilled.
	if n := count(t, c, "SELECT count() FROM issues WHERE issue_id = 'issueA'"); n != 1 {
		t.Fatalf("issueA must be backfilled in place: %d", n)
	}
	if n := count(t, c, "SELECT occurrence_count FROM issues WHERE issue_id = 'issueA'"); n != 1 {
		t.Fatalf("issueA occurrence_count: %d want 1", n)
	}
}

func TestIntegrationFreshInstall(t *testing.T) {
	c, cleanup := integrationClient(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := c.Migrate(ctx, MigrateOptions{Rebuild: true, Logf: t.Logf}); err != nil {
		t.Fatalf("fresh migrate: %v", err)
	}
	for _, tbl := range []string{"logs_raw", "error_occurrences", "issues_agg", "issue_counts_hourly", "log_counts_minutely", "projects", "issues_state", "schema_migrations"} {
		if ok, _ := c.tableExists(ctx, tbl); !ok {
			t.Fatalf("missing table %s", tbl)
		}
	}
	if ef := engineFull(t, c, "error_occurrences"); !strings.Contains(ef, "PARTITION BY toDate(ts)") {
		t.Fatalf("fresh error_occurrences is not v2: %s", ef)
	}
	if err := c.ApplyRetention(ctx, MigrateOptions{Logf: t.Logf}); err != nil {
		t.Fatalf("retention: %v", err)
	}
}

func TestIntegrationSavedSearchVisibility(t *testing.T) {
	c, cleanup := integrationClient(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := c.Migrate(ctx, MigrateOptions{Rebuild: true, Logf: t.Logf}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mine, err := c.UpsertSavedSearch(ctx, api.SavedSearch{ProjectID: "p1", UserID: "alice", Name: "mine", Query: "{}", IsPublic: false})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := c.UpsertSavedSearch(ctx, api.SavedSearch{ProjectID: "p1", UserID: "bob", Name: "shared", Query: "{}", IsPublic: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := c.UpsertSavedSearch(ctx, api.SavedSearch{ProjectID: "p1", UserID: "bob", Name: "private-bob", Query: "{}", IsPublic: false}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// A later version flips "mine" to public: visibility must follow the latest version.
	mine.IsPublic = true
	if _, err := c.UpsertSavedSearch(ctx, mine); err != nil {
		t.Fatalf("update: %v", err)
	}
	names := func(items []api.SavedSearch) []string {
		out := []string{}
		for _, it := range items {
			out = append(out, it.Name)
		}
		return out
	}
	got, err := c.ListSavedSearches(ctx, "p1", "carol", true, false, 50)
	if err != nil {
		t.Fatalf("list public: %v", err)
	}
	if n := names(got); len(n) != 2 || !contains(n, "mine") || !contains(n, "shared") {
		t.Fatalf("carol should see the two public views, got %v", n)
	}
	got, err = c.ListSavedSearches(ctx, "p1", "bob", true, false, 50)
	if err != nil {
		t.Fatalf("list bob: %v", err)
	}
	if n := names(got); len(n) != 3 {
		t.Fatalf("bob should see own + public views, got %v", n)
	}
	got, err = c.ListSavedSearches(ctx, "p1", "bob", false, false, 50)
	if err != nil {
		t.Fatalf("list bob only: %v", err)
	}
	if n := names(got); len(n) != 2 || contains(n, "mine") {
		t.Fatalf("bob (no public) should see only his views, got %v", n)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
