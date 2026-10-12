package clickhouse

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
)

type errorTrendRow struct {
	Bucket time.Time `ch:"bucket"`
	Count  uint64    `ch:"cnt"`
}

// ErrorTrend counts error occurrences per bucket (optionally scoped by service/env/route/method).
func (c *Client) ErrorTrend(ctx context.Context, projectID, service, env, httpRoute, httpMethod string, from, to time.Time, bucketSeconds int) ([]api.TimeSeriesPoint, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("missing project_id")
	}
	if bucketSeconds <= 0 {
		bucketSeconds = 60
	}
	if bucketSeconds < 10 {
		bucketSeconds = 10
	}
	if bucketSeconds > 86400*7 {
		bucketSeconds = 86400 * 7
	}
	from, to = defaultRange(from, to, time.Hour)

	q := `
		SELECT
			toStartOfInterval(ts, INTERVAL ? SECOND) as bucket,
			count() as cnt
		FROM error_occurrences
		WHERE project_id = ?
		  AND ts >= ?
		  AND ts < ?
	`
	var rows []errorTrendRow
	args := make([]any, 0, 8)
	args = append(args, bucketSeconds, projectID, from, to)
	if service != "" {
		q += " AND service = ?\n"
		args = append(args, service)
	}
	if env != "" {
		q += " AND env = ?\n"
		args = append(args, env)
	}
	if httpRoute != "" {
		q += " AND http_route = ?\n"
		args = append(args, httpRoute)
	}
	if httpMethod != "" {
		q += " AND http_method = ?\n"
		args = append(args, httpMethod)
	}
	q += " GROUP BY bucket\n ORDER BY bucket ASC\n"

	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	got := make(map[time.Time]uint64, len(rows))
	for _, r := range rows {
		got[r.Bucket.UTC()] = r.Count
	}
	return fillSeries(from, to, time.Duration(bucketSeconds)*time.Second, got), nil
}

// LogVolumeByLevel reads the pre-aggregated minutely counts (cheap for long ranges).
func (c *Client) LogVolumeByLevel(ctx context.Context, projectID, service, env string, from, to time.Time, bucketSeconds int) (api.Histogram, error) {
	if strings.TrimSpace(projectID) == "" {
		return api.Histogram{}, errors.New("missing project_id")
	}
	from, to = defaultRange(from, to, 24*time.Hour)
	if bucketSeconds < 60 {
		bucketSeconds = int(NiceBucket(from, to, 80) / time.Second)
		if bucketSeconds < 60 {
			bucketSeconds = 60
		}
	}
	q := `SELECT toStartOfInterval(bucket, INTERVAL ? SECOND) AS b, level, sum(cnt) AS n
		FROM log_counts_minutely WHERE project_id = ? AND bucket >= ? AND bucket < ?`
	args := []any{bucketSeconds, projectID, from, to}
	if service != "" {
		q += " AND service = ?"
		args = append(args, service)
	}
	if env != "" {
		q += " AND env = ?"
		args = append(args, env)
	}
	q += " GROUP BY b, level ORDER BY b"
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return api.Histogram{}, err
	}
	defer rows.Close()
	byBucket := map[time.Time]map[string]uint64{}
	seen := map[string]bool{}
	for rows.Next() {
		var b time.Time
		var level string
		var n uint64
		if err := rows.Scan(&b, &level, &n); err != nil {
			return api.Histogram{}, err
		}
		b = b.UTC()
		if byBucket[b] == nil {
			byBucket[b] = map[string]uint64{}
		}
		byBucket[b][level] += n
		seen[level] = true
	}
	h := api.Histogram{BucketSeconds: bucketSeconds, GroupBy: "level"}
	for _, l := range []string{"debug", "info", "warn", "error", "fatal"} {
		if seen[l] {
			h.Series = append(h.Series, l)
			delete(seen, l)
		}
	}
	for l := range seen {
		h.Series = append(h.Series, l)
	}
	step := time.Duration(bucketSeconds) * time.Second
	for b := from.Truncate(step); b.Before(to) && len(h.Buckets) < 5000; b = b.Add(step) {
		counts := byBucket[b]
		if counts == nil {
			counts = map[string]uint64{}
		}
		var total uint64
		for _, n := range counts {
			total += n
		}
		h.Buckets = append(h.Buckets, api.HistogramBucket{TS: b, Total: total, Counts: counts})
	}
	return h, rows.Err()
}

// TopIssues ranks issues by occurrences in the range (hourly counts) and returns their summaries
// with OccurrenceCount replaced by the in-range count.
func (c *Client) TopIssues(ctx context.Context, projectID, service, env string, from, to time.Time, limit int) ([]api.IssueSummary, error) {
	if limit <= 0 {
		limit = 10
	}
	from, to = defaultRange(from, to, 24*time.Hour)
	// Count people over the same hours as the occurrences: the issue's own affected_users is
	// all time, which reads as more people than times when the range is short.
	q := `SELECT issue_id, sum(cnt) AS n, uniqIfMerge(users) AS u FROM issue_counts_hourly WHERE project_id = ? AND bucket >= ? AND bucket < ?`
	args := []any{projectID, from.Truncate(time.Hour), to}
	if service != "" {
		q += " AND service = ?"
		args = append(args, service)
	}
	if env != "" {
		q += " AND env = ?"
		args = append(args, env)
	}
	q += " GROUP BY issue_id ORDER BY n DESC LIMIT ?"
	args = append(args, limit)
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	counts, people := map[string]uint64{}, map[string]uint64{}
	for rows.Next() {
		var id string
		var n, u uint64
		if err := rows.Scan(&id, &n, &u); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		counts[id], people[id] = n, u
	}
	rows.Close()
	if len(ids) == 0 {
		return []api.IssueSummary{}, nil
	}
	summaries, err := c.QueryIssues(ctx, IssueListQuery{ProjectID: projectID, IssueIDs: ids, Limit: len(ids)})
	if err != nil {
		return nil, err
	}
	byID := map[string]api.IssueSummary{}
	for _, s := range summaries {
		byID[s.IssueID] = s
	}
	out := make([]api.IssueSummary, 0, len(ids))
	for _, id := range ids {
		s, ok := byID[id]
		if !ok {
			continue
		}
		s.OccurrenceCount, s.AffectedUsers = counts[id], people[id]
		out = append(out, s)
	}
	return out, nil
}

// Overview computes the dashboard stat tiles for a range.
func (c *Client) Overview(ctx context.Context, projectID, service, env string, from, to time.Time) (api.Overview, error) {
	if strings.TrimSpace(projectID) == "" {
		return api.Overview{}, errors.New("missing project_id")
	}
	from, to = defaultRange(from, to, 24*time.Hour)
	var o api.Overview
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
	q, args := scope(`SELECT sum(cnt), uniq(service) FROM log_counts_minutely WHERE project_id = ? AND bucket >= ? AND bucket < ?`, []any{projectID, from.Truncate(time.Minute), to})
	if err := c.conn.QueryRow(ctx, q, args...).Scan(&o.Events, &o.Services); err != nil {
		return o, err
	}
	q, args = scope(`SELECT count(), uniqIf(user_id_hash, user_id_hash != '') FROM error_occurrences WHERE project_id = ? AND ts >= ? AND ts < ?`, []any{projectID, from, to})
	if err := c.conn.QueryRow(ctx, q, args...).Scan(&o.Errors, &o.AffectedUsers); err != nil {
		return o, err
	}
	counts, err := c.CountIssuesByStatus(ctx, IssueListQuery{ProjectID: projectID, Service: service, Env: env, From: from, To: to})
	if err != nil {
		return o, err
	}
	o.OpenIssues = counts["open"]
	q, args = scope(`SELECT count() FROM (SELECT issue_id, min(first_seen) AS fs FROM issues_agg WHERE project_id = ?`, []any{projectID})
	q += ` GROUP BY issue_id) WHERE fs >= ? AND fs < ?`
	args = append(args, from, to)
	if err := c.conn.QueryRow(ctx, q, args...).Scan(&o.NewIssues); err != nil {
		return o, err
	}
	return o, nil
}
