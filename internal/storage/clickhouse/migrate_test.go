package clickhouse

import (
	"strings"
	"testing"
)

func TestEveryEmbeddedFileIsListed(t *testing.T) {
	listed := map[string]bool{}
	for _, m := range migrations {
		if m.SQL != "" {
			listed[m.SQL] = true
		}
		if m.SQL == "" && m.Fn == nil {
			t.Fatalf("%s has neither SQL nor Fn", m.Name)
		}
	}
	for _, f := range embeddedMigrationFiles() {
		if !listed[f] {
			t.Fatalf("embedded migration %s is not in the ordered list", f)
		}
	}
	for f := range listed {
		if _, err := migrationsFS.ReadFile("migrations/" + f); err != nil {
			t.Fatalf("listed migration %s is not embedded: %v", f, err)
		}
	}
	// Names must be unique and ordered.
	seen := map[string]bool{}
	prev := ""
	for _, m := range migrations {
		if seen[m.Name] || m.Name <= prev {
			t.Fatalf("migration names must be unique and ascending: %s after %s", m.Name, prev)
		}
		seen[m.Name] = true
		prev = m.Name
	}
}

func TestSplitStatements(t *testing.T) {
	raw, err := migrationsFS.ReadFile("migrations/001_initial_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmts := splitStatements(string(raw))
	if len(stmts) < 8 {
		t.Fatalf("expected the initial schema to split into several statements, got %d", len(stmts))
	}
	for _, s := range stmts {
		up := strings.ToUpper(s)
		if strings.HasPrefix(up, "USE ") {
			t.Fatalf("USE must be skipped: %q", s)
		}
		if strings.Contains(s, "--") {
			t.Fatalf("comments must be stripped: %q", firstLine(s))
		}
		if strings.HasSuffix(s, ";") {
			t.Fatalf("statements must not end with ';': %q", firstLine(s))
		}
	}
	// A materialized view with a GROUP BY survives as one statement.
	var mv int
	for _, s := range stmts {
		if strings.HasPrefix(strings.ToUpper(s), "CREATE MATERIALIZED VIEW") && strings.Contains(s, "GROUP BY") {
			mv++
		}
	}
	if mv != 1 {
		t.Fatalf("expected one MV statement, got %d", mv)
	}
}

func TestExpandPlaceholders(t *testing.T) {
	raw, _ := migrationsFS.ReadFile("migrations/007_issues_agg.sql")
	out := expandPlaceholders(string(raw), MigrateOptions{}.withDefaults())
	if strings.Contains(out, "${") {
		t.Fatalf("unexpanded placeholder in 007")
	}
	if !strings.Contains(out, "INTERVAL 365 DAY") {
		t.Fatalf("stats retention default not applied")
	}
	for _, stmt := range splitStatements(out) {
		up := strings.ToUpper(stmt)
		if strings.HasPrefix(up, "INSERT INTO") && !strings.Contains(stmt, "(SELECT count() FROM") {
			t.Fatalf("backfill must be guarded: %s", firstLine(stmt))
		}
	}
}

func TestSchemaV2DDL(t *testing.T) {
	opts := MigrateOptions{RetentionLogsDays: 14, RetentionErrorsDays: 45}.withDefaults()
	for _, spec := range v2Specs {
		sql := spec.createSQL(spec.Name+"_v2", spec.Retention(opts))
		for _, want := range []string{"PARTITION BY toDate(ts)", "ttl_only_drop_parts = 1", "ORDER BY (project_id,", "CODEC(ZSTD(3))"} {
			if !strings.Contains(sql, want) {
				t.Fatalf("%s DDL missing %q:\n%s", spec.Name, want, sql)
			}
		}
		if strings.Count(sql, "\n    ")-len(spec.Indexes) != len(spec.Columns) {
			t.Fatalf("%s: column count mismatch", spec.Name)
		}
		// Every column name is unique.
		seen := map[string]bool{}
		for _, c := range spec.Columns {
			if seen[c.Name] {
				t.Fatalf("%s: duplicate column %s", spec.Name, c.Name)
			}
			seen[c.Name] = true
		}
	}
	spans := spansSpec.createSQL("spans", spansSpec.Retention(MigrateOptions{}.withDefaults()))
	for _, want := range []string{"PARTITION BY toDate(start_ts)", "INTERVAL 7 DAY", "Enum8('unset' = 0, 'ok' = 1, 'error' = 2)", "idx_trace", "ORDER BY (project_id, service, env, start_ts)"} {
		if !strings.Contains(spans, want) {
			t.Fatalf("spans DDL missing %q:\n%s", want, spans)
		}
	}
	logs := logsRawSpec.createSQL("logs_raw_v2", opts.RetentionLogsDays)
	if !strings.Contains(logs, "INTERVAL 14 DAY") {
		t.Fatalf("logs TTL not applied: %s", logs)
	}
	errs := errorOccurrencesSpec.createSQL("x", opts.RetentionErrorsDays)
	if !strings.Contains(errs, "INTERVAL 45 DAY") {
		t.Fatalf("errors TTL not applied")
	}

	// In-place path only adds what is missing, and copies only shared columns with level normalized.
	existing := map[string]bool{"ts": true, "service": true, "env": true, "level": true, "logger": true, "message": true, "project_id": true, "attrs": true}
	adds := logsRawSpec.addMissingColumnsSQL(existing)
	if len(adds) != len(logsRawSpec.Columns)-len(existing) {
		t.Fatalf("expected %d ADD COLUMN statements, got %d", len(logsRawSpec.Columns)-len(existing), len(adds))
	}
	for _, a := range adds {
		if strings.Contains(a, " ts ") || strings.Contains(a, " message ") {
			t.Fatalf("must not re-add existing columns: %s", a)
		}
	}
	cp, ok := logsRawSpec.copySelect("logs_raw", "logs_raw_v2", existing)
	if !ok || !strings.Contains(cp, "multiIf(level = 'warning', 'warn'") || strings.Contains(cp, "event_id") {
		t.Fatalf("copy select wrong: %s", cp)
	}
	if strings.Count(cp, "?") != 2 {
		t.Fatalf("copy select must bind the day window: %s", cp)
	}
}
