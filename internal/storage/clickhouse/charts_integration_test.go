//go:build integration

package clickhouse_test

// Analytics → Charts over messages, errors and request counts.

import (
	"context"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/ingest"
	"github.com/koneb71/pyloggle/internal/logquery"
	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func TestIntegrationChartsOverEverySource(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.SampleWarn, cfg.RateLimitRPS = 1, 1, 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	tn := ingest.Tenant{ProjectID: "p1"}
	now := time.Now().UTC().Truncate(time.Minute)

	for i, user := range []string{"u1", "u2", "u1"} {
		p.Process(ctx, tn, model.Event{Timestamp: model.At(now.Add(-time.Duration(i) * time.Minute)), Service: "shop", Env: "prod", Level: "info", Message: "checkout",
			DurationMS: float64(10 * (i + 1)), User: &model.UserInfo{IDHash: user}}, 100)
	}
	for i, typ := range []string{"ValueError", "ValueError", "KeyError"} {
		p.Process(ctx, tn, model.Event{Timestamp: model.At(now.Add(-time.Duration(i) * time.Minute)), Service: "shop", Env: "prod", Level: "error", Message: "boom", Release: "1.4.0",
			HTTP: &model.HTTPInfo{Method: "POST", Route: "/pay", Status: 500}, User: &model.UserInfo{IDHash: "u" + string(rune('1'+i))},
			Exception: &model.Exception{Type: typ, Frames: []model.Frame{{Filename: "a.py", Function: typ, Lineno: 1}}}}, 100)
	}
	p.ProcessRequestCounts(ctx, tn, []model.RequestCount{
		{Minute: model.At(now.Add(-2 * time.Minute)), Service: "shop", Env: "prod", Release: "1.4.0", Method: "GET", Route: "/cart", Requests: 10, Errors: 1, DurationsMS: []float32{10, 20, 30, 40}, Users: []string{"a", "b"}},
		{Minute: model.At(now.Add(-time.Minute)), Service: "shop", Env: "prod", Release: "1.3.0", Method: "GET", Route: "/orders", Requests: 30, Users: []string{"c"}},
	}, 100)
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	chart := func(source, q string, spec clickhouse.AggregateSpec) map[string]float64 {
		t.Helper()
		parsed, err := logquery.Parse(q)
		if err != nil {
			t.Fatal(err)
		}
		agg, err := c.ChartAggregate(ctx, clickhouse.ChartQuery{Source: source, ProjectID: "p1", From: now.Add(-time.Hour), To: now.Add(time.Minute), Query: parsed}, spec)
		if err != nil {
			t.Fatalf("%s %q %+v: %v", source, q, spec, err)
		}
		out := map[string]float64{}
		if agg.Total.Value != nil {
			out["total"] = *agg.Total.Value
		}
		out["rows"] = float64(agg.Total.Rows)
		for _, g := range agg.Groups {
			if g.Value != nil {
				out[g.Key] = *g.Value
			}
		}
		return out
	}

	if got := chart("messages", "checkout", clickhouse.AggregateSpec{Metric: "people"}); got["total"] != 2 {
		t.Fatalf("people over messages: %v", got)
	}
	if got := chart("errors", "", clickhouse.AggregateSpec{Metric: "problems", GroupBy: "exception"}); got["ValueError"] != 1 || got["KeyError"] != 1 || got["total"] != 2 {
		t.Fatalf("problems by type: %v", got)
	}
	if got := chart("errors", "exception:ValueError", clickhouse.AggregateSpec{Metric: "count"}); got["total"] != 2 {
		t.Fatalf("errors searched: %v", got)
	}
	// Requests: 40 counted plus the 3 crashes counted by their error reports.
	if got := chart("requests", "", clickhouse.AggregateSpec{Metric: "count", GroupBy: "http.route"}); got["/cart"] != 10 || got["/orders"] != 30 || got["/pay"] != 3 || got["rows"] != 43 {
		t.Fatalf("requests by endpoint: %v", got)
	}
	if got := chart("requests", "route:/cart", clickhouse.AggregateSpec{Metric: "p50"}); got["total"] < 19 || got["total"] > 31 {
		t.Fatalf("median response time: %v", got)
	}
	if got := chart("requests", "route:/cart", clickhouse.AggregateSpec{Metric: "failure_rate"}); got["total"] != 10 {
		t.Fatalf("failure rate: %v", got)
	}
	if got := chart("requests", "", clickhouse.AggregateSpec{Metric: "people", GroupBy: "release"}); got["1.4.0"] != 5 || got["1.3.0"] != 1 {
		// 1.4.0: a and b on /cart, u1 u2 u3 on /pay.
		t.Fatalf("people by version: %v", got)
	}
	if got := chart("requests", "version:1.3.0", clickhouse.AggregateSpec{Metric: "count"}); got["total"] != 30 {
		t.Fatalf("requests searched by version: %v", got)
	}
}
