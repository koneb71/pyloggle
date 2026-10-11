//go:build integration

package clickhouse_test

// OTLP metrics: catalog, gauge/counter/histogram series with cumulative differencing, and the
// OpenTelemetry-sourced request statistics.

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/model"
	"pyloggle/internal/otlp"
	"pyloggle/internal/storage/clickhouse"
)

func TestIntegrationMetrics(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	cfg := config.FromEnv()
	cfg.RateLimitRPS = 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	tn := ingest.Tenant{ProjectID: "p1"}
	// Aligned to the 5-minute buckets the assertions below count on.
	start := time.Now().UTC().Truncate(5 * time.Minute).Add(-30 * time.Minute)
	bounds := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}

	attrs := func(kv ...string) model.Attrs {
		a := model.Attrs{}
		for i := 0; i+1 < len(kv); i += 2 {
			a[kv[i]] = kv[i+1]
		}
		return a
	}
	hash := func(a model.Attrs) uint64 {
		keys := make([]string, 0, len(a))
		for k := range a {
			keys = append(keys, k)
		}
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
				keys[j], keys[j-1] = keys[j-1], keys[j]
			}
		}
		return otlp.SeriesHash(keys, a)
	}
	var pts []model.MetricPoint
	// 30 samples, one per minute: a gauge on two hosts, a cumulative counter growing 60/min with a
	// reset at minute 20, and the cumulative HTTP duration histogram for two routes (one failing).
	okAttrs := attrs(model.AttrHTTPMethod, "GET", model.AttrHTTPRoute, "/orders", model.AttrHTTPStatusCode, "200")
	errAttrs := attrs(model.AttrHTTPMethod, "POST", model.AttrHTTPRoute, "/pay", model.AttrHTTPStatusCode, "500")
	var okCounts, errCounts []uint64
	okCounts, errCounts = make([]uint64, 9), make([]uint64, 9)
	var okN, errN uint64
	var okSum, errSum float64
	for i := 0; i < 30; i++ {
		ts := start.Add(time.Duration(i) * time.Minute)
		for h, base := range map[string]float64{"a": 100, "b": 200} {
			a := attrs("host", h)
			pts = append(pts, model.MetricPoint{Service: "svc", Env: "prod", Name: "mem.used", Type: "gauge", Temporality: "unspecified", Unit: "By", Attrs: a, SeriesHash: hash(a), TS: ts, Value: base + float64(i)})
		}
		cv := float64(60 * (i + 1))
		if i >= 20 {
			cv = float64(60 * (i - 19)) // process restarted: counter reset
		}
		pts = append(pts, model.MetricPoint{Service: "svc", Env: "prod", Name: "reqs.total", Type: "sum", Monotonic: true, Temporality: "cumulative", Attrs: model.Attrs{}, SeriesHash: hash(nil), TS: ts, Value: cv})
		// 10 ok requests per minute at ~20ms (bucket index 2: 0.01..0.025), 2 failing at ~300ms (index 6).
		okCounts[2] += 10
		okN += 10
		okSum += 0.2
		errCounts[6] += 2
		errN += 2
		errSum += 0.6
		pts = append(pts,
			model.MetricPoint{Service: "svc", Env: "prod", Name: model.HTTPServerDuration, Type: "histogram", Temporality: "cumulative", Unit: "s", Attrs: okAttrs, SeriesHash: hash(okAttrs), TS: ts, Count: okN, Sum: okSum, Bounds: bounds, BucketCounts: append([]uint64{}, okCounts...)},
			model.MetricPoint{Service: "svc", Env: "prod", Name: model.HTTPServerDuration, Type: "histogram", Temporality: "cumulative", Unit: "s", Attrs: errAttrs, SeriesHash: hash(errAttrs), TS: ts, Count: errN, Sum: errSum, Bounds: bounds, BucketCounts: append([]uint64{}, errCounts...)},
		)
	}
	if n, o := p.ProcessMetrics(ctx, tn, pts, 1000); n != len(pts) || o != ingest.Accepted {
		t.Fatalf("ProcessMetrics: %d %v", n, o)
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	from, to := start, start.Add(30*time.Minute)

	names, err := c.MetricNames(ctx, "p1")
	if err != nil || len(names) != 3 {
		t.Fatalf("names: %+v %v", names, err)
	}
	byName := map[string]int{}
	for i, n := range names {
		byName[n.Name] = i
	}
	if h := names[byName[model.HTTPServerDuration]]; h.Type != "histogram" || h.Unit != "s" || h.Temporality != "cumulative" {
		t.Fatalf("catalog histogram: %+v", h)
	}
	if r := names[byName["reqs.total"]]; r.Type != "sum" || !r.Monotonic {
		t.Fatalf("catalog counter: %+v", r)
	}

	// Gauge avg by host over 5-minute buckets: host b reads 100 above host a.
	ms, found, err := c.MetricSeries(ctx, clickhouse.MetricQuery{ProjectID: "p1", Name: "mem.used", GroupBy: "host", From: from, To: to, Bucket: 5 * time.Minute})
	if err != nil || !found || ms.Agg != "avg" || ms.Type != "gauge" || len(ms.Series) != 2 || ms.BucketSeconds != 300 || len(ms.Series[0].Points) != 6 {
		t.Fatalf("gauge series: %+v found=%v err=%v", ms, found, err)
	}
	if ms.Series[0].Label != "b" || ms.Series[1].Label != "a" || *ms.Series[0].Points[0].Value-*ms.Series[1].Points[0].Value != 100 {
		t.Fatalf("gauge values: %+v", ms.Series)
	}
	// Counter rate: 60/min = 1/s everywhere except the first bucket (no predecessor) and the
	// reset bucket, which must not go negative.
	ms, _, err = c.MetricSeries(ctx, clickhouse.MetricQuery{ProjectID: "p1", Name: "reqs.total", From: from, To: to, Bucket: time.Minute})
	if err != nil || ms.Agg != "rate" || len(ms.Series) != 1 {
		t.Fatalf("rate series: %+v %v", ms, err)
	}
	for i, pt := range ms.Series[0].Points {
		if i == 0 {
			if pt.Value != nil {
				t.Fatalf("first bucket has no rate: %v", *pt.Value)
			}
			continue
		}
		if pt.Value == nil || *pt.Value < 0 || math.Abs(*pt.Value-1) > 0.01 {
			t.Fatalf("rate at %d: %v", i, pt.Value)
		}
	}
	// Histogram p95 by route from differenced cumulative buckets.
	ms, _, err = c.MetricSeries(ctx, clickhouse.MetricQuery{ProjectID: "p1", Name: model.HTTPServerDuration, GroupBy: model.AttrHTTPRoute, From: from, To: to, Bucket: 5 * time.Minute})
	if err != nil || ms.Agg != "p95" || len(ms.Series) != 2 {
		t.Fatalf("p95 series: %+v %v", ms, err)
	}
	for _, s := range ms.Series {
		v := s.Points[3].Value
		switch s.Label {
		case "/orders":
			if v == nil || *v < 0.01 || *v > 0.025 {
				t.Fatalf("/orders p95 %v", v)
			}
		case "/pay":
			if v == nil || *v < 0.25 || *v > 0.5 {
				t.Fatalf("/pay p95 %v", v)
			}
		}
	}
	if _, _, err := c.MetricSeries(ctx, clickhouse.MetricQuery{ProjectID: "p1", Name: "mem.used", Agg: "p95", From: from, To: to}); err == nil {
		t.Fatal("p95 on a gauge must be rejected")
	}
	if _, found, err := c.MetricSeries(ctx, clickhouse.MetricQuery{ProjectID: "p1", Name: "nope", From: from, To: to}); err != nil || found {
		t.Fatalf("unknown metric: found=%v err=%v", found, err)
	}

	// OTel request stats: 12 requests/min after the first sample, 2 of them failing.
	st, ok, err := c.RequestStatsFromOTel(ctx, "p1", "", "", from, to, 60, 10)
	if err != nil || !ok {
		t.Fatalf("otel stats: ok=%v err=%v", ok, err)
	}
	if st.Totals.Requests != 29*12 || st.Totals.Errors != 29*2 || math.Abs(st.Totals.ErrorPct-100.0/6) > 0.01 {
		t.Fatalf("totals: %+v", st.Totals)
	}
	if st.Totals.P50MS < 10 || st.Totals.P50MS > 25 || st.Totals.P99MS < 250 || st.Totals.P99MS > 500 {
		t.Fatalf("latency ms: %+v", st.Totals)
	}
	if len(st.Routes) != 2 || st.Routes[0].Route != "/orders" || st.Routes[0].Requests != 290 || st.Routes[1].ErrorPct != 100 || st.Routes[1].P95MS < 250 {
		t.Fatalf("routes: %+v", st.Routes)
	}
	var sum uint64
	for _, pt := range st.Points {
		sum += pt.Requests
	}
	if sum != 29*12 || len(st.Points) != 30 {
		t.Fatalf("points: sum=%d n=%d", sum, len(st.Points))
	}
	if _, ok, err := c.RequestStatsFromOTel(ctx, "p2", "", "", from, to, 60, 10); err != nil || ok {
		t.Fatalf("project without the metric: ok=%v err=%v", ok, err)
	}
	_ = fmt.Sprint
}
