//go:build integration

package clickhouse_test

// Overview queries: per-app health agrees with what was sent, and releases come back newest
// first with their errors in the range.

import (
	"context"
	"testing"
	"time"

	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/model"
)

func TestIntegrationServiceHealthAndReleases(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.SampleDebug, cfg.SampleWarn = 1, 1, 1
	cfg.RateLimitRPS = 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	tn := ingest.Tenant{ProjectID: "ov"}
	now := time.Now().UTC().Truncate(time.Minute).Add(-time.Minute)

	var events []model.Event
	// web: 40 requests, 4 of them 500s (10% failed), on release 2.0.0.
	for i := 0; i < 40; i++ {
		status, level := uint16(200), "info"
		if i < 4 {
			status, level = 500, "error"
		}
		events = append(events, model.Event{Timestamp: model.At(now.Add(-time.Duration(i) * time.Second)), Service: "web", Env: "prod", Release: "2.0.0", Level: level, Message: "request",
			HTTP: &model.HTTPInfo{Method: "GET", Route: "/orders", Status: status}, Attrs: model.Attrs{"latency_ms": "50"}})
	}
	// web: two crashes on 2.0.0 and one on the older 1.9.0 (sent earlier, so 1.9.0 is older).
	crash := func(at time.Time, release string) model.Event {
		return model.Event{Timestamp: model.At(at), Service: "web", Env: "prod", Release: release, Level: "error", Message: "boom",
			Exception: &model.Exception{Type: "RuntimeError", Message: "boom", Frames: []model.Frame{{Filename: "app.py", Function: "f", Lineno: 1}}}}
	}
	events = append(events, crash(now.Add(-3*time.Hour), "1.9.0"), crash(now.Add(-50*time.Second), "2.0.0"), crash(now.Add(-51*time.Second), "2.0.0"))
	// worker: log lines only, staging.
	for i := 0; i < 5; i++ {
		events = append(events, model.Event{Timestamp: model.At(now.Add(-time.Duration(i) * time.Second)), Service: "worker", Env: "staging", Level: "info", Message: "tick"})
	}
	for _, ev := range events {
		if o := p.Process(ctx, tn, ev, 500); o != ingest.Accepted {
			t.Fatalf("event dropped: %v", o)
		}
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	from, to := now.Add(-time.Hour), now.Add(time.Minute)

	health, err := c.ServiceHealth(ctx, "ov", "", from, to, 10)
	if err != nil {
		t.Fatalf("service health: %v", err)
	}
	if len(health) != 2 || health[0].Service != "web" || health[1].Service != "worker" {
		t.Fatalf("apps: %+v", health)
	}
	web, worker := health[0], health[1]
	if web.Requests != 40 || web.Failed != 4 || web.FailedPct != 10 || web.Status != "failing" {
		t.Fatalf("web: %+v", web)
	}
	// Errors are issue occurrences: the 4 error-level request lines and the 2 recent crashes.
	if web.Errors != 6 || web.Logs != 42 || web.P50MS <= 0 || len(web.Envs) != 1 || web.Envs[0] != "prod" {
		t.Fatalf("web errors/latency/envs: %+v", web)
	}
	if worker.Logs != 5 || worker.Requests != 0 || worker.Status != "ok" || worker.LastSeen.IsZero() {
		t.Fatalf("worker: %+v", worker)
	}
	onlyStaging, err := c.ServiceHealth(ctx, "ov", "staging", from, to, 10)
	if err != nil || len(onlyStaging) != 1 || onlyStaging[0].Service != "worker" {
		t.Fatalf("env filter: %+v %v", onlyStaging, err)
	}

	rels, err := c.ListReleases(ctx, "ov", from, to, 10)
	if err != nil {
		t.Fatalf("releases: %v", err)
	}
	if len(rels) != 2 || rels[0].Release != "2.0.0" || rels[1].Release != "1.9.0" {
		t.Fatalf("releases order: %+v", rels)
	}
	// Errors count occurrences in range: 2.0.0 has 4 failed requests and 2 crashes (two issues);
	// 1.9.0's crash is outside the hour.
	if rels[0].Errors != 6 || rels[0].Issues != 2 || rels[1].Errors != 0 {
		t.Fatalf("release errors: %+v", rels)
	}
}
