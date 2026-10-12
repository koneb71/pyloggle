//go:build integration

package clickhouse_test

// Storage accounting, per-project retention trimming, user purge and project deletion.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/ingest"
	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func TestIntegrationUsageRetentionAndDeletion(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.SampleDebug, cfg.SampleWarn = 1, 1, 1
	cfg.RateLimitRPS = 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	now := time.Now().UTC().Truncate(time.Second)

	for _, id := range []string{"p1", "p2"} {
		if _, _, err := c.CreateProject(ctx, clickhouse.ProjectUpsert{ID: id, Name: id, RetentionDays: map[string]uint16{"p1": 1, "p2": 0}[id]}); err != nil {
			t.Fatal(err)
		}
	}
	// Rows today and three days ago, for two projects and two users.
	for _, id := range []string{"p1", "p2"} {
		tn := ingest.Tenant{ProjectID: id}
		for _, age := range []time.Duration{0, 72 * time.Hour} {
			for i := 0; i < 5; i++ {
				user := "u-1"
				if i%2 == 1 {
					user = "u-2"
				}
				ts := now.Add(-age).Add(-time.Duration(i) * time.Minute)
				ev := model.Event{Timestamp: model.At(ts), Service: "svc", Env: "prod", Level: "info", Logger: "l", Message: fmt.Sprintf("line %d", i), User: &model.UserInfo{IDHash: user}}
				if o := p.Process(ctx, tn, ev, 100); o != ingest.Accepted {
					t.Fatalf("event: %v", o)
				}
				ev = model.Event{Timestamp: model.At(ts), Service: "svc", Env: "prod", Level: "error", Logger: "l", Message: "boom", User: &model.UserInfo{IDHash: user, Email: user + "@x"},
					Exception: &model.Exception{Type: "ValueError", Message: "boom", Frames: []model.Frame{{Filename: "a.py", Function: "f", Lineno: 1}}}}
				if o := p.Process(ctx, tn, ev, 100); o != ingest.Accepted {
					t.Fatalf("error: %v", o)
				}
			}
		}
		p.ProcessSpans(ctx, tn, []model.Span{{Service: "svc", Env: "prod", TraceID: strings.Repeat("a", 32), SpanID: "s" + id, Name: "x", Kind: "server", Status: "ok", StartTS: now, DurationUS: 10}}, 50)
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// Usage rows agree with the tables.
	usage, err := c.ProjectUsage(ctx, "p1", 30)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	rows := map[string]uint64{}
	var bytes uint64
	for _, u := range usage {
		rows[u.Source] += u.Rows
		bytes += u.BytesEst
	}
	if rows["logs"] != 20 || rows["errors"] != 10 || rows["spans"] != 1 || bytes == 0 { // error events are also log lines
		t.Fatalf("usage rows: %v bytes=%d", rows, bytes)
	}
	stored, err := c.StoredBytesByProject(ctx, 30*24*time.Hour)
	if err != nil || stored["p1"] == 0 || stored["p2"] == 0 {
		t.Fatalf("stored bytes: %v %v", stored, err)
	}
	comp, err := c.TableCompression(ctx)
	if err != nil || comp["logs_raw"] <= 0 || comp["logs_raw"] > 1.5 {
		t.Fatalf("compression: %v %v", comp, err)
	}

	count := func(table, project string) uint64 {
		var n uint64
		if err := c.QueryRow(ctx, "SELECT count() FROM "+table+" WHERE project_id = ?", project).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	waitMutations := func() {
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			n, _ := c.PendingMutations(ctx, "")
			if n == 0 {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("mutations did not finish")
	}

	// Per-project retention: p1 keeps one day, so its three-day-old rows go; p2's stay.
	opts := clickhouse.MigrateOptions{RetentionLogsDays: 30, RetentionErrorsDays: 30, RetentionSpansDays: 7, RetentionMetricsDays: 30}
	if err := c.TrimProjects(ctx, opts, t.Logf); err != nil {
		t.Fatalf("trim: %v", err)
	}
	waitMutations()
	if count("logs_raw", "p1") != 10 || count("error_occurrences", "p1") != 5 || count("logs_raw", "p2") != 20 || count("error_occurrences", "p2") != 10 {
		t.Fatalf("after trim: p1 logs=%d errors=%d p2 logs=%d errors=%d", count("logs_raw", "p1"), count("error_occurrences", "p1"), count("logs_raw", "p2"), count("error_occurrences", "p2"))
	}
	if v, _ := c.GetSetting(ctx, "trim.p1.logs_raw"); v == "" {
		t.Fatal("trim state must be recorded")
	}
	// A second run is a no-op (no new days).
	if err := c.TrimProjects(ctx, opts, t.Logf); err != nil {
		t.Fatalf("trim again: %v", err)
	}
	if n, _ := c.PendingMutations(ctx, ""); n != 0 {
		t.Fatalf("second trim must not issue mutations: %d pending", n)
	}

	// Purge one user from p2: rows disappear at once (lightweight delete).
	if err := c.PurgeUser(ctx, "p2", "u-1", "u-1@x"); err != nil {
		t.Fatalf("purge user: %v", err)
	}
	var left uint64
	_ = c.QueryRow(ctx, "SELECT count() FROM logs_raw WHERE project_id = 'p2' AND user_id_hash = 'u-1'").Scan(&left)
	var leftErr uint64
	_ = c.QueryRow(ctx, "SELECT count() FROM error_occurrences WHERE project_id = 'p2' AND (user_id_hash = 'u-1' OR user_email = 'u-1@x')").Scan(&leftErr)
	if left != 0 || leftErr != 0 || count("logs_raw", "p2") != 8 || count("error_occurrences", "p2") != 4 {
		t.Fatalf("purge user: left=%d/%d p2 logs=%d errors=%d", left, leftErr, count("logs_raw", "p2"), count("error_occurrences", "p2"))
	}
	if err := c.RecomputeUsage(ctx, "p2"); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	usage, _ = c.ProjectUsage(ctx, "p2", 30)
	rows = map[string]uint64{}
	for _, u := range usage {
		rows[u.Source] += u.Rows
	}
	if rows["logs"] != 8 || rows["errors"] != 4 {
		t.Fatalf("usage after recompute: %v", rows)
	}

	// Delete p1: tombstone hides it everywhere, the purge empties every table.
	keyHash := func(id string) string {
		pv, _, _ := c.GetProjectByID(ctx, id)
		return pv.APIKeyHash
	}
	h := keyHash("p1")
	if err := c.MarkProjectDeleted(ctx, "p1"); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}
	if _, ok, _ := c.GetProjectByID(ctx, "p1"); ok {
		t.Fatal("deleted project must read as absent")
	}
	if _, ok, _ := c.GetProjectByAPIKeyHash(ctx, h); ok {
		t.Fatal("deleted project's key must stop resolving")
	}
	if pv, ok, _ := c.GetProjectIncludingDeleted(ctx, "p1"); !ok || !pv.IsDeleted {
		t.Fatalf("tombstone: %+v %v", pv, ok)
	}
	list, _ := c.ListProjects(ctx, 10)
	for _, pv := range list {
		if pv.ID == "p1" {
			t.Fatal("deleted project listed")
		}
	}
	if err := c.PurgeProjectData(ctx, "p1"); err != nil {
		t.Fatalf("purge project: %v", err)
	}
	for _, table := range []string{"logs_raw", "error_occurrences", "spans", "issues_agg", "issue_counts_hourly", "log_counts_minutely", "issues_state"} {
		if n := count(table, "p1"); n != 0 {
			t.Fatalf("%s still holds %d p1 rows", table, n)
		}
	}
	if count("logs_raw", "p2") != 8 {
		t.Fatal("purge must not touch other projects")
	}
	if err := c.RecomputeUsage(ctx, "p1"); err != nil {
		t.Fatalf("recompute deleted: %v", err)
	}
	if usage, _ := c.ProjectUsage(ctx, "p1", 30); len(usage) != 0 {
		t.Fatalf("usage after delete: %+v", usage)
	}
	waitMutations()

	// Audit trail.
	if err := c.AddAudit(ctx, api.AuditEntry{Actor: "alice", Action: "project.delete", ProjectID: "p1"}); err != nil {
		t.Fatalf("audit: %v", err)
	}
	entries, err := c.ListAudit(ctx, "", 10)
	if err != nil || len(entries) != 1 || entries[0].Actor != "alice" || entries[0].Action != "project.delete" {
		t.Fatalf("list audit: %+v %v", entries, err)
	}
}
