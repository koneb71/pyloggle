package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/model"
)

// ErrBadAgg marks an aggregation the metric type does not support.
var ErrBadAgg = errors.New("aggregation not supported for this metric type")

// MetricQuery selects one metric for GET /metrics/series.
type MetricQuery struct {
	ProjectID string
	Name      string
	Agg       string // "" = the type's default
	GroupBy   string // "" | service | env | <attribute key>
	Service   string
	Env       string
	From      time.Time
	To        time.Time
	Bucket    time.Duration
	Limit     int // top series kept (default 10, max 50); the rest fold into "other"
}

// MetricAggs lists the aggregations per metric type (first = default).
var MetricAggs = map[string][]string{
	"gauge":     {"avg", "sum", "min", "max", "last"},
	"sum":       {"rate", "avg", "sum", "min", "max", "last"},
	"histogram": {"p95", "p50", "p99", "avg", "count", "rate"},
	"summary":   {"avg", "count", "rate"},
}

// InsertMetricPoints enqueues points; a full queue drops them (counted in the batcher stats).
func (c *Client) InsertMetricPoints(ctx context.Context, points []model.MetricPoint) error {
	var firstErr error
	for _, p := range points {
		if err := c.points.enqueue(ctx, p, 0); err != nil && firstErr == nil && err != errQueueFull {
			firstErr = err
		}
	}
	return firstErr
}

// EnqueueMetricPoints makes *Client usable as an ingest sink.
func (c *Client) EnqueueMetricPoints(ctx context.Context, points []model.MetricPoint) error {
	return c.InsertMetricPoints(ctx, points)
}

func (c *Client) flushPoints(ctx context.Context, points []model.MetricPoint) error {
	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO metrics_points (
			ts, project_id, service, env, name, unit, type, monotonic, temporality, series_hash, attrs,
			value, count, sum, bounds, bucket_counts
		)
	`)
	if err != nil {
		return err
	}
	for _, p := range points {
		bounds, counts := p.Bounds, p.BucketCounts
		if bounds == nil {
			bounds = []float64{}
		}
		if counts == nil {
			counts = []uint64{}
		}
		if err := batch.Append(p.TS.UTC(), p.ProjectID, p.Service, p.Env, p.Name, p.Unit, p.Type, boolToU8(p.Monotonic), p.Temporality, p.SeriesHash, nonNilMap(p.Attrs),
			p.Value, p.Count, p.Sum, bounds, counts); err != nil {
			_ = batch.Abort()
			return err
		}
	}
	return batch.Send()
}

// MetricNames lists the project's metrics from the catalog.
func (c *Client) MetricNames(ctx context.Context, projectID string) ([]api.MetricInfo, error) {
	rows, err := c.conn.Query(ctx, `SELECT name, argMax(type, last_seen), argMax(unit, last_seen), max(monotonic), max(temporality), max(last_seen)
		FROM metrics_catalog WHERE project_id = ? GROUP BY name ORDER BY name LIMIT 2000`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.MetricInfo{}
	for rows.Next() {
		var m api.MetricInfo
		var mono uint8
		if err := rows.Scan(&m.Name, &m.Type, &m.Unit, &mono, &m.Temporality, &m.LastSeen); err != nil {
			return nil, err
		}
		m.Monotonic = mono == 1
		m.LastSeen = m.LastSeen.UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

func (c *Client) metricInfo(ctx context.Context, projectID, name string) (api.MetricInfo, bool, error) {
	rows, err := c.conn.Query(ctx, `SELECT argMax(type, last_seen), argMax(unit, last_seen), max(monotonic), max(temporality), max(last_seen)
		FROM metrics_catalog WHERE project_id = ? AND name = ? GROUP BY name`, projectID, name)
	if err != nil {
		return api.MetricInfo{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return api.MetricInfo{}, false, rows.Err()
	}
	m := api.MetricInfo{Name: name}
	var mono uint8
	if err := rows.Scan(&m.Type, &m.Unit, &mono, &m.Temporality, &m.LastSeen); err != nil {
		return api.MetricInfo{}, false, err
	}
	m.Monotonic = mono == 1
	return m, true, nil
}

func metricGroupExpr(groupBy string) (string, []any) {
	switch strings.TrimSpace(groupBy) {
	case "":
		return "''", nil
	case "service":
		return "service", nil
	case "env":
		return "env", nil
	}
	return "attrs[?]", []any{strings.TrimPrefix(strings.TrimPrefix(groupBy, "attrs."), "@")}
}

// cell is one (bucket, group) aggregate before the final value is computed.
type cell struct {
	n      uint64
	s      float64
	v      float64 // value-based aggregates
	bc     []uint64
	bounds []float64
	has    bool
}

type cellKey struct {
	b time.Time
	g string
}

// cumulativeBaselines returns, per series, the last cumulative sample before from (looking
// back up to lookback), so the first sample inside the window is differenced against what
// came before it. A series without a baseline started inside the window: its first sample
// counts in full instead of being thrown away, which matters for short-lived processes and
// for any window shorter than the export interval.
func (c *Client) cumulativeBaselines(ctx context.Context, projectID, name, service, env string, from time.Time, lookback time.Duration) (map[uint64]cell, error) {
	if lookback <= 0 {
		lookback = time.Hour
	}
	sql := "SELECT series_hash, argMax(value, ts), maxForEach(bucket_counts), any(bounds), max(count), max(sum) FROM metrics_points WHERE project_id = ? AND name = ? AND ts >= ? AND ts < ?"
	args := []any{projectID, name, from.Add(-lookback), from}
	if service != "" {
		sql += " AND service = ?"
		args = append(args, service)
	}
	if env != "" {
		sql += " AND env = ?"
		args = append(args, env)
	}
	rows, err := c.conn.Query(ctx, sql+" GROUP BY series_hash", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint64]cell{}
	for rows.Next() {
		var hash uint64
		var cl cell
		if err := rows.Scan(&hash, &cl.v, &cl.bc, &cl.bounds, &cl.n, &cl.s); err != nil {
			return nil, err
		}
		out[hash] = cl
	}
	return out, rows.Err()
}

// MetricSeries computes one metric over time, split by a group, with the top series kept and
// the rest folded into "other". Cumulative counters and histograms are differenced per series
// in Go (with reset detection) because ClickHouse cannot know where one series' samples end.
func (c *Client) MetricSeries(ctx context.Context, q MetricQuery) (api.MetricSeries, bool, error) {
	if strings.TrimSpace(q.ProjectID) == "" || strings.TrimSpace(q.Name) == "" {
		return api.MetricSeries{}, false, errors.New("missing project_id/name")
	}
	info, ok, err := c.metricInfo(ctx, q.ProjectID, q.Name)
	if err != nil || !ok {
		return api.MetricSeries{}, false, err
	}
	aggs := MetricAggs[info.Type]
	if len(aggs) == 0 {
		aggs = MetricAggs["gauge"]
	}
	agg := strings.ToLower(strings.TrimSpace(q.Agg))
	if agg == "" {
		agg = aggs[0]
		if info.Type == "sum" && !info.Monotonic {
			agg = "avg"
		}
	}
	allowed := false
	for _, a := range aggs {
		allowed = allowed || a == agg
	}
	if !allowed {
		return api.MetricSeries{}, true, fmt.Errorf("%w: %s on %s", ErrBadAgg, agg, info.Type)
	}
	q.From, q.To = defaultRange(q.From, q.To, time.Hour)
	if q.Bucket <= 0 {
		q.Bucket = NiceBucket(q.From, q.To, 60)
	}
	if q.Bucket < time.Minute {
		q.Bucket = time.Minute
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 50 {
		q.Limit = 50
	}
	secs := int(q.Bucket / time.Second)
	gExpr, gArgs := metricGroupExpr(q.GroupBy)
	where := " FROM metrics_points WHERE project_id = ? AND name = ? AND ts >= ? AND ts < ?"
	whereArgs := []any{q.ProjectID, q.Name, q.From, q.To}
	if q.Service != "" {
		where += " AND service = ?"
		whereArgs = append(whereArgs, q.Service)
	}
	if q.Env != "" {
		where += " AND env = ?"
		whereArgs = append(whereArgs, q.Env)
	}
	bucket := "toStartOfInterval(ts, INTERVAL ? SECOND) AS b"
	args := func(extra ...any) []any {
		out := append([]any{secs}, gArgs...)
		out = append(out, extra...)
		return append(out, whereArgs...)
	}
	cumulative := info.Temporality == "cumulative" || (info.Type == "summary")
	cells := map[cellKey]*cell{}
	get := func(b time.Time, g string) *cell {
		k := cellKey{b, g}
		if cl, ok := cells[k]; ok {
			return cl
		}
		cl := &cell{}
		cells[k] = cl
		return cl
	}

	switch {
	case info.Type == "histogram" || info.Type == "summary":
		if cumulative {
			rows, err := c.conn.Query(ctx, "SELECT "+bucket+", "+gExpr+" AS g, series_hash, maxForEach(bucket_counts), any(bounds), max(count), max(sum)"+where+" GROUP BY b, g, series_hash ORDER BY series_hash, b", args()...)
			if err != nil {
				return api.MetricSeries{}, true, err
			}
			base, err := c.cumulativeBaselines(ctx, q.ProjectID, q.Name, q.Service, q.Env, q.From, time.Hour)
			if err != nil {
				rows.Close()
				return api.MetricSeries{}, true, err
			}
			var prevHash uint64
			var prev cell
			first := true
			for rows.Next() {
				var b time.Time
				var g string
				var hash uint64
				var cur cell
				if err := rows.Scan(&b, &g, &hash, &cur.bc, &cur.bounds, &cur.n, &cur.s); err != nil {
					rows.Close()
					return api.MetricSeries{}, true, err
				}
				if first || hash != prevHash {
					first, prevHash, prev = false, hash, base[hash] // zero cell: the series is new
				}
				d := cell{bounds: cur.bounds, bc: diffCounts(cur.bc, prev.bc)}
				if cur.n >= prev.n {
					d.n, d.s = cur.n-prev.n, cur.s-prev.s
				} else {
					d.n, d.s = cur.n, cur.s
				}
				prev = cur
				cl := get(b.UTC(), g)
				cl.n += d.n
				cl.s += d.s
				cl.bc = addCounts(cl.bc, d.bc)
				if len(cl.bounds) == 0 {
					cl.bounds = d.bounds
				}
				cl.has = true
			}
			rows.Close()
		} else {
			rows, err := c.conn.Query(ctx, "SELECT "+bucket+", "+gExpr+" AS g, sumForEach(bucket_counts), any(bounds), sum(count), sum(sum)"+where+" GROUP BY b, g ORDER BY b", args()...)
			if err != nil {
				return api.MetricSeries{}, true, err
			}
			for rows.Next() {
				var b time.Time
				var g string
				var cur cell
				if err := rows.Scan(&b, &g, &cur.bc, &cur.bounds, &cur.n, &cur.s); err != nil {
					rows.Close()
					return api.MetricSeries{}, true, err
				}
				cur.has = true
				*get(b.UTC(), g) = cur
			}
			rows.Close()
		}
	case agg == "rate":
		if cumulative {
			rows, err := c.conn.Query(ctx, "SELECT "+bucket+", "+gExpr+" AS g, series_hash, max(value)"+where+" GROUP BY b, g, series_hash ORDER BY series_hash, b", args()...)
			if err != nil {
				return api.MetricSeries{}, true, err
			}
			base, err := c.cumulativeBaselines(ctx, q.ProjectID, q.Name, q.Service, q.Env, q.From, time.Hour)
			if err != nil {
				rows.Close()
				return api.MetricSeries{}, true, err
			}
			var prevHash uint64
			var prevV float64
			first := true
			for rows.Next() {
				var b time.Time
				var g string
				var hash uint64
				var v float64
				if err := rows.Scan(&b, &g, &hash, &v); err != nil {
					rows.Close()
					return api.MetricSeries{}, true, err
				}
				if first || hash != prevHash {
					first, prevHash, prevV = false, hash, base[hash].v // 0 when the series is new
				}
				d := v - prevV
				if d < 0 {
					d = v
				}
				prevV = v
				cl := get(b.UTC(), g)
				cl.v += d
				cl.has = true
			}
			rows.Close()
		} else {
			rows, err := c.conn.Query(ctx, "SELECT "+bucket+", "+gExpr+" AS g, sum(value)"+where+" GROUP BY b, g ORDER BY b", args()...)
			if err != nil {
				return api.MetricSeries{}, true, err
			}
			for rows.Next() {
				var b time.Time
				var g string
				var v float64
				if err := rows.Scan(&b, &g, &v); err != nil {
					rows.Close()
					return api.MetricSeries{}, true, err
				}
				*get(b.UTC(), g) = cell{v: v, has: true}
			}
			rows.Close()
		}
	default:
		// Value aggregates: average each series inside the bucket (last: its latest sample),
		// then combine the series of a group.
		inner, outer := "avg(value)", agg
		if agg == "last" {
			inner, outer = "argMax(value, ts)", "sum"
		}
		rows, err := c.conn.Query(ctx, "SELECT b, g, "+outer+"(sv) FROM (SELECT "+bucket+", "+gExpr+" AS g, series_hash, "+inner+" AS sv"+where+" GROUP BY b, g, series_hash) GROUP BY b, g ORDER BY b", args()...)
		if err != nil {
			return api.MetricSeries{}, true, err
		}
		for rows.Next() {
			var b time.Time
			var g string
			var v float64
			if err := rows.Scan(&b, &g, &v); err != nil {
				rows.Close()
				return api.MetricSeries{}, true, err
			}
			*get(b.UTC(), g) = cell{v: v, has: true}
		}
		rows.Close()
	}

	// Final values per (bucket, group).
	value := func(cl *cell) (float64, bool) {
		if cl == nil || !cl.has {
			return 0, false
		}
		switch agg {
		case "p50", "p95", "p99":
			qv := map[string]float64{"p50": 0.5, "p95": 0.95, "p99": 0.99}[agg]
			return HistogramQuantile(qv, cl.bounds, cl.bc)
		case "avg":
			if info.Type == "histogram" || info.Type == "summary" {
				if cl.n == 0 {
					return 0, false
				}
				return cl.s / float64(cl.n), true
			}
			return cl.v, true
		case "count":
			return float64(cl.n), true
		case "rate":
			if info.Type == "histogram" || info.Type == "summary" {
				return float64(cl.n) / float64(secs), true
			}
			return cl.v / float64(secs), true
		}
		return cl.v, true
	}
	var buckets []time.Time
	for b := q.From.Truncate(q.Bucket); b.Before(q.To) && len(buckets) < 5000; b = b.Add(q.Bucket) {
		buckets = append(buckets, b)
	}
	groups := map[string][]*float64{}
	for k := range cells {
		if _, ok := groups[k.g]; !ok {
			groups[k.g] = make([]*float64, len(buckets))
		}
	}
	index := map[time.Time]int{}
	for i, b := range buckets {
		index[b] = i
	}
	for k, cl := range cells {
		i, ok := index[k.b]
		if !ok {
			continue
		}
		if v, ok := value(cl); ok {
			vv := v
			groups[k.g][i] = &vv
		}
	}
	// Rank by mean; keep the top q.Limit; fold the rest.
	type ranked struct {
		g     string
		score float64
	}
	var order []ranked
	for g, vals := range groups {
		var sum float64
		var n int
		for _, v := range vals {
			if v != nil {
				sum += *v
				n++
			}
		}
		score := 0.0
		if n > 0 {
			score = sum / float64(n)
		}
		order = append(order, ranked{g, score})
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].score != order[j].score {
			return order[i].score > order[j].score
		}
		return order[i].g < order[j].g
	})
	out := api.MetricSeries{Name: q.Name, Type: info.Type, Unit: info.Unit, Agg: agg, GroupBy: strings.TrimSpace(q.GroupBy), BucketSeconds: secs, Series: []api.MetricSerie{}}
	toPoints := func(vals []*float64) []api.MetricValue {
		pts := make([]api.MetricValue, len(buckets))
		for i, b := range buckets {
			pts[i] = api.MetricValue{TS: b, Value: vals[i]}
		}
		return pts
	}
	additive := agg == "sum" || agg == "count" || agg == "rate"
	var folded [][]*float64
	for i, r := range order {
		if i < q.Limit {
			out.Series = append(out.Series, api.MetricSerie{Label: r.g, Points: toPoints(groups[r.g])})
		} else {
			folded = append(folded, groups[r.g])
		}
	}
	if len(folded) > 0 {
		vals := make([]*float64, len(buckets))
		for i := range buckets {
			var sum float64
			n := 0
			for _, f := range folded {
				if f[i] != nil {
					sum += *f[i]
					n++
				}
			}
			if n > 0 {
				v := sum
				if !additive {
					v = sum / float64(n)
				}
				vals[i] = &v
			}
		}
		out.Other = &api.MetricSerie{Label: "other", Points: toPoints(vals)}
	}
	return out, true, nil
}

// RequestStatsFromOTel builds the dashboard request statistics from the OpenTelemetry HTTP
// server duration histogram (FastAPI native telemetry, contrib instrumentations). ok is false
// when the project has never reported that metric.
func (c *Client) RequestStatsFromOTel(ctx context.Context, projectID, service, env string, from, to time.Time, bucketSeconds, routeLimit int) (api.RequestStats, bool, error) {
	info, ok, err := c.metricInfo(ctx, projectID, model.HTTPServerDuration)
	if err != nil || !ok {
		return api.RequestStats{}, false, err
	}
	from, to = defaultRange(from, to, 24*time.Hour)
	if bucketSeconds < 60 {
		bucketSeconds = int(NiceBucket(from, to, 80) / time.Second)
		if bucketSeconds < 60 {
			bucketSeconds = 60
		}
	}
	if routeLimit <= 0 {
		routeLimit = 10
	}
	scale := 1000.0 // seconds -> ms
	if info.Unit == "ms" {
		scale = 1
	}
	aggBC, aggN, aggS := "sumForEach(bucket_counts)", "sum(count)", "sum(sum)"
	cumulative := info.Temporality == "cumulative"
	if cumulative {
		aggBC, aggN, aggS = "maxForEach(bucket_counts)", "max(count)", "max(sum)"
	}
	sql := "SELECT toStartOfInterval(ts, INTERVAL ? SECOND) AS b, series_hash, any(attrs[?]) AS route, any(attrs[?]) AS method, toUInt16OrZero(any(attrs[?])) AS status, " +
		aggBC + ", any(bounds), " + aggN + ", " + aggS + " FROM metrics_points WHERE project_id = ? AND name = ? AND ts >= ? AND ts < ?"
	args := []any{bucketSeconds, model.AttrHTTPRoute, model.AttrHTTPMethod, model.AttrHTTPStatusCode, projectID, model.HTTPServerDuration, from, to}
	if service != "" {
		sql += " AND service = ?"
		args = append(args, service)
	}
	if env != "" {
		sql += " AND env = ?"
		args = append(args, env)
	}
	sql += " GROUP BY b, series_hash ORDER BY series_hash, b"
	var base map[uint64]cell
	if cumulative {
		if base, err = c.cumulativeBaselines(ctx, projectID, model.HTTPServerDuration, service, env, from, time.Hour); err != nil {
			return api.RequestStats{}, true, err
		}
	}
	rows, err := c.conn.Query(ctx, sql, args...)
	if err != nil {
		return api.RequestStats{}, true, err
	}
	defer rows.Close()

	type sample struct {
		b             time.Time
		route, method string
		status        uint16
		cl            cell
	}
	var samples []sample
	var prevHash uint64
	var prev cell
	first := true
	for rows.Next() {
		var s sample
		var hash uint64
		if err := rows.Scan(&s.b, &hash, &s.route, &s.method, &s.status, &s.cl.bc, &s.cl.bounds, &s.cl.n, &s.cl.s); err != nil {
			return api.RequestStats{}, true, err
		}
		s.b = s.b.UTC()
		if cumulative {
			if first || hash != prevHash {
				first, prevHash, prev = false, hash, base[hash] // zero cell: the series is new
			}
			d := cell{bounds: s.cl.bounds, bc: diffCounts(s.cl.bc, prev.bc)}
			if s.cl.n >= prev.n {
				d.n, d.s = s.cl.n-prev.n, s.cl.s-prev.s
			} else {
				d.n, d.s = s.cl.n, s.cl.s
			}
			prev = s.cl
			s.cl = d
		}
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return api.RequestStats{}, true, err
	}

	merge := func(dst *cell, src cell) {
		dst.n += src.n
		dst.s += src.s
		dst.bc = addCounts(dst.bc, src.bc)
		if len(dst.bounds) == 0 {
			dst.bounds = src.bounds
		}
		dst.has = true
	}
	quant := func(cl cell) (p50, p95, p99 float64) {
		q := func(x float64) float64 {
			v, ok := HistogramQuantile(x, cl.bounds, cl.bc)
			if !ok {
				return 0
			}
			return v * scale
		}
		return q(0.5), q(0.95), q(0.99)
	}
	byBucket := map[time.Time]*api.RequestPoint{}
	bucketCells := map[time.Time]*cell{}
	type routeKey struct{ method, route string }
	routeCells := map[routeKey]*cell{}
	routeErrors := map[routeKey]uint64{}
	var total cell
	var totalErrors uint64
	for _, s := range samples {
		if bucketCells[s.b] == nil {
			bucketCells[s.b] = &cell{}
			byBucket[s.b] = &api.RequestPoint{TS: s.b}
		}
		merge(bucketCells[s.b], s.cl)
		merge(&total, s.cl)
		rk := routeKey{s.method, s.route}
		if routeCells[rk] == nil {
			routeCells[rk] = &cell{}
		}
		merge(routeCells[rk], s.cl)
		if s.status >= 500 {
			byBucket[s.b].Errors += s.cl.n
			routeErrors[rk] += s.cl.n
			totalErrors += s.cl.n
		}
	}
	out := api.RequestStats{BucketSeconds: bucketSeconds, From: from, To: to, Points: []api.RequestPoint{}, Routes: []api.RouteStats{}}
	span := to.Sub(from).Seconds()
	step := time.Duration(bucketSeconds) * time.Second
	for b := from.Truncate(step); b.Before(to) && len(out.Points) < 5000; b = b.Add(step) {
		p := api.RequestPoint{TS: b}
		if bp, ok := byBucket[b]; ok {
			p = *bp
			p.Requests = bucketCells[b].n
			p.P50MS, p.P95MS, p.P99MS = quant(*bucketCells[b])
		}
		out.Points = append(out.Points, p)
	}
	out.Totals = api.RouteStats{Requests: total.n, Errors: totalErrors}
	out.Totals.P50MS, out.Totals.P95MS, out.Totals.P99MS = quant(total)
	if total.n > 0 {
		out.Totals.ErrorPct = float64(totalErrors) / float64(total.n) * 100
	}
	if span > 0 {
		out.Totals.RPS = float64(total.n) / span
	}
	for rk, cl := range routeCells {
		r := api.RouteStats{Method: rk.method, Route: rk.route, Requests: cl.n, Errors: routeErrors[rk]}
		r.P50MS, r.P95MS, r.P99MS = quant(*cl)
		if cl.n > 0 {
			r.ErrorPct = float64(r.Errors) / float64(cl.n) * 100
		}
		if span > 0 {
			r.RPS = float64(cl.n) / span
		}
		out.Routes = append(out.Routes, r)
	}
	sort.Slice(out.Routes, func(i, j int) bool {
		if out.Routes[i].Requests != out.Routes[j].Requests {
			return out.Routes[i].Requests > out.Routes[j].Requests
		}
		return out.Routes[i].Route < out.Routes[j].Route
	})
	if len(out.Routes) > routeLimit {
		out.Routes = out.Routes[:routeLimit]
	}
	return out, true, nil
}
