package metrics

import (
	"strings"
	"testing"
)

func TestTextExposition(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("t_events_total", "Events.", "project", "outcome")
	g := r.NewGauge("t_depth", "Depth.", "kind")
	h := r.NewHistogram("t_seconds", "Latency.", []float64{0.1, 1}, "kind")
	c.Inc("p1", "accepted")
	c.Add(2, "p1", "accepted")
	c.Inc("p2", `we"ird\`)
	g.Set(12, "logs")
	h.Observe(0.05, "logs")
	h.Observe(0.5, "logs")
	h.Observe(7, "logs")

	var b strings.Builder
	r.WriteText(&b)
	want := `# HELP t_events_total Events.
# TYPE t_events_total counter
t_events_total{project="p1",outcome="accepted"} 3
t_events_total{project="p2",outcome="we\"ird\\"} 1
# HELP t_depth Depth.
# TYPE t_depth gauge
t_depth{kind="logs"} 12
# HELP t_seconds Latency.
# TYPE t_seconds histogram
t_seconds_bucket{kind="logs",le="0.1"} 1
t_seconds_bucket{kind="logs",le="1"} 2
t_seconds_bucket{kind="logs",le="+Inf"} 3
t_seconds_sum{kind="logs"} 7.55
t_seconds_count{kind="logs"} 3
`
	if b.String() != want {
		t.Fatalf("exposition mismatch:\n%s\nwant:\n%s", b.String(), want)
	}
	snap := r.Snapshot()
	if snap[`t_events_total{project="p1",outcome="accepted"}`] != 3 || snap[`t_seconds_count{kind="logs"}`] != 3 {
		t.Fatalf("snapshot: %+v", snap)
	}
	if q := h.Quantile(0.5, "logs"); q < 0.1 || q > 1 {
		t.Fatalf("p50 should fall in the second bucket, got %v", q)
	}
}

func TestLabelArityIsEnforced(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on wrong label count")
		}
	}()
	NewRegistry().NewCounter("x_total", "x", "a").Inc("a", "b")
}
