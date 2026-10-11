package logquery

import (
	"strings"
	"testing"
)

func TestSpansSchema(t *testing.T) {
	q, err := Parse(`service:api duration:>250 status:error "GET /orders" db.system:postgres -kind:internal`)
	if err != nil {
		t.Fatal(err)
	}
	sql, args, err := q.SQLFor(Spans)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"service = ?", "(duration_us / 1000) > ?", "status = ?", "positionCaseInsensitiveUTF8(name, ?) > 0", "attrs[?] = ?", "NOT (kind = ?)"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("spans SQL missing %q: %s", want, sql)
		}
	}
	if strings.Contains(sql, "message") || len(args) != 7 || args[1] != 250.0 {
		t.Fatalf("spans SQL must not touch log columns: %s %v", sql, args)
	}
	// The same query against logs keeps the explorer semantics (status is a level alias there).
	lsql, _, err := q.SQL()
	if err != nil || !strings.Contains(lsql, "has(?, level)") || !strings.Contains(lsql, "duration_ms > ?") || !strings.Contains(lsql, "positionCaseInsensitiveUTF8(message, ?)") {
		t.Fatalf("logs SQL: %s %v", lsql, err)
	}
	fields := Spans.Fields()
	for _, want := range []string{"name", "kind", "duration_ms", "parent_span_id", "attrs.<key>"} {
		found := false
		for _, f := range fields {
			found = found || f == want
		}
		if !found {
			t.Fatalf("spans fields missing %s: %v", want, fields)
		}
	}
	for _, f := range fields {
		if f == "tags.<key>" || f == "level" {
			t.Fatalf("spans must not advertise %s", f)
		}
	}
	if e, a, k := Spans.Resolve("@db.statement"); e != "attrs[?]" || len(a) != 1 || a[0] != "db.statement" || k != "string" {
		t.Fatalf("resolve attr: %s %v %s", e, a, k)
	}
}
