package clickhouse

import (
	"strings"
	"testing"
	"time"

	"pyloggle/internal/api"
)

func TestRuleFilterQueriesAreWellFormed(t *testing.T) {
	rule := api.AlertRule{ProjectID: "p", Service: "api", Env: "prod", HTTPRoute: "/x", HTTPMethod: "GET"}
	now := time.Now()

	q, args := countErrorOccurrencesQuery(rule, now.Add(-time.Minute), now)
	if strings.Contains(q, "{") {
		t.Fatalf("named params leaked into query: %s", q)
	}
	if got, want := strings.Count(q, "?"), len(args); got != want {
		t.Fatalf("placeholders %d != args %d: %s", got, want, q)
	}
	for _, col := range []string{"service", "env", "http_route", "http_method"} {
		if !strings.Contains(q, " AND "+col+" = ?") {
			t.Fatalf("missing filter for %s: %s", col, q)
		}
	}

	q, args = issueSpikeCountsQuery(rule, now.Add(-2*time.Minute), now.Add(-time.Minute), now.Add(-time.Minute), now, 5)
	if got, want := strings.Count(q, "?"), len(args); got != want {
		t.Fatalf("spike placeholders %d != args %d: %s", got, want, q)
	}
	whereIdx := strings.Index(q, "WHERE")
	filterIdx := strings.Index(q, "AND service = ?")
	groupIdx := strings.Index(q, "GROUP BY")
	limitIdx := strings.Index(q, "LIMIT")
	if !(whereIdx < filterIdx && filterIdx < groupIdx && groupIdx < limitIdx) {
		t.Fatalf("filters must sit between WHERE and GROUP BY: %s", q)
	}
	if args[len(args)-1] != 5 {
		t.Fatalf("limit must be the last arg, got %v", args[len(args)-1])
	}
	if !strings.Contains(q, "uniqIf(user_id_hash, ts >= ? AND ts < ? AND user_id_hash != '') as cur_users") {
		t.Fatalf("spike query must count people in the current window: %s", q)
	}

	q, args = tracesWithSpansQuery("p", []string{"a", "b"}, now.Add(-time.Hour), now)
	if !strings.Contains(q, "trace_id IN (?)") || strings.Contains(q, "has(") || strings.Count(q, "?") != len(args) {
		t.Fatalf("traces-with-spans query: %s %v", q, args)
	}

	// No filters -> no dangling AND.
	q, args = countErrorOccurrencesQuery(api.AlertRule{ProjectID: "p"}, now.Add(-time.Minute), now)
	if strings.Contains(q, "AND service") || len(args) != 3 {
		t.Fatalf("unexpected filters: %s %v", q, args)
	}
}
