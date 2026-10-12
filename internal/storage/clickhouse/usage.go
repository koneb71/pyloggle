package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
)

// Storage accounting. system.parts cannot attribute bytes to projects (parts mix projects), so
// every event table feeds project_usage_daily through a materialized view with an uncompressed
// estimate of the row (fixed overhead plus the string and map lengths). The real per-table
// compression ratio from system.parts turns that into an on-disk estimate.

// usageSource describes one event table's contribution.
type usageSource struct {
	Source string
	Table  string
	TSCol  string
	Bytes  string // SQL expression estimating the uncompressed row size
}

var usageSources = []usageSource{
	{"logs", "logs_raw", "ts", "96 + length(message) + length(logger) + length(http_url) + length(trace_id) + length(request_id) + length(exception_type) + length(toString(attrs)) + length(toString(tags))"},
	{"errors", "error_occurrences", "ts", "256 + length(message) + length(title) + length(frames_json) + length(chain_json) + length(breadcrumbs_json) + length(http_body) + length(extra_json) + length(toString(attrs)) + length(toString(tags)) + length(toString(http_headers))"},
	{"spans", "spans", "start_ts", "128 + length(name) + length(status_message) + length(events_json) + length(toString(attrs))"},
	{"metrics", "metrics_points", "ts", "64 + length(toString(attrs)) + 8 * length(bounds) + 8 * length(bucket_counts)"},
}

func (s usageSource) selectSQL(where string) string {
	return fmt.Sprintf("SELECT project_id, toDate(%s) AS day, '%s' AS source, count() AS rows, sum(%s) AS bytes_est FROM %s%s GROUP BY project_id, day",
		s.TSCol, s.Source, s.Bytes, s.Table, where)
}

// createUsage is the 014_usage migration step: the summing table, a guarded backfill per
// source and the views that keep it current.
func createUsage(ctx context.Context, c *Client, _ MigrateOptions) error {
	if err := c.Exec(ctx, `CREATE TABLE IF NOT EXISTS project_usage_daily (
    project_id String,
    day Date,
    source LowCardinality(String),
    rows UInt64,
    bytes_est UInt64
)
ENGINE = SummingMergeTree((rows, bytes_est))
ORDER BY (project_id, source, day)
TTL day + INTERVAL 400 DAY
SETTINGS index_granularity = 8192`); err != nil {
		return err
	}
	for _, s := range usageSources {
		exists, err := c.tableExists(ctx, s.Table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		guard := fmt.Sprintf(" WHERE (SELECT count() FROM project_usage_daily WHERE source = '%s') = 0", s.Source)
		if err := c.Exec(ctx, "INSERT INTO project_usage_daily "+s.selectSQL(guard)); err != nil {
			return fmt.Errorf("usage backfill %s: %w", s.Source, err)
		}
		if err := c.Exec(ctx, fmt.Sprintf("CREATE MATERIALIZED VIEW IF NOT EXISTS project_usage_%s_mv TO project_usage_daily AS %s", s.Source, s.selectSQL(""))); err != nil {
			return fmt.Errorf("usage view %s: %w", s.Source, err)
		}
	}
	if err := c.Exec(ctx, `ALTER TABLE projects ADD COLUMN IF NOT EXISTS retention_days UInt16 DEFAULT 0`); err != nil {
		return err
	}
	if err := c.Exec(ctx, `ALTER TABLE projects ADD COLUMN IF NOT EXISTS is_deleted UInt8 DEFAULT 0`); err != nil {
		return err
	}
	return c.Exec(ctx, `CREATE TABLE IF NOT EXISTS admin_audit (
    ts DateTime64(3, 'UTC'),
    actor String,
    action LowCardinality(String),
    project_id String,
    target String,
    details String
)
ENGINE = MergeTree()
ORDER BY ts
TTL toDate(ts) + INTERVAL 730 DAY
SETTINGS index_granularity = 8192`)
}

// ProjectUsage returns the per-day, per-source rows of one project over the last days.
func (c *Client) ProjectUsage(ctx context.Context, projectID string, days int) ([]api.UsageDay, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("missing project_id")
	}
	if days <= 0 {
		days = 30
	}
	rows, err := c.conn.Query(ctx, `SELECT day, source, sum(rows), sum(bytes_est) FROM project_usage_daily
		WHERE project_id = ? AND day >= today() - ? GROUP BY day, source ORDER BY day, source`, projectID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.UsageDay{}
	for rows.Next() {
		var d api.UsageDay
		if err := rows.Scan(&d.Day, &d.Source, &d.Rows, &d.BytesEst); err != nil {
			return nil, err
		}
		d.Day = d.Day.UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// TableCompression returns compressed/uncompressed per table from the active parts.
func (c *Client) TableCompression(ctx context.Context) (map[string]float64, error) {
	rows, err := c.conn.Query(ctx, `SELECT table, sum(data_compressed_bytes), sum(data_uncompressed_bytes)
		FROM system.parts WHERE database = currentDatabase() AND active GROUP BY table`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var table string
		var comp, uncomp uint64
		if err := rows.Scan(&table, &comp, &uncomp); err != nil {
			return nil, err
		}
		if uncomp > 0 {
			out[table] = float64(comp) / float64(uncomp)
		}
	}
	return out, rows.Err()
}

// usageRatio is the compression ratio for a source's table (a conservative default when the
// table has no parts yet).
func UsageRatio(comp map[string]float64, source string) float64 {
	for _, s := range usageSources {
		if s.Source == source {
			if r, ok := comp[s.Table]; ok && r > 0 {
				return r
			}
		}
	}
	return 0.25
}

// StoredBytesByProject estimates the on-disk bytes per project over the window (the ingest
// quota guard reads it every minute).
func (c *Client) StoredBytesByProject(ctx context.Context, window time.Duration) (map[string]uint64, error) {
	days := int(window / (24 * time.Hour))
	if days <= 0 {
		days = 30
	}
	comp, err := c.TableCompression(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.conn.Query(ctx, `SELECT project_id, source, sum(bytes_est) FROM project_usage_daily WHERE day >= today() - ? GROUP BY project_id, source`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]uint64{}
	for rows.Next() {
		var project, source string
		var bytes uint64
		if err := rows.Scan(&project, &source, &bytes); err != nil {
			return nil, err
		}
		out[project] += uint64(float64(bytes) * UsageRatio(comp, source))
	}
	return out, rows.Err()
}

// RecomputeUsage rebuilds a project's usage rows from the event tables (after a purge).
func (c *Client) RecomputeUsage(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("missing project_id")
	}
	if err := c.conn.Exec(ctx, "DELETE FROM project_usage_daily WHERE project_id = ?", projectID); err != nil {
		return err
	}
	for _, s := range usageSources {
		exists, err := c.tableExists(ctx, s.Table)
		if err != nil || !exists {
			continue
		}
		if err := c.conn.Exec(ctx, "INSERT INTO project_usage_daily "+s.selectSQL(" WHERE project_id = ?"), projectID); err != nil {
			return fmt.Errorf("recompute %s: %w", s.Source, err)
		}
	}
	return nil
}

// AddAudit records an administrative action.
func (c *Client) AddAudit(ctx context.Context, a api.AuditEntry) error {
	if a.TS.IsZero() {
		a.TS = time.Now().UTC()
	}
	return c.conn.Exec(ctx, "INSERT INTO admin_audit (ts, actor, action, project_id, target, details) VALUES ("+ts3Expr+", ?, ?, ?, ?, ?)",
		ts3(a.TS), a.Actor, a.Action, a.ProjectID, a.Target, a.Details)
}

// ListAudit returns the newest audit rows, optionally for one project.
func (c *Client) ListAudit(ctx context.Context, projectID string, limit int) ([]api.AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := "SELECT ts, actor, action, project_id, target, details FROM admin_audit"
	args := []any{}
	if projectID != "" {
		q += " WHERE project_id = ?"
		args = append(args, projectID)
	}
	q += " ORDER BY ts DESC LIMIT ?"
	args = append(args, limit)
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.AuditEntry{}
	for rows.Next() {
		var a api.AuditEntry
		if err := rows.Scan(&a.TS, &a.Actor, &a.Action, &a.ProjectID, &a.Target, &a.Details); err != nil {
			return nil, err
		}
		a.TS = a.TS.UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}

// PendingMutations counts unfinished mutations whose command mentions needle (a project id).
func (c *Client) PendingMutations(ctx context.Context, needle string) (int, error) {
	var n uint64
	q := "SELECT count() FROM system.mutations WHERE database = currentDatabase() AND is_done = 0"
	args := []any{}
	if needle != "" {
		q += " AND position(command, ?) > 0"
		args = append(args, needle)
	}
	if err := c.conn.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return int(n), nil
}
