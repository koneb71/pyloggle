package clickhouse

import (
	"context"
	"errors"
	"strings"
	"time"

	"pyloggle/internal/api"
)

type errorTrendRow struct {
	Bucket time.Time `ch:"bucket"`
	Count  uint64    `ch:"cnt"`
}

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
	if bucketSeconds > 86400 {
		bucketSeconds = 86400
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-1 * time.Hour)
	}

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
	args = append(args, bucketSeconds, projectID, from.UTC(), to.UTC())
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

	out := make([]api.TimeSeriesPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, api.TimeSeriesPoint{TS: r.Bucket, Count: r.Count})
	}
	return out, nil
}
