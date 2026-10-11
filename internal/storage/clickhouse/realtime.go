package clickhouse

import (
	"context"
	"errors"
	"strings"
	"time"
)

// IssuesChangedAt returns a timestamp that advances when the issues list should change,
// either because new occurrences arrived (issues_stats.last_seen) or because issue state
// changed (issues_state.updated_at).
func (c *Client) IssuesChangedAt(ctx context.Context, projectID, service, env string) (time.Time, error) {
	if strings.TrimSpace(projectID) == "" {
		return time.Time{}, errors.New("missing project_id")
	}
	qStats := `
		SELECT max(last_seen) as last_seen
		FROM issues_stats
		WHERE project_id = ?
	`
	argsStats := make([]any, 0, 3)
	argsStats = append(argsStats, projectID)
	if service != "" {
		qStats += " AND service = ?\n"
		argsStats = append(argsStats, service)
	}
	if env != "" {
		qStats += " AND env = ?\n"
		argsStats = append(argsStats, env)
	}
	var lastSeen time.Time
	if err := c.conn.QueryRow(ctx, qStats, argsStats...).Scan(&lastSeen); err != nil {
		return time.Time{}, err
	}

	qState := `
		SELECT max(updated_at) as updated_at
		FROM issues_state
		WHERE project_id = ?
	`
	argsState := make([]any, 0, 3)
	argsState = append(argsState, projectID)
	if service != "" {
		qState += " AND service = ?\n"
		argsState = append(argsState, service)
	}
	if env != "" {
		qState += " AND env = ?\n"
		argsState = append(argsState, env)
	}
	var updatedAt time.Time
	if err := c.conn.QueryRow(ctx, qState, argsState...).Scan(&updatedAt); err != nil {
		return time.Time{}, err
	}

	if updatedAt.After(lastSeen) {
		return updatedAt, nil
	}
	return lastSeen, nil
}

// LogsChangedAt returns max(ts) within the logs list filter window.
func (c *Client) LogsChangedAt(ctx context.Context, projectID, service, env, level, q, traceID, requestID string, from, to time.Time) (time.Time, error) {
	if strings.TrimSpace(projectID) == "" {
		return time.Time{}, errors.New("missing project_id")
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-1 * time.Hour)
	}
	qry := `
		SELECT max(ts) as ts
		FROM logs_raw
		WHERE project_id = ?
		  AND ts >= ?
		  AND ts < ?
	`
	args := make([]any, 0, 10)
	args = append(args, projectID, from.UTC(), to.UTC())
	if service != "" {
		qry += " AND service = ?\n"
		args = append(args, service)
	}
	if env != "" {
		qry += " AND env = ?\n"
		args = append(args, env)
	}
	if level != "" {
		qry += " AND level = ?\n"
		args = append(args, level)
	}
	if traceID != "" {
		qry += " AND trace_id = ?\n"
		args = append(args, traceID)
	}
	if requestID != "" {
		qry += " AND request_id = ?\n"
		args = append(args, requestID)
	}
	if q != "" {
		qry += " AND positionCaseInsensitiveUTF8(message, ?) > 0\n"
		args = append(args, q)
	}
	var maxTS time.Time
	if err := c.conn.QueryRow(ctx, qry, args...).Scan(&maxTS); err != nil {
		return time.Time{}, err
	}
	return maxTS, nil
}

// DashboardsChangedAt returns max(ts) within the dashboards filter window.
func (c *Client) DashboardsChangedAt(ctx context.Context, projectID, service, env string, from, to time.Time) (time.Time, error) {
	if strings.TrimSpace(projectID) == "" {
		return time.Time{}, errors.New("missing project_id")
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-1 * time.Hour)
	}
	qry := `
		SELECT max(ts) as ts
		FROM error_occurrences
		WHERE project_id = ?
		  AND ts >= ?
		  AND ts < ?
	`
	args := make([]any, 0, 6)
	args = append(args, projectID, from.UTC(), to.UTC())
	if service != "" {
		qry += " AND service = ?\n"
		args = append(args, service)
	}
	if env != "" {
		qry += " AND env = ?\n"
		args = append(args, env)
	}
	var maxTS time.Time
	if err := c.conn.QueryRow(ctx, qry, args...).Scan(&maxTS); err != nil {
		return time.Time{}, err
	}
	return maxTS, nil
}
