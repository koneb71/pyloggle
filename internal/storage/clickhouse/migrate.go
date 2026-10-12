package clickhouse

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrateOptions carries the knobs migrations and retention need.
type MigrateOptions struct {
	Rebuild              bool // rebuild event tables with daily partitions (default true)
	RetentionLogsDays    int
	RetentionErrorsDays  int
	RetentionStatsDays   int
	RetentionSpansDays   int // spans (OTLP traces), default 7
	RetentionMetricsDays int // metrics_points (OTLP metrics), default 30
	Logf                 func(format string, args ...any)
}

func (o MigrateOptions) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (o MigrateOptions) withDefaults() MigrateOptions {
	if o.RetentionLogsDays <= 0 {
		o.RetentionLogsDays = 30
	}
	if o.RetentionErrorsDays <= 0 {
		o.RetentionErrorsDays = 30
	}
	if o.RetentionStatsDays <= 0 {
		o.RetentionStatsDays = 365
	}
	if o.RetentionSpansDays <= 0 {
		o.RetentionSpansDays = 7
	}
	if o.RetentionMetricsDays <= 0 {
		o.RetentionMetricsDays = 30
	}
	return o
}

type migration struct {
	Name string
	SQL  string // embedded file name; empty for Go steps
	Fn   func(ctx context.Context, c *Client, opts MigrateOptions) error
}

// migrations is the explicit, ordered list. Add new steps at the end; never reorder.
var migrations = []migration{
	{Name: "001_initial_schema", SQL: "001_initial_schema.sql"},
	{Name: "002_alerting", SQL: "002_alerting.sql"},
	{Name: "003_alerting_telegram", SQL: "003_alerting_telegram.sql"},
	{Name: "004_saved_searches_pins", SQL: "004_saved_searches_pins.sql"},
	{Name: "005_ips", SQL: "005_ips.sql"},
	{Name: "006_events_v2", Fn: rebuildV2},
	{Name: "007_issues_agg", SQL: "007_issues_agg.sql"},
	{Name: "008_drop_v1_stats", SQL: "008_drop_v1_stats.sql"},
	{Name: "009_alerting_v2", SQL: "009_alerting_v2.sql"},
	{Name: "010_issues_workflow", SQL: "010_issues_workflow.sql"},
	{Name: "011_request_stats", SQL: "011_request_stats.sql"},
	{Name: "012_spans", Fn: createSpans},
	{Name: "013_metrics", Fn: createMetrics},
	{Name: "014_usage", Fn: createUsage},
	{Name: "015_api_keys", Fn: createAPIKeys},
	{Name: "016_alert_once_and_recap", SQL: "016_alert_once_and_recap.sql"},
}

// Migrate applies every migration not yet recorded in schema_migrations, in order.
// It is safe to run at every startup. Deployments created with the old initdb DDL simply get
// 001-005 re-applied (all IF NOT EXISTS) before the v2 steps run.
func (c *Client) Migrate(ctx context.Context, opts MigrateOptions) ([]string, error) {
	opts = opts.withDefaults()
	if err := c.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name String, checksum String, applied_at DateTime64(3, 'UTC')
	) ENGINE = MergeTree ORDER BY name`); err != nil {
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}
	if err := c.Exec(ctx, `CREATE TABLE IF NOT EXISTS pyloggle_settings (
		key String, value String, updated_at DateTime64(3, 'UTC')
	) ENGINE = ReplacingMergeTree(updated_at) ORDER BY key`); err != nil {
		return nil, fmt.Errorf("pyloggle_settings: %w", err)
	}

	done := map[string]bool{}
	rows, err := c.conn.Query(ctx, "SELECT name FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		done[name] = true
	}
	rows.Close()

	var applied []string
	for _, m := range migrations {
		if done[m.Name] {
			continue
		}
		start := time.Now()
		checksum := ""
		if m.SQL != "" {
			raw, err := migrationsFS.ReadFile("migrations/" + m.SQL)
			if err != nil {
				return applied, fmt.Errorf("%s: %w", m.Name, err)
			}
			sum := sha256.Sum256(raw)
			checksum = hex.EncodeToString(sum[:8])
			for _, stmt := range splitStatements(expandPlaceholders(string(raw), opts)) {
				if err := c.Exec(ctx, stmt); err != nil {
					return applied, fmt.Errorf("%s: %w\nstatement: %s", m.Name, err, firstLine(stmt))
				}
			}
		}
		if m.Fn != nil {
			if err := m.Fn(ctx, c, opts); err != nil {
				return applied, fmt.Errorf("%s: %w", m.Name, err)
			}
		}
		if err := c.conn.Exec(ctx, "INSERT INTO schema_migrations (name, checksum, applied_at) VALUES (?, ?, ?)",
			m.Name, checksum, time.Now().UTC()); err != nil {
			return applied, err
		}
		applied = append(applied, m.Name)
		opts.logf("migrate: applied %s in %s", m.Name, time.Since(start).Round(time.Millisecond))
	}
	return applied, nil
}

// Exec runs one DDL/DML statement.
// QueryRow runs a single-row query (tests, admin tooling).
func (c *Client) QueryRow(ctx context.Context, query string, args ...any) interface{ Scan(dest ...any) error } {
	return c.conn.QueryRow(ctx, query, args...)
}

func (c *Client) Exec(ctx context.Context, stmt string) error {
	return c.conn.Exec(ctx, stmt)
}

// tableColumns returns the live column set of a table in the connected database, or an empty map
// if the table does not exist.
func (c *Client) tableColumns(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := c.conn.Query(ctx, "SELECT name FROM system.columns WHERE database = currentDatabase() AND table = ?", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func (c *Client) tableExists(ctx context.Context, table string) (bool, error) {
	cols, err := c.tableColumns(ctx, table)
	if err != nil {
		return false, err
	}
	return len(cols) > 0, nil
}

// expandPlaceholders substitutes ${RETENTION_*_DAYS} in SQL files.
func expandPlaceholders(sql string, opts MigrateOptions) string {
	r := strings.NewReplacer(
		"${RETENTION_LOGS_DAYS}", strconv.Itoa(opts.RetentionLogsDays),
		"${RETENTION_ERRORS_DAYS}", strconv.Itoa(opts.RetentionErrorsDays),
		"${RETENTION_STATS_DAYS}", strconv.Itoa(opts.RetentionStatsDays),
	)
	return r.Replace(sql)
}

// splitStatements strips "--" comments, splits on ";" and drops USE / empty statements.
// Our DDL never carries ";" or "--" inside string literals, which keeps this simple.
func splitStatements(sql string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	var out []string
	for _, stmt := range strings.Split(cleaned.String(), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		upper := strings.ToUpper(stmt)
		if strings.HasPrefix(upper, "USE ") {
			continue
		}
		out = append(out, stmt)
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

// embeddedMigrationFiles lists the embedded SQL files (used by tests to catch unlisted files).
func embeddedMigrationFiles() []string {
	entries, _ := migrationsFS.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}
