package alerting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func onceRule(kind string, threshold float64) api.AlertRule {
	return api.AlertRule{ID: "r-" + kind, ProjectID: "p1", Name: kind, ConditionType: kind, Threshold: threshold, TimeWindowSeconds: 600, CooldownSeconds: 60, Enabled: true}
}

func issueTitles(ev AlertEvent) []string {
	var out []string
	for _, is := range ev.Issues {
		out = append(out, is.Title)
	}
	for _, s := range ev.Spikes {
		out = append(out, s.Title)
	}
	return out
}

func TestNewProblemAlertsOnce(t *testing.T) {
	store := &fakeStore{rules: []api.AlertRule{onceRule("new_issue", 1)}, newIssues: []api.IssueSummary{{IssueID: "i1", Title: "KeyError"}}}
	r, n, _ := newTestRunner(store)
	r.RunOnce(context.Background())
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatalf("a new problem must alert once, got %d alerts", len(n.events))
	}
	store.newIssues = append(store.newIssues, api.IssueSummary{IssueID: "i2", Title: "TimeoutError"})
	r.RunOnce(context.Background())
	if len(n.events) != 2 || len(n.events[1].Issues) != 1 || n.events[1].Issues[0].Title != "TimeoutError" {
		t.Fatalf("only the second problem is new: %v", issueTitles(n.events[len(n.events)-1]))
	}
	r.RunOnce(context.Background())
	if len(n.events) != 2 {
		t.Fatalf("nothing new, nothing sent: %d", len(n.events))
	}
}

func TestComebackAlertsOncePerComeback(t *testing.T) {
	sum := api.IssueSummary{IssueID: "i1", Title: "PaymentDeclined"}
	store := &fakeStore{rules: []api.AlertRule{onceRule("regression", 1)}, comebacks: []clickhouse.RegressionEvent{{ID: "act-1", IssueID: "i1", Issue: sum}}}
	r, n, _ := newTestRunner(store)
	r.RunOnce(context.Background())
	r.RunOnce(context.Background()) // e.g. someone assigned or commented on it meanwhile
	if len(n.events) != 1 {
		t.Fatalf("one comeback, one alert; got %d", len(n.events))
	}
	// Fixed again, then back again: a new comeback alerts again.
	store.comebacks = append([]clickhouse.RegressionEvent{{ID: "act-2", IssueID: "i1", Issue: sum}}, store.comebacks...)
	r.RunOnce(context.Background())
	if len(n.events) != 2 || len(n.events[1].Issues) != 1 {
		t.Fatalf("a second comeback alerts once more: %d", len(n.events))
	}
}

func TestJumpAlertsAtMostOnceADay(t *testing.T) {
	store := &fakeStore{rules: []api.AlertRule{onceRule("issue_spike", 5)}, spikes: []clickhouse.IssueSpikeRow{{IssueID: "i1", Title: "TimeoutError", CurCount: 40, PrevCount: 2}}}
	r, n, now := newTestRunner(store)
	store.clock = func() time.Time { return r.now() }
	r.RunOnce(context.Background())
	r.now = func() time.Time { return now.Add(23 * time.Hour) }
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatalf("the same problem jumping again within a day stays quiet: %d", len(n.events))
	}
	r.now = func() time.Time { return now.Add(25 * time.Hour) }
	r.RunOnce(context.Background())
	if len(n.events) != 2 {
		t.Fatalf("a day later it may alert again: %d", len(n.events))
	}
}

func TestCountRuleAlertsOncePerIncident(t *testing.T) {
	store := &fakeStore{rules: []api.AlertRule{onceRule("error_rate", 0.01)}, count: 100}
	r, n, _ := newTestRunner(store)
	r.RunOnce(context.Background())
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatalf("while errors continue, one alert: %d", len(n.events))
	}
	store.count = 0
	r.RunOnce(context.Background())
	if len(n.events) != 1 || store.incident["r-error_rate"] {
		t.Fatalf("back to normal closes the incident quietly: events=%d open=%v", len(n.events), store.incident["r-error_rate"])
	}
	store.count = 100
	r.RunOnce(context.Background())
	if len(n.events) != 2 {
		t.Fatalf("the next incident alerts again: %d", len(n.events))
	}
}

type failingNotifier struct{}

func (failingNotifier) Name() string                             { return "broken" }
func (failingNotifier) Notify(context.Context, AlertEvent) error { return errors.New("down") }

func TestFailedDeliveryRecordsNothing(t *testing.T) {
	store := &fakeStore{rules: []api.AlertRule{onceRule("new_issue", 1)}, newIssues: []api.IssueSummary{{IssueID: "i1", Title: "KeyError"}}}
	r, n, _ := newTestRunner(store)
	working := r.notifiers
	r.notifiers = []Notifier{failingNotifier{}}
	r.RunOnce(context.Background())
	if len(store.sent) != 0 {
		t.Fatalf("nothing was delivered, so nothing is recorded: %v", store.sent)
	}
	r.notifiers = working
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatalf("the problem is sent once delivery works: %d", len(n.events))
	}
}
