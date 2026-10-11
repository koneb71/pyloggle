package alerting

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
	"pyloggle/internal/storage/clickhouse"
)

type fakeStore struct {
	mu        sync.Mutex
	rules     []api.AlertRule
	lastFired map[string]time.Time
	count     uint64
	lastQuery clickhouse.LogQuery
	firings   []clickhouse.AlertFiring
}

func (f *fakeStore) ListEnabledAlertRules(context.Context) ([]api.AlertRule, error) {
	return f.rules, nil
}
func (f *fakeStore) GetLastAlertFiredAt(_ context.Context, id string) (time.Time, bool, error) {
	t, ok := f.lastFired[id]
	return t, ok, nil
}
func (f *fakeStore) InsertAlertFiring(_ context.Context, fr clickhouse.AlertFiring) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.firings = append(f.firings, fr)
	return nil
}
func (f *fakeStore) CountErrorOccurrences(context.Context, api.AlertRule, time.Time, time.Time) (uint64, error) {
	return f.count, nil
}
func (f *fakeStore) IssueSpikeCounts(context.Context, api.AlertRule, time.Time, time.Time, time.Time, time.Time, int) ([]clickhouse.IssueSpikeRow, error) {
	return nil, nil
}
func (f *fakeStore) NewIssues(context.Context, api.AlertRule, time.Time, time.Time, int) ([]api.IssueSummary, error) {
	return nil, nil
}
func (f *fakeStore) RegressedIssues(context.Context, api.AlertRule, time.Time, time.Time, int) ([]api.IssueSummary, error) {
	return nil, nil
}
func (f *fakeStore) CountLogs(_ context.Context, q clickhouse.LogQuery) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = q
	return f.count, nil
}

type fakeNotifier struct {
	mu     sync.Mutex
	events []AlertEvent
}

func (n *fakeNotifier) Name() string { return "fake" }
func (n *fakeNotifier) Notify(_ context.Context, ev AlertEvent) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, ev)
	return nil
}

func newTestRunner(store *fakeStore) (*Runner, *fakeNotifier, time.Time) {
	cfg := config.Config{PublicURL: "https://pl.example/", AlertingEvalDelay: 30 * time.Second}
	n := &fakeNotifier{}
	r := NewRunnerWith(cfg, store, []Notifier{n})
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	return r, n, now
}

func logRule(query, cmp string, threshold float64) api.AlertRule {
	return api.AlertRule{ID: "r1", ProjectID: "p1", Name: "api errors", Service: "api", ConditionType: "log_query", Query: query, Comparison: cmp,
		Threshold: threshold, TimeWindowSeconds: 300, CooldownSeconds: 600, Enabled: true,
		NotificationEmails: []string{"oncall@example.com"}, WebhookURLs: []string{"https://hooks.example/secret"}}
}

func TestLogQueryRuleFiresWithCountAndLink(t *testing.T) {
	store := &fakeStore{rules: []api.AlertRule{logRule(`level:error "timed out"`, "gt", 2)}, count: 5}
	r, n, now := newTestRunner(store)
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatalf("expected one firing, got %d", len(n.events))
	}
	ev := n.events[0]
	if ev.Kind != "log_query" || ev.Count != 5 {
		t.Fatalf("event: %+v", ev)
	}
	if !strings.HasPrefix(ev.URL, "https://pl.example/ui/logs?") || !strings.Contains(ev.URL, "q=level%3Aerror+%22timed+out%22") {
		t.Fatalf("url: %s", ev.URL)
	}
	// The evaluation window ends eval-delay before now.
	if !ev.WindowEnd.Equal(now.Add(-30*time.Second)) || !ev.WindowStart.Equal(now.Add(-330*time.Second)) {
		t.Fatalf("window: %s .. %s", ev.WindowStart, ev.WindowEnd)
	}
	if store.lastQuery.ProjectID != "p1" || store.lastQuery.Service != "api" || store.lastQuery.Where == "" {
		t.Fatalf("query not scoped: %+v", store.lastQuery)
	}
	if len(store.firings) != 1 || store.firings[0].Count != 5 || store.firings[0].URL != ev.URL {
		t.Fatalf("firing row: %+v", store.firings)
	}
}

func TestAbsenceRuleAndComparisons(t *testing.T) {
	store := &fakeStore{rules: []api.AlertRule{logRule(`job:nightly-export`, "lt", 1)}, count: 0}
	r, n, _ := newTestRunner(store)
	r.RunOnce(context.Background())
	if len(n.events) != 1 || n.events[0].Count != 0 {
		t.Fatalf("absence rule should fire on zero lines: %+v", n.events)
	}
	store.count = 3
	n.events = nil
	store.lastFired = nil
	r.RunOnce(context.Background())
	if len(n.events) != 0 {
		t.Fatalf("absence rule must not fire when lines exist")
	}
	for _, tc := range []struct {
		op   string
		v, t float64
		want bool
	}{{"", 3, 2, true}, {"gt", 2, 2, false}, {"gte", 2, 2, true}, {"lt", 1, 2, true}, {"lte", 2, 2, true}, {"eq", 2, 2, true}, {"eq", 3, 2, false}, {"nope", 3, 2, false}} {
		if got := Compare(tc.op, tc.v, tc.t); got != tc.want {
			t.Fatalf("Compare(%q, %v, %v) = %v", tc.op, tc.v, tc.t, got)
		}
	}
}

func TestCooldownAndBadQuery(t *testing.T) {
	rule := logRule(`level:error`, "gt", 0)
	store := &fakeStore{rules: []api.AlertRule{rule}, count: 10, lastFired: map[string]time.Time{"r1": time.Date(2026, 10, 11, 11, 55, 0, 0, time.UTC)}}
	r, n, _ := newTestRunner(store)
	r.RunOnce(context.Background())
	if len(n.events) != 0 {
		t.Fatalf("cooldown (600s) should suppress a firing 5 minutes after the last one")
	}
	bad := logRule(`level:(error`, "gt", 0)
	bad.ID = "r2"
	store.rules = []api.AlertRule{bad}
	store.lastFired = nil
	r.RunOnce(context.Background())
	r.RunOnce(context.Background())
	if len(n.events) != 0 || r.badRules["r2"] == "" {
		t.Fatalf("invalid query must be skipped and remembered: events=%d bad=%v", len(n.events), r.badRules)
	}
}

func TestPublicPayloadHidesNotificationTargets(t *testing.T) {
	rule := logRule(`level:error`, "gt", 0)
	rule.TelegramChatIDs = []string{"12345"}
	rule.SlackWebhookURLs = []string{"https://hooks.slack.com/services/T/B/x"}
	ev := AlertEvent{Rule: rule, Kind: "log_query", Count: 3, URL: "https://pl.example/ui/logs?q=level%3Aerror", FiredAt: time.Now()}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, secret := range []string{"oncall@example.com", "hooks.example/secret", "12345", "hooks.slack.com", "notification_emails", "webhook_urls"} {
		if strings.Contains(s, secret) {
			t.Fatalf("payload leaks %q: %s", secret, s)
		}
	}
	if !strings.Contains(s, `"rule":{"id":"r1"`) || !strings.Contains(s, `"query":"level:error"`) || !strings.Contains(s, `"count":3`) {
		t.Fatalf("payload shape: %s", s)
	}
	slack := SlackPayload(ev)
	blocks := slack["blocks"].([]map[string]any)
	if blocks[len(blocks)-1]["type"] != "actions" {
		t.Fatalf("slack payload should end with the Open button when a URL is set: %v", blocks)
	}
	ev.URL = ""
	blocks = SlackPayload(ev)["blocks"].([]map[string]any)
	if blocks[len(blocks)-1]["type"] == "actions" {
		t.Fatal("no actions block without a URL (Slack rejects empty button urls)")
	}
}

func TestErrorRateLinkCarriesScope(t *testing.T) {
	rule := api.AlertRule{ID: "r3", ProjectID: "p1", Name: "5xx", Service: "api", Env: "prod", HTTPRoute: "/orders", ConditionType: "error_rate",
		Threshold: 0.001, TimeWindowSeconds: 60, Enabled: true}
	store := &fakeStore{rules: []api.AlertRule{rule}, count: 10}
	r, n, _ := newTestRunner(store)
	r.RunOnce(context.Background())
	if len(n.events) != 1 || !strings.Contains(n.events[0].URL, "q=service%3Aapi+env%3Aprod+http.route%3A%2Forders+level%3Aerror") {
		t.Fatalf("error_rate link: %+v", n.events)
	}
}
