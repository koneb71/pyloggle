package webui

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func TestOverviewComparesWithThePreviousRange(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/metrics/overview?from=2026-10-11T00:00:00Z&to=2026-10-12T00:00:00Z&compare=previous", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %v", rec.Code, out)
	}
	if len(store.overviewRanges) != 2 {
		t.Fatalf("want current + previous queries, got %v", store.overviewRanges)
	}
	prev := store.overviewRanges[1]
	if !prev[0].Equal(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)) || !prev[1].Equal(time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("previous range: %v", prev)
	}
	if out["previous"].(map[string]any)["events"].(float64) != 20 {
		t.Fatalf("previous overview missing: %v", out)
	}
	// Without compare there is no second query and no "previous" key.
	store.overviewRanges = nil
	_, out = do(t, u, "GET", "/ui/api/metrics/overview?from=-1h", nil, nil)
	if _, ok := out["previous"]; ok || len(store.overviewRanges) != 1 {
		t.Fatalf("compare must be opt-in: %v", out)
	}
}

func TestRequestStatsPreviousTotals(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/metrics/requests?from=2026-10-11T12:00:00Z&to=2026-10-11T13:00:00Z&compare=previous", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %v", rec.Code, out)
	}
	pt, ok := out["previous_totals"].(map[string]any)
	if !ok || pt["requests"].(float64) != 10 {
		t.Fatalf("previous_totals: %v", out)
	}
	prev := store.requestRanges[len(store.requestRanges)-1]
	if !prev[0].Equal(time.Date(2026, 10, 11, 11, 0, 0, 0, time.UTC)) || !prev[1].Equal(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("previous range: %v", prev)
	}
}

func TestTopIssuesCarrySparklines(t *testing.T) {
	u, _, _ := newTestUI("disabled")
	u.store.(*fakeStore).issues = nil
	rec, out := do(t, u, "GET", "/ui/api/metrics/top-issues?from=-24h", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	_ = out // the fake TopIssues returns no rows; the sparkline join is covered below
	items := []api.IssueSummary{{IssueID: "a"}, {IssueID: "b"}}
	u.attachSparklines(httptest.NewRequest("GET", "/", nil), "default", items)
	if len(items[0].Sparkline) != 24 || items[1].Sparkline[23] != 7 {
		t.Fatalf("sparklines not attached: %+v", items)
	}
}

func TestAttentionRanksCameBackFirstThenByVolume(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	from := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	old := from.Add(-72 * time.Hour)
	store.issues = []api.IssueSummary{
		{IssueID: "steady", Status: "open", FirstSeen: old},
		{IssueID: "spiking", Status: "open", FirstSeen: old},
		{IssueID: "fresh", Status: "open", FirstSeen: from.Add(2 * time.Hour)},
		{IssueID: "back", Status: "open", IsRegressed: true, FirstSeen: old},
		{IssueID: "muted", Status: "ignored", FirstSeen: from.Add(time.Hour)},
		{IssueID: "small", Status: "open", FirstSeen: old},
	}
	store.spikes = []clickhouse.IssueSpikeRow{
		{IssueID: "steady", CurCount: 500, PrevCount: 480},
		{IssueID: "spiking", CurCount: 120, PrevCount: 30},
		{IssueID: "fresh", CurCount: 3},
		{IssueID: "back", CurCount: 2},
		{IssueID: "muted", CurCount: 900},
		{IssueID: "small", CurCount: 6, PrevCount: 0}, // below the minimum count: not a spike
	}
	rec, out := do(t, u, "GET", "/ui/api/metrics/attention?from=2026-10-11T00:00:00Z&to=2026-10-12T00:00:00Z", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %v", rec.Code, out)
	}
	items := out["items"].([]any)
	var order []string
	for _, it := range items {
		m := it.(map[string]any)
		order = append(order, m["issue"].(map[string]any)["issue_id"].(string)+":"+m["reasons"].([]any)[0].(string))
	}
	want := []string{"back:regressed", "spiking:spike", "fresh:new"}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
	counts := out["counts"].(map[string]any)
	if counts["regressed"].(float64) != 1 || counts["new"].(float64) != 1 || counts["spike"].(float64) != 1 {
		t.Fatalf("counts %v", counts)
	}
	first := items[0].(map[string]any)["issue"].(map[string]any)
	if len(first["sparkline"].([]any)) != 24 {
		t.Fatalf("attention items carry sparklines: %v", first)
	}
	// The comparison window is the same length, just before.
	w := store.spikeWindows
	if !w[0].Equal(from.Add(-24*time.Hour)) || !w[1].Equal(from) || !w[2].Equal(from) {
		t.Fatalf("windows %v", w)
	}
}

func TestServicesAndReleasesEndpoints(t *testing.T) {
	u, _, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/metrics/services?from=-24h", nil, nil)
	if rec.Code != 200 || out["items"].([]any)[0].(map[string]any)["status"] != "failing" {
		t.Fatalf("services: %d %v", rec.Code, out)
	}
	rec, out = do(t, u, "GET", "/ui/api/releases?limit=5", nil, nil)
	if rec.Code != 200 || out["items"].([]any)[0].(map[string]any)["release"] != "1.4.0" {
		t.Fatalf("releases: %d %v", rec.Code, out)
	}
}

func TestClassifyService(t *testing.T) {
	end := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	recent := end.Add(-time.Minute)
	for _, tc := range []struct {
		h    api.ServiceHealth
		want string
	}{
		{api.ServiceHealth{Requests: 100, FailedPct: 6, LastSeen: recent}, "failing"},
		{api.ServiceHealth{Requests: 10, FailedPct: 50, LastSeen: recent}, "ok"}, // too few requests to judge
		{api.ServiceHealth{Requests: 100, FailedPct: 1.5, LastSeen: recent}, "degraded"},
		{api.ServiceHealth{Logs: 5, Errors: 1, LastSeen: recent}, "degraded"},
		{api.ServiceHealth{Logs: 5, LastSeen: end.Add(-2 * time.Hour)}, "silent"},
		{api.ServiceHealth{Requests: 100, FailedPct: 50, Errors: 9, LastSeen: end.Add(-2 * time.Hour)}, "silent"}, // stopped sending: not known to be failing now
		{api.ServiceHealth{Logs: 5, LastSeen: recent}, "ok"},
	} {
		if got := clickhouse.ClassifyService(tc.h, end); got != tc.want {
			t.Fatalf("%+v: got %s want %s", tc.h, got, tc.want)
		}
	}
	list := []api.ServiceHealth{{Service: "b", Status: "ok", Logs: 9}, {Service: "a", Status: "failing"}, {Service: "c", Status: "ok", Logs: 99}}
	clickhouse.SortServiceHealth(list)
	if list[0].Service != "a" || list[1].Service != "c" || list[2].Service != "b" {
		t.Fatalf("sort: %+v", list)
	}
}
