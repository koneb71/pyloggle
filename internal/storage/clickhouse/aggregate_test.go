package clickhouse

import (
	"strings"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/logquery"
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
		topSQL, topArgs, seriesSQL, seriesArgs, err := aggregateQueries(messagesSource, where, args, c.spec, 60, []string{"a", "b"})
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

func TestChartSource(t *testing.T) {
	from, to := time.Date(2026, 10, 15, 3, 7, 30, 0, time.UTC), time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	parse := func(s string) logquery.Query {
		q, err := logquery.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	base := ChartQuery{ProjectID: "p", Service: "shop", From: from, To: to}
	cases := []struct {
		source, q, groupBy, metric, table string
		firstArg                          time.Time
	}{
		{"messages", "level:error", "", "count", "logs_raw", from},
		{"errors", "exception:ValueError", "release", "problems", "error_occurrences", from},
		{"requests", "route:/orders*", "http.route", "p95", "request_stats_minutely", from.Truncate(time.Minute)},
		{"requests", "", "release", "count", "request_usage_hourly", from.Truncate(time.Hour)},
		{"requests", "", "", "people", "request_usage_hourly", from.Truncate(time.Hour)},
		{"requests", "version:1.4.0", "", "count", "request_usage_hourly", from.Truncate(time.Hour)},
	}
	for _, c := range cases {
		q := base
		q.Source, q.Query = c.source, parse(c.q)
		spec := AggregateSpec{GroupBy: c.groupBy, Metric: c.metric}
		src, where, args, err := chartSource(q, spec)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if src.table != c.table || !args[1].(time.Time).Equal(c.firstArg) || !strings.Contains(where, "service = ?") {
			t.Fatalf("%+v: %s %s %v", c, src.table, where, args)
		}
		top, topArgs, series, seriesArgs, err := aggregateQueries(src, where, args, AggregateSpec{GroupBy: c.groupBy, Metric: c.metric, Limit: 5}, 60, []string{"a"})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if strings.Count(series, "?") != len(seriesArgs) || (top != "" && strings.Count(top, "?") != len(topArgs)) {
			t.Fatalf("%+v placeholders: %s %v", c, series, seriesArgs)
		}
		if c.source == "requests" && !strings.Contains(series, "sum(requests) AS rows") {
			t.Fatalf("a request row stands for its count: %s", series)
		}
	}
	for _, bad := range []struct{ source, q, groupBy string }{
		{"requests", "timeout", ""},
		{"requests", "@user:1", ""},
		{"requests", "", "logger"},
		{"spans", "", ""},
	} {
		q := base
		q.Source, q.Query = bad.source, parse(bad.q)
		src, where, args, err := chartSource(q, AggregateSpec{GroupBy: bad.groupBy, Metric: "count"})
		if err == nil {
			_, _, _, _, err = aggregateQueries(src, where, args, AggregateSpec{GroupBy: bad.groupBy, Metric: "count"}, 60, nil)
		}
		if err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
}

func TestRequestMetric(t *testing.T) {
	for metric, want := range map[string]string{
		"count":        "sum(requests)",
		"failed":       "sum(errors)",
		"failure_rate": "toFloat64(sum(errors)) / sum(requests) * 100",
		"p95":          latencyMerge + "[2]",
		"people":       "uniqIfMerge(users)",
	} {
		if e, _, err := requestMetric(metric, ""); err != nil || !strings.Contains(e, want) {
			t.Errorf("%s: %s %v", metric, e, err)
		}
	}
	if _, _, err := requestMetric("avg", ""); err == nil {
		t.Error("requests have no avg")
	}
	if _, _, err := requestMetric("p95", "http.status"); err == nil {
		t.Error("requests measure their response time only")
	}
	if _, _, err := rowMetric(logquery.Logs, "problems", ""); err == nil {
		t.Error("problems are counted over errors")
	}
	if e, _, err := rowMetric(logquery.Errors, "avg", "http.status"); err != nil || !strings.Contains(e, "http_status") {
		t.Errorf("errors measure their fields: %s %v", e, err)
	}
}
