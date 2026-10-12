package clickhouse

import (
	"context"
	"errors"
	"sort"
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

// ServiceHealth summarizes each app over a range from the pre-aggregated tables: requests,
// failures and latency (request_stats_minutely), error occurrences (issue_counts_hourly), and
// log volume and last activity (log_counts_minutely). Status is classified by ClassifyService.
func (c *Client) ServiceHealth(ctx context.Context, projectID, env string, from, to time.Time, limit int) ([]api.ServiceHealth, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("missing project_id")
	}
	from, to = defaultRange(from, to, 24*time.Hour)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	byService := map[string]*api.ServiceHealth{}
	get := func(s string) *api.ServiceHealth {
		h := byService[s]
		if h == nil {
			h = &api.ServiceHealth{Service: s, Envs: []string{}}
			byService[s] = h
		}
		return h
	}
	envClause := ""
	envArgs := []any{}
	if env != "" {
		envClause = " AND env = ?"
		envArgs = append(envArgs, env)
	}

	q := `SELECT service, sum(cnt), max(bucket), groupUniqArray(8)(env) FROM log_counts_minutely
		WHERE project_id = ? AND bucket >= ? AND bucket < ?` + envClause + ` GROUP BY service`
	rows, err := c.conn.Query(ctx, q, append([]any{projectID, from.Truncate(time.Minute), to}, envArgs...)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var svc string
		var n uint64
		var last time.Time
		var envs []string
		if err := rows.Scan(&svc, &n, &last, &envs); err != nil {
			rows.Close()
			return nil, err
		}
		h := get(svc)
		h.Logs, h.LastSeen = n, last.UTC()
		h.Envs = append(h.Envs, envs...)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	q = `SELECT service, sum(requests), sum(errors), ` + latencyMerge + ` FROM request_stats_minutely
		WHERE project_id = ? AND bucket >= ? AND bucket < ?` + envClause + ` GROUP BY service`
	rows, err = c.conn.Query(ctx, q, append([]any{projectID, from.Truncate(time.Minute), to}, envArgs...)...)
	if err != nil {
		return nil, err
	}
	minutes := to.Sub(from).Minutes()
	for rows.Next() {
		var svc string
		var req, failed uint64
		var qs []float32
		if err := rows.Scan(&svc, &req, &failed, &qs); err != nil {
			rows.Close()
			return nil, err
		}
		h := get(svc)
		h.Requests, h.Failed = req, failed
		h.P50MS, h.P95MS, _ = q3(qs)
		if req > 0 {
			h.FailedPct = float64(failed) / float64(req) * 100
		}
		if minutes > 0 {
			h.RPM = float64(req) / minutes
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	q = `SELECT service, sum(cnt) FROM issue_counts_hourly
		WHERE project_id = ? AND bucket >= ? AND bucket < ?` + envClause + ` GROUP BY service`
	rows, err = c.conn.Query(ctx, q, append([]any{projectID, from.Truncate(time.Hour), to}, envArgs...)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var svc string
		var n uint64
		if err := rows.Scan(&svc, &n); err != nil {
			rows.Close()
			return nil, err
		}
		get(svc).Errors = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]api.ServiceHealth, 0, len(byService))
	for _, h := range byService {
		h.Status = ClassifyService(*h, to)
		out = append(out, *h)
	}
	SortServiceHealth(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Health thresholds: enough traffic to judge, and the failure shares that count as a problem.
const (
	healthMinRequests = 20
	healthFailingPct  = 5.0
	healthDegradedPct = 1.0
	healthSilentAfter = time.Hour
)

// ClassifyService turns the numbers into one word. "silent" comes first: an app that sent
// nothing in the last hour of the range can't be called failing or fine right now. Then
// "failing" when 5% or more of at least 20 requests failed, "degraded" at 1% or with any error
// occurrences, otherwise "ok".
func ClassifyService(h api.ServiceHealth, rangeEnd time.Time) string {
	judged := h.Requests >= healthMinRequests
	switch {
	case h.LastSeen.IsZero() || rangeEnd.Sub(h.LastSeen) > healthSilentAfter:
		return "silent"
	case judged && h.FailedPct >= healthFailingPct:
		return "failing"
	case (judged && h.FailedPct >= healthDegradedPct) || h.Errors > 0:
		return "degraded"
	}
	return "ok"
}

var healthRank = map[string]int{"failing": 0, "degraded": 1, "silent": 2, "ok": 3}

// SortServiceHealth puts problems first, then the busiest apps.
func SortServiceHealth(list []api.ServiceHealth) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if healthRank[a.Status] != healthRank[b.Status] {
			return healthRank[a.Status] < healthRank[b.Status]
		}
		if a.Requests+a.Logs != b.Requests+b.Logs {
			return a.Requests+a.Logs > b.Requests+b.Logs
		}
		return a.Service < b.Service
	})
}
