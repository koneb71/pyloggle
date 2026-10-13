package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// projectTables lists every table that carries project rows, data and configuration alike.
// projects itself keeps the tombstone row.
var projectTables = []string{
	"logs_raw", "error_occurrences", "spans", "metrics_points",
	"issues_state", "issues_activity", "issues_agg", "issue_counts_hourly", "log_counts_minutely",
	"request_stats_minutely", "releases", "metrics_catalog", "project_usage_daily",
	"alert_rules", "alert_firings", "saved_searches", "project_api_keys", "project_api_key_usage",
}

// logTables hold a project's messages and what is built from them alone: the per-minute counts
// and the request statistics. Issues, request timelines, metrics and releases stay.
var logTables = []string{"logs_raw", "log_counts_minutely", "request_stats_minutely"}

// deleteRows removes the rows matching cond: a lightweight DELETE first (they disappear from
// reads at once), then a regular mutation that rewrites the parts and frees the disk.
func (c *Client) deleteRows(ctx context.Context, table, cond string, args ...any) error {
	if err := c.conn.Exec(ctx, "DELETE FROM "+table+" WHERE "+cond, args...); err != nil {
		return fmt.Errorf("%s: %w", table, err)
	}
	if err := c.conn.Exec(ctx, "ALTER TABLE "+table+" DELETE WHERE "+cond, args...); err != nil {
		return fmt.Errorf("%s: %w", table, err)
	}
	return nil
}

// PurgeProjectData removes every row of a project. Call MarkProjectDeleted before it so new
// data and reads are already refused.
func (c *Client) PurgeProjectData(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("missing project_id")
	}
	var firstErr error
	for _, t := range projectTables {
		exists, err := c.tableExists(ctx, t)
		if err != nil || !exists {
			continue
		}
		if err := c.deleteRows(ctx, t, "project_id = ?", projectID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ClearLogs removes every message of a project, with the counts and request statistics built
// from them. The lightweight deletes wait until the rows are hidden, which on a large project
// takes longer than the time limit meant for UI reads, so that limit is lifted here.
func (c *Client) ClearLogs(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("missing project_id")
	}
	ctx = ch.Context(ctx, ch.WithSettings(ch.Settings{"max_execution_time": 600}))
	for _, t := range logTables {
		exists, err := c.tableExists(ctx, t)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := c.deleteRows(ctx, t, "project_id = ?", projectID); err != nil {
			return err
		}
	}
	return nil
}

// PurgeUser removes the log lines and error occurrences of one user (by id hash and/or e-mail)
// from a project. Aggregates keep their HyperLogLog user counts (they cannot shrink); spans
// and metrics carry no user.
func (c *Client) PurgeUser(ctx context.Context, projectID, userIDHash, userEmail string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("missing project_id")
	}
	userIDHash, userEmail = strings.TrimSpace(userIDHash), strings.TrimSpace(userEmail)
	if userIDHash == "" && userEmail == "" {
		return errors.New("user_id_hash or user_email is required")
	}
	if userIDHash != "" {
		if err := c.deleteRows(ctx, "logs_raw", "project_id = ? AND (user_id_hash = ?)", projectID, userIDHash); err != nil {
			return err
		}
	}
	var conds []string
	args := []any{projectID}
	if userIDHash != "" {
		conds, args = append(conds, "user_id_hash = ?"), append(args, userIDHash)
	}
	if userEmail != "" {
		conds, args = append(conds, "user_email = ?"), append(args, userEmail)
	}
	return c.deleteRows(ctx, "error_occurrences", "project_id = ? AND ("+strings.Join(conds, " OR ")+")", args...)
}
