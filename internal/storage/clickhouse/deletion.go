package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// projectTables lists every table that carries project rows, data and configuration alike.
// projects itself keeps the tombstone row.
var projectTables = []string{
	"logs_raw", "error_occurrences", "spans", "metrics_points",
	"issues_state", "issues_activity", "issues_agg", "issue_counts_hourly", "log_counts_minutely",
	"request_stats_minutely", "releases", "metrics_catalog", "project_usage_daily",
	"alert_rules", "alert_firings", "saved_searches", "project_api_keys", "project_api_key_usage",
}

// PurgeProjectData removes every row of a project: a lightweight DELETE first (rows disappear
// from reads at once), then a regular mutation that rewrites the parts and frees the disk.
// Call MarkProjectDeleted before it so new data and reads are already refused.
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
		if err := c.conn.Exec(ctx, "DELETE FROM "+t+" WHERE project_id = ?", projectID); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", t, err)
		}
		if err := c.conn.Exec(ctx, "ALTER TABLE "+t+" DELETE WHERE project_id = ?", projectID); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", t, err)
		}
	}
	return firstErr
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
	run := func(table, cond string, args ...any) error {
		all := append([]any{projectID}, args...)
		if err := c.conn.Exec(ctx, "DELETE FROM "+table+" WHERE project_id = ? AND ("+cond+")", all...); err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
		if err := c.conn.Exec(ctx, "ALTER TABLE "+table+" DELETE WHERE project_id = ? AND ("+cond+")", all...); err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
		return nil
	}
	if userIDHash != "" {
		if err := run("logs_raw", "user_id_hash = ?", userIDHash); err != nil {
			return err
		}
	}
	var conds []string
	var args []any
	if userIDHash != "" {
		conds, args = append(conds, "user_id_hash = ?"), append(args, userIDHash)
	}
	if userEmail != "" {
		conds, args = append(conds, "user_email = ?"), append(args, userEmail)
	}
	return run("error_occurrences", strings.Join(conds, " OR "), args...)
}
