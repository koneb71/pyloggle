package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Schema v2 for the two event tables, generated from one column spec so the fresh CREATE, the
// in-place ADD COLUMN fallback and the rebuild copy never disagree.

type column struct {
	Name string
	Type string // full type incl. DEFAULT/CODEC
}

type tableSpec struct {
	Name       string
	Columns    []column
	Indexes    []string
	OrderBy    string
	TTLColumn  string // expression base for TTL: toDate(<col>)
	Retention  func(opts MigrateOptions) int
	Normalized map[string]string // column -> SELECT expression used when copying from v1
}

var logsRawSpec = tableSpec{
	Name: "logs_raw",
	Columns: []column{
		{"ts", "DateTime64(3, 'UTC') CODEC(DoubleDelta, ZSTD(1))"},
		{"event_id", "String"},
		{"project_id", "String"},
		{"service", "LowCardinality(String)"},
		{"env", "LowCardinality(String)"},
		{"level", "LowCardinality(String)"},
		{"logger", "LowCardinality(String)"},
		{"message", "String CODEC(ZSTD(3))"},
		{"trace_id", "String"},
		{"span_id", "String"},
		{"request_id", "String"},
		{"issue_id", "String"},
		{"release", "LowCardinality(String)"},
		{"hostname", "LowCardinality(String)"},
		{"source", "LowCardinality(String)"},
		{"http_method", "LowCardinality(String)"},
		{"http_route", "String"},
		{"http_status", "UInt16"},
		{"http_url", "String CODEC(ZSTD(3))"},
		{"http_remote_addr", "String"},
		{"http_server_addr", "String"},
		{"duration_ms", "Float32"},
		{"user_id_hash", "String"},
		{"exception_type", "String"},
		{"attrs", "Map(String, String) CODEC(ZSTD(3))"},
		{"tags", "Map(String, String)"},
	},
	Indexes: []string{
		"INDEX idx_event event_id TYPE bloom_filter(0.01) GRANULARITY 1",
		"INDEX idx_trace trace_id TYPE bloom_filter(0.01) GRANULARITY 1",
		"INDEX idx_request request_id TYPE bloom_filter(0.01) GRANULARITY 1",
		"INDEX idx_issue issue_id TYPE bloom_filter(0.01) GRANULARITY 1",
		"INDEX idx_msg message TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 2",
		"INDEX idx_attr_keys mapKeys(attrs) TYPE bloom_filter(0.01) GRANULARITY 1",
	},
	OrderBy:   "(project_id, service, env, ts)",
	TTLColumn: "ts",
	Retention: func(o MigrateOptions) int { return o.RetentionLogsDays },
	Normalized: map[string]string{
		"level": "multiIf(level = 'warning', 'warn', level = 'critical', 'fatal', level)",
	},
}

var errorOccurrencesSpec = tableSpec{
	Name: "error_occurrences",
	Columns: []column{
		{"ts", "DateTime64(3, 'UTC') CODEC(DoubleDelta, ZSTD(1))"},
		{"event_id", "String"},
		{"project_id", "String"},
		{"service", "LowCardinality(String)"},
		{"env", "LowCardinality(String)"},
		{"issue_id", "String"},
		{"level", "LowCardinality(String)"},
		{"logger", "LowCardinality(String)"},
		{"message", "String CODEC(ZSTD(3))"},
		{"exception_type", "String"},
		{"exception_module", "String"},
		{"exception_message", "String CODEC(ZSTD(3))"},
		{"stack_fingerprint", "String"},
		{"fingerprint_version", "UInt8 DEFAULT 1"},
		{"title", "String"},
		{"culprit", "String"},
		{"stack_frames", "Array(Tuple(filename String, lineno UInt32, function String, context String, is_in_app UInt8))"},
		{"frames_json", "String CODEC(ZSTD(3))"},
		{"chain_json", "String CODEC(ZSTD(3))"},
		{"notes", "Array(String)"},
		{"breadcrumbs_json", "String CODEC(ZSTD(3))"},
		{"trace_id", "String"},
		{"span_id", "String"},
		{"request_id", "String"},
		{"http_method", "LowCardinality(String)"},
		{"http_route", "String"},
		{"http_status", "UInt16"},
		{"http_url", "String CODEC(ZSTD(3))"},
		{"http_query", "String"},
		{"http_headers", "Map(String, String) CODEC(ZSTD(3))"},
		{"http_body", "String CODEC(ZSTD(3))"},
		{"http_user_agent", "String"},
		{"http_remote_addr", "String"},
		{"http_server_addr", "String"},
		{"http_client_ip", "String"},
		{"user_id_hash", "String"},
		{"user_id", "String"},
		{"user_email", "String"},
		{"user_username", "String"},
		{"user_ip", "String"},
		{"release", "LowCardinality(String)"},
		{"hostname", "LowCardinality(String)"},
		{"runtime_name", "LowCardinality(String)"},
		{"runtime_version", "LowCardinality(String)"},
		{"runtime_platform", "LowCardinality(String)"},
		{"runtime_pid", "UInt32"},
		{"runtime_thread", "String"},
		{"sdk_name", "LowCardinality(String)"},
		{"sdk_version", "LowCardinality(String)"},
		{"source", "LowCardinality(String)"},
		{"mechanism_type", "LowCardinality(String)"},
		{"mechanism_handled", "UInt8 DEFAULT 1"},
		{"tags", "Map(String, String)"},
		{"attrs", "Map(String, String) CODEC(ZSTD(3))"},
		{"extra_json", "String CODEC(ZSTD(3))"},
	},
	Indexes: []string{
		"INDEX idx_issue issue_id TYPE bloom_filter(0.01) GRANULARITY 1",
		"INDEX idx_event event_id TYPE bloom_filter(0.01) GRANULARITY 1",
		"INDEX idx_fingerprint stack_fingerprint TYPE bloom_filter(0.01) GRANULARITY 1",
		"INDEX idx_request request_id TYPE bloom_filter(0.01) GRANULARITY 1",
		"INDEX idx_trace trace_id TYPE bloom_filter(0.01) GRANULARITY 1",
	},
	OrderBy:   "(project_id, service, env, issue_id, ts)",
	TTLColumn: "ts",
	Retention: func(o MigrateOptions) int { return o.RetentionErrorsDays },
	Normalized: map[string]string{
		"level": "multiIf(level = '', 'error', level = 'warning', 'warn', level = 'critical', 'fatal', level)",
	},
}

var v2Specs = []tableSpec{logsRawSpec, errorOccurrencesSpec}

func (s tableSpec) ttlExpr(days int) string {
	if days <= 0 {
		days = 30
	}
	return fmt.Sprintf("toDate(%s) + INTERVAL %d DAY", s.TTLColumn, days)
}

// createSQL renders CREATE TABLE for the spec under the given table name.
func (s tableSpec) createSQL(name string, days int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %s (\n", name)
	for _, c := range s.Columns {
		fmt.Fprintf(&b, "    %s %s,\n", c.Name, c.Type)
	}
	for i, idx := range s.Indexes {
		sep := ","
		if i == len(s.Indexes)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "    %s%s\n", idx, sep)
	}
	fmt.Fprintf(&b, ")\nENGINE = MergeTree()\nPARTITION BY toDate(%s)\nORDER BY %s\nTTL %s\nSETTINGS index_granularity = 8192, ttl_only_drop_parts = 1",
		s.TTLColumn, s.OrderBy, s.ttlExpr(days))
	return b.String()
}

// addMissingColumnsSQL renders one ALTER per column the live table lacks (in-place upgrade path).
func (s tableSpec) addMissingColumnsSQL(existing map[string]bool) []string {
	var out []string
	prev := ""
	for _, c := range s.Columns {
		if !existing[c.Name] {
			stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", s.Name, c.Name, c.Type)
			if prev != "" && existing[prev] {
				stmt += " AFTER " + prev
			}
			out = append(out, stmt)
		}
		prev = c.Name
	}
	return out
}

// copySelect renders the INSERT ... SELECT used to move the retention window from the v1 table
// into the rebuilt one, over the columns both tables share (missing v2 columns take defaults).
func (s tableSpec) copySelect(from, to string, existing map[string]bool) (string, bool) {
	var cols, exprs []string
	for _, c := range s.Columns {
		if !existing[c.Name] {
			continue
		}
		cols = append(cols, c.Name)
		if e, ok := s.Normalized[c.Name]; ok {
			exprs = append(exprs, e)
		} else {
			exprs = append(exprs, c.Name)
		}
	}
	if len(cols) == 0 {
		return "", false
	}
	return fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s WHERE %s >= ? AND %s < ?",
		to, strings.Join(cols, ", "), strings.Join(exprs, ", "), from, s.TTLColumn, s.TTLColumn), true
}

// rebuildV2 is the Go migration step for the two event tables.
//
// Rebuild path (default): create <table>_v2 with daily partitions, the project-first sort key,
// codecs and the configured TTL; copy only the retention window from the old table day by day;
// swap the tables; drop the old one (everything older than the retention window goes with it).
//
// In-place path (PYLOGGLE_MIGRATE_REBUILD=false): add the missing columns and set the TTL on the
// existing tables; partitions stay monthly.
func rebuildV2(ctx context.Context, c *Client, opts MigrateOptions) error {
	// Views that select from error_occurrences must not survive the swap.
	for _, stmt := range []string{"DROP VIEW IF EXISTS issues_stats_mv", "DROP VIEW IF EXISTS issues"} {
		if err := c.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	for _, spec := range v2Specs {
		days := spec.Retention(opts)
		existing, err := c.tableColumns(ctx, spec.Name)
		if err != nil {
			return err
		}
		if !opts.Rebuild && len(existing) > 0 {
			for _, stmt := range spec.addMissingColumnsSQL(existing) {
				if err := c.Exec(ctx, stmt); err != nil {
					return fmt.Errorf("%s: %w", stmt, err)
				}
			}
			if err := c.Exec(ctx, fmt.Sprintf("ALTER TABLE %s MODIFY TTL %s", spec.Name, spec.ttlExpr(days))); err != nil {
				return err
			}
			opts.logf("migrate: %s upgraded in place (%d columns added)", spec.Name, len(spec.addMissingColumnsSQL(existing)))
			continue
		}

		tmp := spec.Name + "_v2"
		if err := c.Exec(ctx, "DROP TABLE IF EXISTS "+tmp); err != nil {
			return err
		}
		if err := c.Exec(ctx, spec.createSQL(tmp, days)); err != nil {
			return fmt.Errorf("create %s: %w", tmp, err)
		}
		if len(existing) == 0 {
			if err := c.Exec(ctx, fmt.Sprintf("RENAME TABLE %s TO %s", tmp, spec.Name)); err != nil {
				return err
			}
			opts.logf("migrate: created %s (v2)", spec.Name)
			continue
		}
		if err := c.copyRetentionWindow(ctx, spec, tmp, existing, days, opts); err != nil {
			return err
		}
		if err := c.swapTables(ctx, spec.Name, tmp); err != nil {
			return err
		}
		opts.logf("migrate: %s rebuilt (daily partitions, %d-day TTL)", spec.Name, days)
	}
	return nil
}

func (c *Client) copyRetentionWindow(ctx context.Context, spec tableSpec, to string, existing map[string]bool, days int, opts MigrateOptions) error {
	stmt, ok := spec.copySelect(spec.Name, to, existing)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	start := now.Add(-time.Duration(days) * 24 * time.Hour).Truncate(24 * time.Hour)
	end := now.Add(48 * time.Hour) // tolerate client clocks slightly ahead
	for day := start; day.Before(end); day = day.Add(24 * time.Hour) {
		if err := c.conn.Exec(ctx, stmt, day, day.Add(24*time.Hour)); err != nil {
			return fmt.Errorf("copy %s day %s: %w", spec.Name, day.Format("2006-01-02"), err)
		}
	}
	opts.logf("migrate: copied the last %d days of %s", days, spec.Name)
	return nil
}

// swapTables prefers the atomic EXCHANGE and falls back to RENAME for Ordinary databases.
func (c *Client) swapTables(ctx context.Context, live, fresh string) error {
	if err := c.Exec(ctx, fmt.Sprintf("EXCHANGE TABLES %s AND %s", live, fresh)); err == nil {
		return c.Exec(ctx, "DROP TABLE IF EXISTS "+fresh)
	}
	old := live + "_v1_old"
	if err := c.Exec(ctx, fmt.Sprintf("RENAME TABLE %s TO %s, %s TO %s", live, old, fresh, live)); err != nil {
		return err
	}
	return c.Exec(ctx, "DROP TABLE IF EXISTS "+old)
}
