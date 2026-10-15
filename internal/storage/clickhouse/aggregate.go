package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/logquery"
)

// AggregateSpec is a chart: a metric over a field, split by a group-by field, bucketed over
// time. Field names are the query language's.
type AggregateSpec struct {
	GroupBy string        // "" = no grouping
	Metric  string        // see ChartMetrics
	Of      string        // measured field for avg, sum, min, max and the percentiles (duration_ms, http.status, attrs.<k>)
	Bucket  time.Duration // 0 = NiceBucket(from, to, 60)
	Limit   int           // top groups kept (default 10, max 50); the rest fold into "other"
	Sort    string        // value (default) | rows
}

// AggregateMetrics lists the metrics over messages in menu order (GET /logs/aggregate).
var AggregateMetrics = ChartMetrics["messages"]

// ChartMetrics lists each chart source's metrics in menu order. Messages and errors measure any
// numeric field; requests have their counts, failures and response times built in.
var ChartMetrics = map[string][]string{
	"messages": {"count", "people", "avg", "sum", "min", "max", "p50", "p95", "p99"},
	"errors":   {"count", "problems", "people", "avg", "sum", "min", "max", "p50", "p95", "p99"},
	"requests": {"count", "failed", "failure_rate", "people", "p50", "p95", "p99"},
}

const otherKey = "other"

// aggSource is what a chart reads: a table, its time column, how many things one row stands for
// and how metrics are written over it.
type aggSource struct {
	name   string
	table  string
	ts     string
	rows   string
	schema *logquery.Schema
	metric func(metric, of string) (string, []any, error)
}

var (
	messagesSource = aggSource{name: "messages", table: "logs_raw", ts: "ts", rows: "count()", schema: logquery.Logs, metric: metricExpr}
	errorsSource   = aggSource{name: "errors", table: "error_occurrences", ts: "ts", rows: "count()", schema: logquery.Errors,
		metric: func(metric, of string) (string, []any, error) { return rowMetric(logquery.Errors, metric, of) }}
	// Requests read the per-minute statistics unless a chart needs versions or people, which only
	// the hourly table has.
	requestsMinutely = aggSource{name: "requests", table: "request_stats_minutely", ts: "bucket", rows: "sum(requests)", schema: logquery.Requests, metric: requestMetric}
	requestsHourly   = aggSource{name: "requests", table: "request_usage_hourly", ts: "hour", rows: "sum(requests)", schema: logquery.Requests, metric: requestMetric}
)

// additive metrics add up across groups and buckets: an empty bucket is 0 and "other" has a value.
func additive(metric string) bool {
	return metric == "count" || metric == "sum" || metric == "failed"
}

// metricExpr renders a metric over messages.
func metricExpr(metric, of string) (string, []any, error) {
	return rowMetric(logquery.Logs, metric, of)
}

// rowMetric renders the aggregate for a metric over a field of a table with one row per thing
// (messages, errors). Unset numeric columns are stored as 0 (duration_ms, http_status), so they
// become NULL and no longer drag averages down; map values are parsed leniently. The result is
// always Nullable(Float64) so scanning is uniform.
func rowMetric(schema *logquery.Schema, metric, of string) (string, []any, error) {
	switch metric {
	case "count":
		return "toNullable(toFloat64(count()))", nil, nil
	case "people":
		return "toNullable(toFloat64(uniqIf(user_id_hash, user_id_hash != '')))", nil, nil
	case "problems":
		if schema != logquery.Errors {
			return "", nil, errors.New("problems are counted over errors")
		}
		return "toNullable(toFloat64(uniq(issue_id)))", nil, nil
	}
	if strings.TrimSpace(of) == "" {
		return "", nil, errors.New("metric needs a field (of=...)")
	}
	expr, args, kind := schema.Resolve(of)
	var x string
	switch {
	case len(args) > 0:
		x = "toFloat64OrNull(" + expr + ")"
	case expr == "duration_ms" || expr == "http_status":
		x = "nullIf(toFloat64(" + expr + "), 0)"
	case kind == "number":
		x = "toFloat64(" + expr + ")"
	default:
		return "", nil, fmt.Errorf("%s is not numeric", of)
	}
	var agg string
	switch metric {
	case "avg", "sum", "min", "max":
		agg = metric + "(" + x + ")"
	case "p50":
		agg = "quantileTDigest(0.5)(" + x + ")"
	case "p95":
		agg = "quantileTDigest(0.95)(" + x + ")"
	case "p99":
		agg = "quantileTDigest(0.99)(" + x + ")"
	default:
		return "", nil, fmt.Errorf("unknown metric %q", metric)
	}
	return "toNullable(toFloat64(" + agg + "))", args, nil
}

// requestMetric renders a metric over request counts: requests, failures (status 500 or above),
// the share that failed, response-time percentiles and people.
func requestMetric(metric, of string) (string, []any, error) {
	if of = strings.TrimSpace(of); of != "" && of != "duration_ms" && strings.HasPrefix(metric, "p") && metric != "people" {
		return "", nil, fmt.Errorf("requests measure their response time, not %s", of)
	}
	switch metric {
	case "count":
		return "toNullable(toFloat64(sum(requests)))", nil, nil
	case "failed":
		return "toNullable(toFloat64(sum(errors)))", nil, nil
	case "failure_rate":
		return "if(sum(requests) = 0, NULL, toFloat64(sum(errors)) / sum(requests) * 100)", nil, nil
	case "p50", "p95", "p99":
		i := map[string]string{"p50": "1", "p95": "2", "p99": "3"}[metric]
		return "toNullable(toFloat64(" + latencyMerge + "[" + i + "]))", nil, nil
	case "people":
		return "toNullable(toFloat64(uniqIfMerge(users)))", nil, nil
	}
	return "", nil, fmt.Errorf("unknown metric %q for requests", metric)
}

// groupExpr renders the group key as a string.
func groupExpr(schema *logquery.Schema, groupBy string) (string, []any, error) {
	if strings.TrimSpace(groupBy) == "" {
		return "''", nil, nil
	}
	if err := schema.Check(groupBy); err != nil {
		return "", nil, err
	}
	expr, args, _ := schema.Resolve(groupBy)
	return "toString(" + expr + ")", args, nil
}

// aggregateQueries renders the two statements an aggregate runs: the ranked groups over the
// whole range (with totals) and the per-bucket values for those groups plus "other".
// Placeholders are positional, so args follow the textual order: SELECT expressions first,
// then the WHERE clause, then LIMIT.
func aggregateQueries(src aggSource, where string, whereArgs []any, spec AggregateSpec, secs int, keys []string) (topSQL string, topArgs []any, seriesSQL string, seriesArgs []any, err error) {
	gExpr, gArgs, err := groupExpr(src.schema, spec.GroupBy)
	if err != nil {
		return "", nil, "", nil, err
	}
	mExpr, mArgs, err := src.metric(spec.Metric, spec.Of)
	if err != nil {
		return "", nil, "", nil, err
	}
	order := "value DESC NULLS LAST, g ASC"
	if spec.Sort == "rows" || spec.Metric == "count" {
		order = "rows DESC, g ASC"
	}
	from := " FROM " + src.table + " WHERE " + where
	if spec.GroupBy != "" {
		topSQL = "SELECT " + gExpr + " AS g, " + src.rows + " AS rows, " + mExpr + " AS value" + from +
			" GROUP BY g WITH TOTALS ORDER BY " + order + " LIMIT ?"
		topArgs = append(append(append(append([]any{}, gArgs...), mArgs...), whereArgs...), spec.Limit)
	}
	bucket := "toStartOfInterval(" + src.ts + ", INTERVAL ? SECOND) AS b"
	if spec.GroupBy == "" {
		seriesSQL = "SELECT " + bucket + ", '' AS g, " + src.rows + " AS rows, " + mExpr + " AS value" + from +
			" GROUP BY b, g WITH TOTALS ORDER BY b"
		seriesArgs = append(append([]any{secs}, mArgs...), whereArgs...)
		return topSQL, topArgs, seriesSQL, seriesArgs, nil
	}
	seriesSQL = "SELECT " + bucket + ", if(has(?, " + gExpr + "), " + gExpr + ", '" + otherKey + "') AS g, " + src.rows + " AS rows, " + mExpr +
		" AS value" + from + " GROUP BY b, g WITH TOTALS ORDER BY b"
	seriesArgs = append([]any{secs, nonNilStrings(keys)}, gArgs...)
	seriesArgs = append(seriesArgs, gArgs...)
	seriesArgs = append(seriesArgs, mArgs...)
	seriesArgs = append(seriesArgs, whereArgs...)
	return topSQL, topArgs, seriesSQL, seriesArgs, nil
}

// LogAggregate charts the messages a Logs search selects.
func (c *Client) LogAggregate(ctx context.Context, q LogQuery, spec AggregateSpec) (api.LogAggregate, error) {
	from, to := defaultRange(q.From, q.To, time.Hour)
	q.From, q.To = from, to
	where, args, err := q.where()
	if err != nil {
		return api.LogAggregate{}, err
	}
	return c.aggregate(ctx, messagesSource, from, to, where, args, spec)
}

// ChartQuery is what Analytics → Charts reads: a source ("messages", "errors" or "requests"), a
// project, an optional app and environment, a range and a search in the query language.
type ChartQuery struct {
	Source    string
	ProjectID string
	Service   string
	Env       string
	From      time.Time
	To        time.Time
	Query     logquery.Query
}

// ChartSchema is the query language schema of a chart source.
func ChartSchema(source string) (*logquery.Schema, bool) {
	switch source {
	case "messages":
		return logquery.Logs, true
	case "errors":
		return logquery.Errors, true
	case "requests":
		return logquery.Requests, true
	}
	return nil, false
}

// chartSource picks the table a chart reads and renders its filter.
func chartSource(q ChartQuery, spec AggregateSpec) (aggSource, string, []any, error) {
	if strings.TrimSpace(q.ProjectID) == "" {
		return aggSource{}, "", nil, errors.New("missing project_id")
	}
	var src aggSource
	from, to := q.From, q.To
	switch q.Source {
	case "messages":
		src = messagesSource
	case "errors":
		src = errorsSource
	case "requests":
		src = requestsMinutely
		from = from.Truncate(time.Minute)
		if spec.Metric == "people" || logquery.Requests.Canonical(spec.GroupBy) == "release" || q.Query.Mentions(logquery.Requests, "release") {
			src = requestsHourly
			from = from.Truncate(time.Hour)
		}
	default:
		return aggSource{}, "", nil, fmt.Errorf("unknown source %q: messages, errors or requests", q.Source)
	}
	where := "project_id = ? AND " + src.ts + " >= ? AND " + src.ts + " < ?"
	args := []any{q.ProjectID, from, to}
	if q.Service != "" {
		where += " AND service = ?"
		args = append(args, q.Service)
	}
	if q.Env != "" {
		where += " AND env = ?"
		args = append(args, q.Env)
	}
	cond, cargs, err := q.Query.SQLFor(src.schema)
	if err != nil {
		return aggSource{}, "", nil, err
	}
	if cond != "" {
		where += " AND (" + cond + ")"
		args = append(args, cargs...)
	}
	return src, where, args, nil
}

// ChartAggregate charts messages, errors or request counts.
func (c *Client) ChartAggregate(ctx context.Context, q ChartQuery, spec AggregateSpec) (api.LogAggregate, error) {
	q.From, q.To = defaultRange(q.From, q.To, 24*time.Hour)
	if spec.Metric == "" {
		spec.Metric = "count"
	}
	src, where, args, err := chartSource(q, spec)
	if err != nil {
		return api.LogAggregate{}, err
	}
	return c.aggregate(ctx, src, q.From, q.To, where, args, spec)
}

// aggregate runs a chart: top groups first, then one scan for the per-bucket values of those
// groups with everything else folded into "other".
func (c *Client) aggregate(ctx context.Context, src aggSource, from, to time.Time, where string, args []any, spec AggregateSpec) (api.LogAggregate, error) {
	if spec.Bucket <= 0 {
		spec.Bucket = NiceBucket(from, to, 60)
	}
	switch {
	case src.ts == "hour" && spec.Bucket < time.Hour:
		spec.Bucket = time.Hour // request counts by version or people are kept per hour
	case src.ts == "bucket" && spec.Bucket < time.Minute:
		spec.Bucket = time.Minute
	}
	if spec.Limit <= 0 {
		spec.Limit = 10
	}
	if spec.Limit > 50 {
		spec.Limit = 50
	}
	if spec.Metric == "" {
		spec.Metric = "count"
	}
	secs := int(spec.Bucket / time.Second)
	out := api.LogAggregate{BucketSeconds: secs, GroupBy: spec.GroupBy, Metric: spec.Metric, Of: spec.Of, Groups: []api.AggregateGroup{}}
	if spec.Metric == "count" {
		out.Of = ""
	}

	// Pass 1: ranked groups and the range totals.
	var keys []string
	groups := map[string]*api.AggregateGroup{}
	if spec.GroupBy != "" {
		topSQL, topArgs, _, _, err := aggregateQueries(src, where, args, spec, secs, nil)
		if err != nil {
			return api.LogAggregate{}, err
		}
		rows, err := c.conn.Query(ctx, topSQL, topArgs...)
		if err != nil {
			return api.LogAggregate{}, err
		}
		for rows.Next() {
			var g api.AggregateGroup
			if err := rows.Scan(&g.Key, &g.Rows, &g.Value); err != nil {
				rows.Close()
				return api.LogAggregate{}, err
			}
			g.Value = finite(g.Value)
			keys = append(keys, g.Key)
			gc := g
			groups[g.Key] = &gc
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return api.LogAggregate{}, err
		}
		var tg string
		if err := rows.Totals(&tg, &out.Total.Rows, &out.Total.Value); err != nil {
			out.Total = api.AggregateGroup{} // no totals block: nothing matched
		}
		rows.Close()
		out.Total.Key = "total"
		out.Total.Value = finite(out.Total.Value)
	}

	// Pass 2: per-bucket values for the kept groups (+ other).
	_, _, seriesSQL, seriesArgs, err := aggregateQueries(src, where, args, spec, secs, keys)
	if err != nil {
		return api.LogAggregate{}, err
	}
	rows, err := c.conn.Query(ctx, seriesSQL, seriesArgs...)
	if err != nil {
		return api.LogAggregate{}, err
	}
	defer rows.Close()
	type cell struct {
		rows  uint64
		value *float64
	}
	cells := map[time.Time]map[string]cell{}
	for rows.Next() {
		var b time.Time
		var g string
		var cl cell
		if err := rows.Scan(&b, &g, &cl.rows, &cl.value); err != nil {
			return api.LogAggregate{}, err
		}
		cl.value = finite(cl.value)
		b = b.UTC()
		if cells[b] == nil {
			cells[b] = map[string]cell{}
		}
		cells[b][g] = cl
	}
	if err := rows.Err(); err != nil {
		return api.LogAggregate{}, err
	}
	if spec.GroupBy == "" {
		var tb time.Time
		var tg string
		if err := rows.Totals(&tb, &tg, &out.Total.Rows, &out.Total.Value); err != nil {
			out.Total = api.AggregateGroup{}
		}
		out.Total.Key = "total"
		out.Total.Value = finite(out.Total.Value)
	}

	start := from.Truncate(spec.Bucket)
	for b := start; b.Before(to) && len(out.Buckets) < 5000; b = b.Add(spec.Bucket) {
		out.Buckets = append(out.Buckets, b)
	}
	nb := len(out.Buckets)
	seriesFor := func(key string) []*float64 {
		s := make([]*float64, nb)
		for i, b := range out.Buckets {
			cl, ok := cells[b][key]
			switch {
			case ok:
				s[i] = cl.value
			case additive(spec.Metric):
				zero := 0.0
				s[i] = &zero
			}
		}
		return s
	}
	var other api.AggregateGroup
	hasOther := false
	for _, m := range cells {
		if cl, ok := m[otherKey]; ok {
			hasOther = true
			other.Rows += cl.rows
		}
	}
	if spec.GroupBy == "" {
		out.Total.Series = seriesFor("")
	} else {
		for _, k := range keys {
			g := groups[k]
			g.Series = seriesFor(k)
			out.Groups = append(out.Groups, *g)
		}
		if hasOther {
			other.Key = otherKey
			other.Series = seriesFor(otherKey)
			if spec.Metric == "count" {
				v := float64(other.Rows)
				other.Value = &v
			} else if spec.Metric == "sum" || spec.Metric == "failed" {
				var sum float64
				for _, v := range other.Series {
					if v != nil {
						sum += *v
					}
				}
				other.Value = &sum
			}
			out.Other = &other
		}
		// The per-bucket total row count is the sum over groups (count metric only: for the
		// others the range total is exact and the per-bucket total would need its own scan).
		if spec.Metric == "count" {
			tot := make([]*float64, nb)
			for i, b := range out.Buckets {
				var n float64
				for _, cl := range cells[b] {
					n += float64(cl.rows)
				}
				tot[i] = &n
			}
			out.Total.Series = tot
		}
	}
	if spec.Metric == "count" {
		out.Histogram = countHistogram(out)
	}
	return out, nil
}

// countHistogram reshapes a count aggregate into the stacked timeline the explorer renders.
func countHistogram(a api.LogAggregate) *api.Histogram {
	h := &api.Histogram{BucketSeconds: a.BucketSeconds, GroupBy: a.GroupBy}
	series := a.Groups
	if a.Other != nil {
		series = append(append([]api.AggregateGroup{}, series...), *a.Other)
	}
	if a.GroupBy == "" {
		series = []api.AggregateGroup{{Key: "total", Series: a.Total.Series}}
	}
	for _, g := range series {
		h.Series = append(h.Series, g.Key)
	}
	for i, b := range a.Buckets {
		hb := api.HistogramBucket{TS: b, Counts: map[string]uint64{}}
		for _, g := range series {
			if i < len(g.Series) && g.Series[i] != nil {
				n := uint64(*g.Series[i])
				hb.Counts[g.Key] = n
				hb.Total += n
			}
		}
		h.Buckets = append(h.Buckets, hb)
	}
	return h
}

// finite drops NaN/Inf (an avg over no values) so JSON stays valid.
func finite(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	return v
}
