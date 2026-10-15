package clickhouse

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// retentionTarget maps a table to its TTL expression base and the configured retention.
type retentionTarget struct {
	Table  string
	Expr   func(days int) string
	Days   func(opts MigrateOptions) int
	Exists bool
}

func retentionTargets(opts MigrateOptions) []retentionTarget {
	return []retentionTarget{
		{Table: "logs_raw", Expr: func(d int) string { return fmt.Sprintf("toDate(ts) + INTERVAL %d DAY", d) }, Days: func(o MigrateOptions) int { return o.RetentionLogsDays }},
		{Table: "error_occurrences", Expr: func(d int) string { return fmt.Sprintf("toDate(ts) + INTERVAL %d DAY", d) }, Days: func(o MigrateOptions) int { return o.RetentionErrorsDays }},
		{Table: "issue_counts_hourly", Expr: func(d int) string { return fmt.Sprintf("bucket + INTERVAL %d DAY", d) }, Days: func(o MigrateOptions) int { return o.RetentionStatsDays }},
		{Table: "request_stats_minutely", Expr: func(d int) string { return fmt.Sprintf("bucket + INTERVAL %d DAY", d) }, Days: func(o MigrateOptions) int { return o.RetentionStatsDays }},
		{Table: "request_usage_hourly", Expr: func(d int) string { return fmt.Sprintf("hour + INTERVAL %d DAY", d) }, Days: func(o MigrateOptions) int { return o.RetentionStatsDays }},
		// Tables that arrive with 0.3; ApplyRetention skips them until they exist.
		{Table: "spans", Expr: func(d int) string { return fmt.Sprintf("toDate(start_ts) + INTERVAL %d DAY", d) }, Days: func(o MigrateOptions) int { return o.RetentionSpansDays }},
		{Table: "metrics_points", Expr: func(d int) string { return fmt.Sprintf("toDate(ts) + INTERVAL %d DAY", d) }, Days: func(o MigrateOptions) int { return o.RetentionMetricsDays }},
	}
}

// ApplyRetention makes the live TTLs match the configured retention days. The applied value is
// remembered in pyloggle_settings so the ALTER only runs when the configuration changes.
func (c *Client) ApplyRetention(ctx context.Context, opts MigrateOptions) error {
	opts = opts.withDefaults()
	for _, t := range retentionTargets(opts) {
		exists, err := c.tableExists(ctx, t.Table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		days := t.Days(opts)
		key := "retention_days." + t.Table
		if cur, _ := c.GetSetting(ctx, key); cur == strconv.Itoa(days) {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s MODIFY TTL %s", t.Table, t.Expr(days))
		if err := c.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
		if err := c.SetSetting(ctx, key, strconv.Itoa(days)); err != nil {
			return err
		}
		opts.logf("retention: %s keeps %d days", t.Table, days)
	}
	return nil
}

// GetSetting reads a value from pyloggle_settings ("" when unset).
func (c *Client) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := c.conn.QueryRow(ctx, "SELECT argMax(value, updated_at) FROM pyloggle_settings WHERE key = ?", key).Scan(&v)
	if err != nil {
		return "", err
	}
	return v, nil
}

// SetSetting writes a value (newest row wins).
func (c *Client) SetSetting(ctx context.Context, key, value string) error {
	return c.conn.Exec(ctx, "INSERT INTO pyloggle_settings (key, value, updated_at) VALUES (?, ?, ?)", key, value, time.Now().UTC())
}
