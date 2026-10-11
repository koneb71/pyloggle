package clickhouse

import (
	"strings"
	"testing"
	"time"
)

func TestTraceListQueryShape(t *testing.T) {
	q := SpanQuery{ProjectID: "p", From: time.Unix(0, 0), To: time.Unix(3600, 0), Where: "status = ? AND attrs[?] = ?", Args: []any{"error", "db.system", "postgres"},
		BeforeTS: time.Unix(1800, 0), BeforeID: "abc", Limit: 20}
	sql, args := traceListQuery(q)
	if got, want := strings.Count(sql, "?"), len(args); got != want {
		t.Fatalf("placeholders %d != args %d: %s", got, want, sql)
	}
	if !strings.Contains(sql, "trace_id IN (SELECT trace_id FROM spans WHERE") || !strings.Contains(sql, entrySpan) || !strings.Contains(sql, "HAVING (ts, trace_id) < (toDateTime64(?, 6, 'UTC'), ?)") {
		t.Fatalf("shape: %s", sql)
	}
	if args[len(args)-1] != 20 || args[len(args)-2] != "abc" || args[len(args)-3] != "1970-01-01 00:30:00.000000" {
		t.Fatalf("cursor/limit args: %v", args)
	}
	sql, args = traceListQuery(SpanQuery{ProjectID: "p", From: time.Unix(0, 0), To: time.Unix(1, 0), Limit: 5})
	if strings.Contains(sql, "IN (SELECT") || strings.Contains(sql, "HAVING") || len(args) != 4 {
		t.Fatalf("plain list: %s %v", sql, args)
	}
}
