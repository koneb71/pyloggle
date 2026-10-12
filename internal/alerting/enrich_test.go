package alerting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/metrics"
	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func spikeRule() api.AlertRule {
	return api.AlertRule{ID: "r9", ProjectID: "p1", Name: "Checkout errors", Service: "api", Env: "prod", ConditionType: "issue_spike",
		Threshold: 5, TimeWindowSeconds: 300, Enabled: true, TelegramChatIDs: []string{"42"}}
}

func checkoutOccurrence(at time.Time) api.ErrorOccurrence {
	yes, no := true, false
	return api.ErrorOccurrence{
		TS: at, EventID: "e1", IssueID: "i1", Service: "api", Env: "prod", Release: "1.4.0", Title: "PaymentDeclined: card declined",
		Frames: []model.Frame{
			{Filename: "app/gateway.py", Function: "call", Lineno: 41, Context: "    raise PaymentDeclined(resp)", InApp: &yes},
			{Filename: "site-packages/httpx/_client.py", Function: "send", Lineno: 900, Context: "return resp", InApp: &no},
		},
		Breadcrumbs: []model.Breadcrumb{
			{TS: model.At(at.Add(-4 * time.Second)), Category: "app", Message: "cart loaded"},
			{TS: model.At(at.Add(-3 * time.Second)), Category: "db", Message: "SELECT * FROM carts"},
			{TS: model.At(at.Add(-2 * time.Second)), Type: "http", Category: "http.request", Data: model.Attrs{"method": "POST", "url": "https://psp.example/charge?token=secret", "status_code": "402"}},
			{TS: model.At(at.Add(-time.Second)), Category: "payments", Message: "retrying https://psp.example/charge?card=4242 in\n1s"},
		},
		RequestID: "req-1", TraceID: "t1", HTTPMethod: "POST", HTTPRoute: "/pay", HTTPStatus: 500,
		UserIDHash: "hash-of-ana", User: &model.UserInfo{ID: "ana", Email: "ana@example.com"},
		HTTP: &model.HTTPInfo{Method: "POST", Route: "/pay", Status: 500, URL: "https://shop.example/pay?session=abc"},
	}
}

func spikeStore(now time.Time) *fakeStore {
	return &fakeStore{
		rules: []api.AlertRule{spikeRule()},
		spikes: []clickhouse.IssueSpikeRow{
			{IssueID: "i1", Title: "PaymentDeclined: card declined", CurCount: 60, PrevCount: 3, CurUsers: 15},
			{IssueID: "i2", Title: "KeyError: 'sku'", CurCount: 20, PrevCount: 0, CurUsers: 1},
		},
		occs:   map[string][]api.ErrorOccurrence{"i1": {checkoutOccurrence(now.Add(-time.Minute))}},
		traces: map[string]bool{"t1": true},
	}
}

func TestSpikeFiringCarriesContext(t *testing.T) {
	r, n, now := newTestRunner(nil)
	r.store = spikeStore(now)
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatalf("expected one firing, got %d", len(n.events))
	}
	ev := n.events[0]
	if len(ev.TopIssues) != 2 {
		t.Fatalf("top issues: %+v", ev.TopIssues)
	}
	ic := ev.TopIssues[0]
	want := map[string]string{
		"where":   "app/gateway.py, line 41, in call",
		"code":    "raise PaymentDeclined(resp)",
		"request": "POST /pay → 500",
		"release": "1.4.0",
		"url":     "https://pl.example/ui/issues/i1?event=e1",
	}
	got := map[string]string{"where": ic.Where, "code": ic.Code, "request": ic.Request, "release": ic.Release, "url": ic.URL}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	if ic.Users != 15 || ic.Count != 60 || ic.PrevCount != 3 || ic.Reason != "jumped" {
		t.Fatalf("counts: %+v", ic)
	}
	if !strings.Contains(ic.LogsURL, "/ui/logs?") || !strings.Contains(ic.LogsURL, "q=request%3Areq-1") {
		t.Fatalf("logs link: %s", ic.LogsURL)
	}
	if !strings.HasPrefix(ic.TraceURL, "https://pl.example/ui/traces/t1?at=") {
		t.Fatalf("timeline link: %s", ic.TraceURL)
	}
	if len(ic.Steps) != 3 || ic.Steps[0] != "11:58:57 · Database · SELECT * FROM carts" || ic.Steps[1] != "11:58:58 · HTTP · POST https://psp.example/charge (402)" ||
		ic.Steps[2] != "11:58:59 · payments · retrying https://psp.example/charge in 1s" {
		t.Fatalf("steps: %q", ic.Steps)
	}
	for _, s := range ic.Steps {
		if strings.Contains(s, "token=") || strings.Contains(s, "card=") {
			t.Fatalf("a step kept a query string: %q", s)
		}
	}
	// The second issue has no stored occurrence: it is listed with its counts, without detail.
	if ev.TopIssues[1].Where != "" || ev.TopIssues[1].URL != "https://pl.example/ui/issues/i2" {
		t.Fatalf("second issue: %+v", ev.TopIssues[1])
	}

	// The payload carries the context but never who, request URLs or the steps.
	b, _ := json.Marshal(ev)
	s := string(b)
	for _, want := range []string{`"top_issues":[`, `"context":{`, `"title":"Problems suddenly jumped"`, `"where":"app/gateway.py, line 41, in call"`, `"users":15`} {
		if !strings.Contains(s, want) {
			t.Fatalf("payload lacks %s: %s", want, s)
		}
	}
	for _, leak := range []string{"ana@example.com", "hash-of-ana", `"ana"`, "session=abc", "token=secret", "cart loaded", "steps", "0001-01-01"} {
		if strings.Contains(s, leak) {
			t.Fatalf("payload leaks %q: %s", leak, s)
		}
	}
	store := r.store.(*fakeStore)
	if len(store.firings) != 1 || !strings.Contains(mustJSON(t, store.firings[0].Payload), `"top_issues"`) {
		t.Fatalf("the stored firing should carry the context: %+v", store.firings)
	}
}

func TestTimelineLinkOnlyWhenTheTraceWasStored(t *testing.T) {
	r, n, now := newTestRunner(nil)
	store := spikeStore(now)
	store.traces = nil
	r.store = store
	r.RunOnce(context.Background())
	if len(n.events) != 1 || n.events[0].TopIssues[0].TraceURL != "" || n.events[0].TopIssues[0].LogsURL == "" {
		t.Fatalf("no spans, so no timeline link: %+v", n.events)
	}
}

func TestEnrichmentStaysWithinItsBudget(t *testing.T) {
	r, n, now := newTestRunner(nil)
	store := spikeStore(now)
	store.block = true
	r.store = store
	r.enrichBudget = 50 * time.Millisecond
	before := metrics.AlertEnrichFailure.Get("timeout")
	start := time.Now()
	r.RunOnce(context.Background())
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a stuck lookup held the alert for %s", took)
	}
	if len(n.events) != 1 {
		t.Fatalf("the alert must still be sent: %d", len(n.events))
	}
	if ic := n.events[0].TopIssues[0]; ic.Where != "" || ic.Count != 60 {
		t.Fatalf("counts stay, details are skipped: %+v", ic)
	}
	if metrics.AlertEnrichFailure.Get("timeout") <= before {
		t.Fatal("a timed-out enrichment must be counted")
	}
}

func TestEnrichmentPanicIsRecovered(t *testing.T) {
	r, n, now := newTestRunner(nil)
	store := spikeStore(now)
	store.panics = true
	r.store = store
	before := metrics.AlertEnrichFailure.Get("panic")
	r.RunOnce(context.Background())
	if len(n.events) != 1 || len(n.events[0].TopIssues) != 0 || len(n.events[0].Spikes) != 2 {
		t.Fatalf("the alert must be sent without context: %+v", n.events)
	}
	if metrics.AlertEnrichFailure.Get("panic") != before+1 {
		t.Fatal("a panic must be counted")
	}
}

func TestEnrichmentGivesUpBeforeThePassDeadline(t *testing.T) {
	r, _, now := newTestRunner(nil)
	store := spikeStore(now)
	store.block = true
	r.store = store
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ev := AlertEvent{Rule: spikeRule(), Kind: "issue_spike", WindowStart: now.Add(-5 * time.Minute), WindowEnd: now, Spikes: []IssueSpike{{IssueID: "i1", Title: "x", CurCount: 9}}}
	start := time.Now()
	r.enrich(ctx, &ev)
	if took := time.Since(start); took > 150*time.Millisecond {
		t.Fatalf("enrichment should use at most half of what is left of the pass, took %s", took)
	}
}

func TestErrorRateNamesTheTopIssues(t *testing.T) {
	rule := api.AlertRule{ID: "r3", ProjectID: "p1", Name: "Errors", ConditionType: "error_rate", Threshold: 0.1, TimeWindowSeconds: 300, Enabled: true}
	r, n, now := newTestRunner(nil)
	store := spikeStore(now)
	store.rules, store.count = []api.AlertRule{rule}, 42
	r.store = store
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatalf("expected a firing")
	}
	ev := n.events[0]
	if len(ev.TopIssues) != 2 || ev.TopIssues[0].Reason != "most_frequent" || ev.TopIssues[0].Where == "" {
		t.Fatalf("top issues: %+v", ev.TopIssues)
	}
	if h := headline(ev); h != "42 errors in 5 minutes, 8.4 a minute. The limit is 6 a minute." {
		t.Fatalf("headline: %q", h)
	}
}

func TestLogQueryFiringCarriesSampleLines(t *testing.T) {
	r, n, now := newTestRunner(nil)
	store := &fakeStore{rules: []api.AlertRule{logRule(`level:error`, "gt", 2)}, count: 5}
	for i := 0; i < 5; i++ {
		store.logs = append(store.logs, api.LogEvent{TS: now.Add(-time.Duration(i) * time.Minute), Level: "error", Service: "api", Message: "timed out\n  after 5s"})
	}
	r.store = store
	r.RunOnce(context.Background())
	if len(n.events) != 1 || len(n.events[0].Samples) != 3 || n.events[0].Samples[0].Message != "timed out after 5s" {
		t.Fatalf("samples: %+v", n.events)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
