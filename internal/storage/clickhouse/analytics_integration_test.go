//go:build integration

package clickhouse_test

// Analytics and request stats: aggregate totals agree with counts, quantiles agree with a
// hand-run query, and request_stats_minutely agrees with the raw rows.

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/ingest"
	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func TestIntegrationAnalyticsAndRequestStats(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.SampleDebug, cfg.SampleWarn = 1, 1, 1
	cfg.RateLimitRPS = 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	tn := ingest.Tenant{ProjectID: "p1"}
	now := time.Now().UTC().Truncate(time.Minute).Add(-time.Minute)

	req := func(at time.Time, method, route string, status int, latencyMS int, items int) model.Event {
		ev := model.Event{Timestamp: model.At(at), Service: "web", Env: "prod", Level: "info", Logger: "web.http", Message: "request",
			HTTP:  &model.HTTPInfo{Method: method, Route: route, Status: uint16(status)},
			Attrs: model.Attrs{"latency_ms": fmt.Sprint(latencyMS), "items": fmt.Sprint(items)}}
		if status >= 500 {
			ev.Level = "error"
		}
		return ev
	}
	var events []model.Event
	// 20 GET /orders at 10..200 ms, 5 POST /pay 500s with latency, 3 POST /pay crashes without.
	for i := 1; i <= 20; i++ {
		events = append(events, req(now.Add(-time.Duration(i)*time.Second), "GET", "/orders", 200, i*10, i))
	}
	for i := 1; i <= 5; i++ {
		events = append(events, req(now.Add(-time.Duration(30+i)*time.Second), "POST", "/pay", 500, 300, 1))
	}
	for i := 1; i <= 3; i++ {
		events = append(events, model.Event{Timestamp: model.At(now.Add(-time.Duration(40+i) * time.Second)), Service: "web", Env: "prod", Level: "error", Logger: "web.pay", Message: "boom",
			HTTP:      &model.HTTPInfo{Method: "POST", Route: "/pay", Status: 500},
			Exception: &model.Exception{Type: "RuntimeError", Message: "boom", Frames: []model.Frame{{Filename: "pay.py", Function: "pay", Lineno: 1}}}})
	}
	// Noise: lines without HTTP context must not count as requests.
	for i := 1; i <= 4; i++ {
		events = append(events, model.Event{Timestamp: model.At(now.Add(-time.Duration(50+i) * time.Second)), Service: "web", Env: "prod", Level: "info", Logger: "web.jobs", Message: "tick"})
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
	lq := clickhouse.LogQuery{ProjectID: "p1", From: from, To: to}

	// count by route: totals agree with CountLogs and with the histogram.
	agg, err := c.LogAggregate(ctx, lq, clickhouse.AggregateSpec{GroupBy: "http.route", Metric: "count", Limit: 10})
	if err != nil {
		t.Fatalf("aggregate count: %v", err)
	}
	n, _ := c.CountLogs(ctx, lq)
	if agg.Total.Rows != n || n != uint64(len(events)) {
		t.Fatalf("total rows %d != count %d (events %d)", agg.Total.Rows, n, len(events))
	}
	byKey := map[string]uint64{}
	for _, g := range agg.Groups {
		byKey[g.Key] = g.Rows
	}
	if byKey["/orders"] != 20 || byKey["/pay"] != 8 || byKey[""] != 4 || agg.Other != nil {
		t.Fatalf("groups: %+v other=%+v", byKey, agg.Other)
	}
	var histTotal uint64
	for _, b := range agg.Histogram.Buckets {
		histTotal += b.Total
	}
	if agg.Histogram == nil || histTotal != n || len(agg.Histogram.Series) != 3 {
		t.Fatalf("histogram: total %d series %v", histTotal, agg.Histogram.Series)
	}
	var seriesSum float64
	for _, v := range agg.Groups[0].Series {
		if v != nil {
			seriesSum += *v
		}
	}
	if seriesSum != float64(agg.Groups[0].Rows) {
		t.Fatalf("series must sum to the group's rows: %v vs %d", seriesSum, agg.Groups[0].Rows)
	}

	// limit 1 folds the rest into other.
	agg, err = c.LogAggregate(ctx, lq, clickhouse.AggregateSpec{GroupBy: "http.route", Metric: "count", Limit: 1})
	if err != nil || len(agg.Groups) != 1 || agg.Groups[0].Key != "/orders" || agg.Other == nil || agg.Other.Rows != 12 {
		t.Fatalf("other fold: %+v %v", agg, err)
	}

	// p95 of duration by route equals a hand-run query; rows without a duration are ignored.
	agg, err = c.LogAggregate(ctx, lq, clickhouse.AggregateSpec{GroupBy: "http.route", Metric: "p95", Of: "duration_ms", Limit: 10})
	if err != nil {
		t.Fatalf("aggregate p95: %v", err)
	}
	var want float64
	if err := c.QueryRow(ctx, `SELECT toFloat64(quantileTDigest(0.95)(toFloat64(duration_ms))) FROM logs_raw WHERE project_id = 'p1' AND http_route = '/orders' AND duration_ms > 0`).Scan(&want); err != nil {
		t.Fatalf("hand query: %v", err)
	}
	var orders, empty *float64
	for _, g := range agg.Groups {
		switch g.Key {
		case "/orders":
			orders = g.Value
		case "":
			empty = g.Value
		}
	}
	if orders == nil || math.Abs(*orders-want) > 0.5 || empty != nil {
		t.Fatalf("p95: got %v want %v; no-http group value %v", orders, want, empty)
	}
	if agg.Total.Value == nil || *agg.Total.Value < 100 {
		t.Fatalf("range total p95 must come from WITH TOTALS: %v", agg.Total.Value)
	}

	// avg over a map attribute parses the strings; ungrouped queries work too.
	agg, err = c.LogAggregate(ctx, lq, clickhouse.AggregateSpec{Metric: "avg", Of: "attrs.items"})
	if err != nil || agg.Total.Value == nil || math.Abs(*agg.Total.Value-(210.0+5)/25) > 0.01 || len(agg.Total.Series) == 0 {
		t.Fatalf("avg attrs.items: %+v %v", agg.Total, err)
	}

	// request_stats_minutely: 25 rows with a latency + 3 crashes = 28 requests, 8 errors.
	st, err := c.RequestStats(ctx, "p1", "", "", from, to, 60, 10)
	if err != nil {
		t.Fatalf("request stats: %v", err)
	}
	if st.Totals.Requests != 28 || st.Totals.Errors != 8 || math.Abs(st.Totals.ErrorPct-800.0/28) > 0.01 {
		t.Fatalf("totals: %+v", st.Totals)
	}
	if st.Totals.P50MS <= 0 || st.Totals.P95MS < st.Totals.P50MS || st.Totals.P99MS < st.Totals.P95MS {
		t.Fatalf("latency quantiles: %+v", st.Totals)
	}
	var sumReq, sumErr uint64
	for _, pt := range st.Points {
		sumReq += pt.Requests
		sumErr += pt.Errors
	}
	if sumReq != 28 || sumErr != 8 {
		t.Fatalf("points sum %d/%d", sumReq, sumErr)
	}
	if len(st.Routes) != 2 || st.Routes[0].Route != "/orders" || st.Routes[0].Requests != 20 || st.Routes[1].Route != "/pay" || st.Routes[1].Errors != 8 || st.Routes[1].ErrorPct != 100 {
		t.Fatalf("routes: %+v", st.Routes)
	}
	if st.Routes[1].P95MS < 299 || st.Routes[1].P95MS > 301 {
		t.Fatalf("/pay latency should come from the five timed rows only: %+v", st.Routes[1])
	}
	st, err = c.RequestStats(ctx, "p1", "nope", "", from, to, 60, 10)
	if err != nil || st.Totals.Requests != 0 || len(st.Routes) != 0 {
		t.Fatalf("scoped to an unknown service: %+v %v", st.Totals, err)
	}
}
