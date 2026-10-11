package clickhouse

import (
	"context"
	"errors"
	"strings"
	"time"

	"pyloggle/internal/api"
)

const latencyMerge = "quantilesTDigestIfMerge(0.5, 0.95, 0.99)(latency)"

// RequestStats reads request_stats_minutely: traffic and latency per bucket, the range totals
// and the busiest routes. Pre-aggregated, so it stays cheap over long ranges.
func (c *Client) RequestStats(ctx context.Context, projectID, service, env string, from, to time.Time, bucketSeconds, routeLimit int) (api.RequestStats, error) {
	if strings.TrimSpace(projectID) == "" {
		return api.RequestStats{}, errors.New("missing project_id")
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
	if routeLimit > 100 {
		routeLimit = 100
	}
	scope := func(q string, args []any) (string, []any) {
		if service != "" {
			q += " AND service = ?"
			args = append(args, service)
		}
		if env != "" {
			q += " AND env = ?"
			args = append(args, env)
		}
		return q, args
	}
	out := api.RequestStats{BucketSeconds: bucketSeconds, From: from, To: to, Points: []api.RequestPoint{}, Routes: []api.RouteStats{}}
	base := " FROM request_stats_minutely WHERE project_id = ? AND bucket >= ? AND bucket < ?"
	baseArgs := []any{projectID, from.Truncate(time.Minute), to}
	span := to.Sub(from).Seconds()

	// Totals.
	q, args := scope("SELECT sum(requests), sum(errors), "+latencyMerge+base, baseArgs)
	var qs []float32
	if err := c.conn.QueryRow(ctx, q, args...).Scan(&out.Totals.Requests, &out.Totals.Errors, &qs); err != nil {
		return out, err
	}
	out.Totals = fillRoute(out.Totals, qs, span)

	// Per bucket.
	q, args = scope("SELECT toStartOfInterval(bucket, INTERVAL ? SECOND) AS b, sum(requests), sum(errors), "+latencyMerge+base, append([]any{bucketSeconds}, baseArgs...))
	q += " GROUP BY b ORDER BY b"
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return out, err
	}
	got := map[time.Time]api.RequestPoint{}
	for rows.Next() {
		var p api.RequestPoint
		var qs []float32
		if err := rows.Scan(&p.TS, &p.Requests, &p.Errors, &qs); err != nil {
			rows.Close()
			return out, err
		}
		p.TS = p.TS.UTC()
		p.P50MS, p.P95MS, p.P99MS = q3(qs)
		got[p.TS] = p
	}
	rows.Close()
	step := time.Duration(bucketSeconds) * time.Second
	for b := from.Truncate(step); b.Before(to) && len(out.Points) < 5000; b = b.Add(step) {
		p, ok := got[b]
		if !ok {
			p = api.RequestPoint{TS: b}
		}
		out.Points = append(out.Points, p)
	}

	// Busiest routes.
	q, args = scope("SELECT http_method, http_route, sum(requests) AS n, sum(errors), "+latencyMerge+base, baseArgs)
	q += " GROUP BY http_method, http_route ORDER BY n DESC, http_route ASC LIMIT ?"
	args = append(args, routeLimit)
	rows, err = c.conn.Query(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r api.RouteStats
		var qs []float32
		if err := rows.Scan(&r.Method, &r.Route, &r.Requests, &r.Errors, &qs); err != nil {
			return out, err
		}
		out.Routes = append(out.Routes, fillRoute(r, qs, span))
	}
	return out, rows.Err()
}

func fillRoute(r api.RouteStats, qs []float32, spanSeconds float64) api.RouteStats {
	r.P50MS, r.P95MS, r.P99MS = q3(qs)
	if r.Requests > 0 {
		r.ErrorPct = float64(r.Errors) / float64(r.Requests) * 100
	}
	if spanSeconds > 0 {
		r.RPS = float64(r.Requests) / spanSeconds
	}
	return r
}

// q3 unpacks the p50/p95/p99 array; an empty digest yields NaN, which becomes 0.
func q3(qs []float32) (p50, p95, p99 float64) {
	get := func(i int) float64 {
		if i >= len(qs) || qs[i] != qs[i] {
			return 0
		}
		return float64(qs[i])
	}
	return get(0), get(1), get(2)
}
