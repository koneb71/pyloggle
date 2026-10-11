package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/logquery"
)

// AggregateSpec is an analytics query over the explorer selection: a metric over a field,
// split by a group-by field, bucketed over time. Field names are the query language's.
type AggregateSpec struct {
	GroupBy string        // "" = no grouping
	Metric  string        // count | avg | sum | min | max | p50 | p95 | p99
	Of      string        // measured field for every metric but count (duration_ms, http.status, attrs.<k>)
	Bucket  time.Duration // 0 = NiceBucket(from, to, 60)
	Limit   int           // top groups kept (default 10, max 50); the rest fold into "other"
	Sort    string        // value (default) | rows
}

// AggregateMetrics lists the supported metrics in menu order.
var AggregateMetrics = []string{"count", "avg", "sum", "min", "max", "p50", "p95", "p99"}

const otherKey = "other"

// metricExpr renders the aggregate for a metric over a field. Unset numeric columns are stored
// as 0 (duration_ms, http_status), so they become NULL and no longer drag averages down; map
// values are parsed leniently. The result is always Nullable(Float64) so scanning is uniform.
func metricExpr(metric, of string) (string, []any, error) {
	if metric == "count" {
		return "toNullable(toFloat64(count()))", nil, nil
	}
	if strings.TrimSpace(of) == "" {
		return "", nil, errors.New("metric needs a field (of=...)")
	}
	expr, args, kind := logquery.ResolveField(of)
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

// groupExpr renders the group key as a string.
func groupExpr(groupBy string) (string, []any) {
	if strings.TrimSpace(groupBy) == "" {
		return "''", nil
	}
	expr, args, _ := logquery.ResolveField(groupBy)
	return "toString(" + expr + ")", args
}

// aggregateQueries renders the two statements LogAggregate runs: the ranked groups over the
// whole range (with totals) and the per-bucket values for those groups plus "other".
// Placeholders are positional, so args follow the textual order: SELECT expressions first,
// then the WHERE clause, then LIMIT.
func aggregateQueries(where string, whereArgs []any, spec AggregateSpec, secs int, keys []string) (topSQL string, topArgs []any, seriesSQL string, seriesArgs []any, err error) {
	gExpr, gArgs := groupExpr(spec.GroupBy)
	mExpr, mArgs, err := metricExpr(spec.Metric, spec.Of)
	if err != nil {
		return "", nil, "", nil, err
	}
	order := "value DESC NULLS LAST, g ASC"
	if spec.Sort == "rows" || spec.Metric == "count" {
		order = "rows DESC, g ASC"
	}
	if spec.GroupBy != "" {
		topSQL = "SELECT " + gExpr + " AS g, count() AS rows, " + mExpr + " AS value FROM logs_raw WHERE " + where +
			" GROUP BY g WITH TOTALS ORDER BY " + order + " LIMIT ?"
		topArgs = append(append(append(append([]any{}, gArgs...), mArgs...), whereArgs...), spec.Limit)
	}
	bucket := "toStartOfInterval(ts, INTERVAL ? SECOND) AS b"
	if spec.GroupBy == "" {
		seriesSQL = "SELECT " + bucket + ", '' AS g, count() AS rows, " + mExpr + " AS value FROM logs_raw WHERE " + where +
			" GROUP BY b, g WITH TOTALS ORDER BY b"
		seriesArgs = append(append([]any{secs}, mArgs...), whereArgs...)
		return topSQL, topArgs, seriesSQL, seriesArgs, nil
	}
	seriesSQL = "SELECT " + bucket + ", if(has(?, " + gExpr + "), " + gExpr + ", '" + otherKey + "') AS g, count() AS rows, " + mExpr +
		" AS value FROM logs_raw WHERE " + where + " GROUP BY b, g WITH TOTALS ORDER BY b"
	seriesArgs = append([]any{secs, nonNilStrings(keys)}, gArgs...)
	seriesArgs = append(seriesArgs, gArgs...)
	seriesArgs = append(seriesArgs, mArgs...)
	seriesArgs = append(seriesArgs, whereArgs...)
	return topSQL, topArgs, seriesSQL, seriesArgs, nil
}

// LogAggregate runs the analytics query: top groups first, then one scan for the per-bucket
// values of those groups with everything else folded into "other".
func (c *Client) LogAggregate(ctx context.Context, q LogQuery, spec AggregateSpec) (api.LogAggregate, error) {
	from, to := defaultRange(q.From, q.To, time.Hour)
	q.From, q.To = from, to
	if spec.Bucket <= 0 {
		spec.Bucket = NiceBucket(from, to, 60)
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
	where, args, err := q.where()
	if err != nil {
		return api.LogAggregate{}, err
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
		topSQL, topArgs, _, _, err := aggregateQueries(where, args, spec, secs, nil)
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
	_, _, seriesSQL, seriesArgs, err := aggregateQueries(where, args, spec, secs, keys)
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
			case spec.Metric == "count":
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
			} else if spec.Metric == "sum" {
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
