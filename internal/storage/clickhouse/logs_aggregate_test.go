package clickhouse

import (
	"strings"
	"testing"
	"time"
)

func TestAggregateQueriesPlaceholderParity(t *testing.T) {
	q := LogQuery{ProjectID: "p", From: time.Unix(0, 0), To: time.Unix(3600, 0), Where: "attrs[?] = ?", Args: []any{"user_id", "42"}}
	where, args, err := q.where()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		spec AggregateSpec
		top  bool
	}{
		{AggregateSpec{GroupBy: "http.route", Metric: "p95", Of: "duration_ms", Limit: 10}, true},
		{AggregateSpec{GroupBy: "attrs.region", Metric: "avg", Of: "attrs.items", Limit: 5}, true},
		{AggregateSpec{GroupBy: "service", Metric: "count", Limit: 8}, true},
		{AggregateSpec{Metric: "max", Of: "http.status", Limit: 8}, false},
		{AggregateSpec{Metric: "count", Limit: 8}, false},
	}
	for _, c := range cases {
		topSQL, topArgs, seriesSQL, seriesArgs, err := aggregateQueries(where, args, c.spec, 60, []string{"a", "b"})
		if err != nil {
			t.Fatalf("%+v: %v", c.spec, err)
		}
		if c.top {
			if got, want := strings.Count(topSQL, "?"), len(topArgs); got != want {
				t.Fatalf("%+v top placeholders %d != args %d: %s", c.spec, got, want, topSQL)
			}
			if !strings.Contains(topSQL, "WITH TOTALS") || !strings.HasSuffix(topSQL, "LIMIT ?") || topArgs[len(topArgs)-1] != c.spec.Limit {
				t.Fatalf("%+v top shape: %s %v", c.spec, topSQL, topArgs)
			}
		} else if topSQL != "" {
			t.Fatalf("ungrouped query must not rank groups: %s", topSQL)
		}
		if got, want := strings.Count(seriesSQL, "?"), len(seriesArgs); got != want {
			t.Fatalf("%+v series placeholders %d != args %d: %s", c.spec, got, want, seriesSQL)
		}
		if seriesArgs[0] != 60 {
			t.Fatalf("bucket seconds must bind first: %v", seriesArgs)
		}
		if c.spec.GroupBy != "" {
			if _, ok := seriesArgs[1].([]string); !ok || !strings.Contains(seriesSQL, "if(has(?, ") {
				t.Fatalf("kept keys must bind right after the bucket: %s %v", seriesSQL, seriesArgs)
			}
		}
		// The map key of the WHERE must come after every SELECT arg.
		if i := indexOf(seriesArgs, "user_id"); i < 0 || i != len(seriesArgs)-2 {
			t.Fatalf("where args must bind last: %v", seriesArgs)
		}
	}
}

func TestMetricExpr(t *testing.T) {
	for _, bad := range []struct{ m, of string }{{"avg", ""}, {"avg", "level"}, {"p95", "message"}, {"median", "duration_ms"}, {"sum", "service"}} {
		if _, _, err := metricExpr(bad.m, bad.of); err == nil {
			t.Fatalf("%s(%s) must be rejected", bad.m, bad.of)
		}
	}
	e, args, err := metricExpr("avg", "duration_ms")
	if err != nil || !strings.Contains(e, "nullIf(toFloat64(duration_ms), 0)") || len(args) != 0 {
		t.Fatalf("duration: %s %v %v", e, args, err)
	}
	e, args, err = metricExpr("p99", "@items")
	if err != nil || !strings.Contains(e, "quantileTDigest(0.99)(toFloat64OrNull(attrs[?]))") || len(args) != 1 || args[0] != "items" {
		t.Fatalf("attr: %s %v %v", e, args, err)
	}
	e, _, _ = metricExpr("count", "whatever")
	if e != "toNullable(toFloat64(count()))" {
		t.Fatalf("count: %s", e)
	}
}

func indexOf(args []any, want any) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}
