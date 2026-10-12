package alerting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func recapRule(tz string, hour uint8) api.AlertRule {
	return api.AlertRule{ID: "recap", ProjectID: "p1", Name: "Morning recap", ConditionType: "daily_recap", SendHour: hour, Timezone: tz,
		TimeWindowSeconds: 86400, CooldownSeconds: 86400, Enabled: true}
}

func TestRecapIsDueAfterTheLocalHour(t *testing.T) {
	rule := recapRule("Asia/Manila", 8) // UTC+8
	if _, _, _, due, err := recapDue(rule, time.Date(2026, 10, 11, 23, 30, 0, 0, time.UTC)); err != nil || due {
		t.Fatalf("07:30 in Manila is before 8:00: due=%v err=%v", due, err)
	}
	day, from, to, due, err := recapDue(rule, time.Date(2026, 10, 12, 0, 30, 0, 0, time.UTC))
	if err != nil || !due || day != "2026-10-12" {
		t.Fatalf("08:30 in Manila is due: %v %q %v", due, day, err)
	}
	if !from.Equal(time.Date(2026, 10, 10, 16, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("yesterday in Manila: %s .. %s", from.UTC(), to.UTC())
	}
	// The day the clocks go back in New York is 25 hours long.
	_, from, to, due, err = recapDue(recapRule("America/New_York", 8), time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC))
	if err != nil || !due || to.Sub(from) != 25*time.Hour {
		t.Fatalf("DST day: due=%v %s err=%v", due, to.Sub(from), err)
	}
	if _, _, _, _, err := recapDue(recapRule("Mars/Olympus", 8), time.Now()); err == nil {
		t.Fatal("an unknown time zone is an error")
	}
}

func TestRecapIsSentOncePerDay(t *testing.T) {
	store := &fakeStore{rules: []api.AlertRule{recapRule("UTC", 8)}}
	r, n, _ := newTestRunner(store) // 12:00 UTC
	r.RunOnce(context.Background())
	r.RunOnce(context.Background())
	again := NewRunnerWith(r.cfg, store, r.notifiers) // a restart the same day
	again.now = r.now
	again.RunOnce(context.Background())
	if len(n.events) != 1 || n.events[0].Kind != "daily_recap" || n.events[0].Recap == nil {
		t.Fatalf("one recap per day: %d", len(n.events))
	}
	if !n.events[0].WindowStart.Equal(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("the recap covers yesterday: %s", n.events[0].WindowStart)
	}
	r.now = func() time.Time { return time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC) }
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatal("the next day waits for the send hour")
	}
	r.now = func() time.Time { return time.Date(2026, 10, 12, 8, 1, 0, 0, time.UTC) }
	r.RunOnce(context.Background())
	if len(n.events) != 2 {
		t.Fatalf("the next day's recap goes out after 8:00: %d", len(n.events))
	}
}

func recapStore() *fakeStore {
	y := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC) // yesterday for the test runner's clock
	return &fakeStore{
		rules:   []api.AlertRule{recapRule("UTC", 8)},
		project: clickhouse.ProjectView{ID: "p1", Name: "Shop"},
		overview: map[time.Time]api.Overview{
			y:                   {Errors: 142, AffectedUsers: 12, Events: 12300, Services: 4},
			y.AddDate(0, 0, -1): {Errors: 118},
		},
		health: []api.ServiceHealth{
			{Service: "api", Status: "ok", Requests: 9000, Failed: 90},
			{Service: "billing", Status: "failing", Requests: 1000, Failed: 72, FailedPct: 7.2},
			{Service: "inventory", Status: "silent", LastSeen: y.Add(22 * time.Hour)},
		},
		newIssues: []api.IssueSummary{{IssueID: "n1", Title: "KeyError: 'sku'", OccurrenceCount: 12, AffectedUsers: 3}},
		comebacks: []clickhouse.RegressionEvent{{ID: "a1", IssueID: "c1", Issue: api.IssueSummary{IssueID: "c1", Title: "PaymentDeclined", OccurrenceCount: 40}}},
		top:       []api.IssueSummary{{IssueID: "t1", Title: "TimeoutError", OccurrenceCount: 60, AffectedUsers: 15}},
		ruleList:  []api.AlertRule{{ID: "r1", Name: "Checkout errors", ConditionType: "error_rate", Enabled: true}, recapRule("UTC", 8)},
		sentList: []api.AlertFiring{
			{RuleID: "r1", FiredAt: y.Add(10 * time.Hour)}, {RuleID: "r1", FiredAt: y.Add(14 * time.Hour)},
			{RuleID: "gone", FiredAt: y.Add(15 * time.Hour)},
			{RuleID: "r1", FiredAt: y.AddDate(0, 0, -1)},     // the day before: not counted
			{RuleID: "recap", FiredAt: y.Add(8 * time.Hour)}, // the recap itself: not counted
		},
		openIncs: []clickhouse.OpenIncident{{RuleID: "r1", Since: y.Add(14 * time.Hour)}},
	}
}

func TestRecapContent(t *testing.T) {
	store := recapStore()
	r, n, _ := newTestRunner(store)
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatalf("expected a recap")
	}
	ev := n.events[0]
	rc := ev.Recap
	if rc.Project != "Shop" || rc.Totals.Errors != 142 || rc.Previous.Errors != 118 || rc.Totals.Requests != 10000 || rc.Totals.FailedRequests != 162 ||
		rc.Totals.NewProblems != 1 || rc.Totals.CameBack != 1 || len(rc.Apps) != 2 || len(rc.Ongoing) != 1 {
		t.Fatalf("recap: %+v", rc)
	}
	if len(rc.Alerts) != 2 || rc.Alerts[0] != (RecapAlert{Rule: "Checkout errors", Count: 2}) || rc.Alerts[1] != (RecapAlert{Rule: "A deleted rule", Count: 1}) {
		t.Fatalf("alerts sent yesterday: %+v", rc.Alerts)
	}
	m := buildMessage(ev)
	if m.Title != "Yesterday in Shop" || m.Window != "Saturday, Oct 10 (UTC)" {
		t.Fatalf("title/window: %q %q", m.Title, m.Window)
	}
	if want := "Oct 10: 142 errors, up 20% from the day before. 1 new problem, 1 came back. 1.62% of requests failed."; m.Headline != want {
		t.Fatalf("headline:\n got %q\nwant %q", m.Headline, want)
	}
	plain := renderPlain(m)
	for _, want := range []string{"New problems:", "• KeyError: 'sku': 12 times · 3 people  https://pl.example/ui/issues/n1", "Came back:", "Happened most:",
		"Apps that need a look:", "billing is failing: 7.2% of requests failed", "inventory went silent: last heard from Oct 10 22:00 UTC",
		"Still going on:", "Checkout errors: too many errors, since Oct 10 14:00 UTC", "Alerts sent:", "Checkout errors: 2 · A deleted rule: 1",
		"Open in Pyloggle: https://pl.example/ui/overview?from=2026-10-10T00%3A00%3A00Z&to=2026-10-11T00%3A00%3A00Z"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain recap lacks %q:\n%s", want, plain)
		}
	}
	html, mode := telegramText(m, true)
	if mode != "HTML" || !strings.Contains(html, "📋 <b>Yesterday in Shop</b>") || !strings.Contains(html, `• <a href="https://pl.example/ui/issues/t1">TimeoutError</a>: 60 times · 15 people`) {
		t.Fatalf("telegram recap:\n%s", html)
	}
	b, _ := json.Marshal(renderSlack(m))
	if !strings.Contains(string(b), "*Apps that need a look*") {
		t.Fatalf("slack recap: %s", b)
	}
	payload, _ := json.Marshal(ev)
	if !strings.Contains(string(payload), `"recap":{"date":"2026-10-10","timezone":"UTC","project":"Shop"`) || !strings.Contains(string(payload), `"summary":"Oct 10: 142 errors`) {
		t.Fatalf("payload: %s", payload)
	}
}

func TestRecapOfAQuietDay(t *testing.T) {
	y := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{rules: []api.AlertRule{recapRule("UTC", 8)}, overview: map[time.Time]api.Overview{y: {Events: 12300, Services: 4}}}
	r, n, _ := newTestRunner(store)
	r.RunOnce(context.Background())
	if len(n.events) != 1 {
		t.Fatal("a quiet day still gets a recap")
	}
	m := buildMessage(n.events[0])
	if m.Headline != "Oct 10 was quiet: no errors. 12,300 messages from 4 apps." || len(m.Sections) != 0 {
		t.Fatalf("quiet day: %q %+v", m.Headline, m.Sections)
	}
}

func TestRecapLongListsFitTelegram(t *testing.T) {
	rc := Recap{Date: "2026-10-10", Timezone: "UTC", Project: "Shop", Totals: RecapTotals{Errors: 5, NewProblems: 400}}
	for i := 0; i < 400; i++ {
		rc.New = append(rc.New, RecapIssue{IssueID: "x", Title: strings.Repeat("Very long problem title ", 6), Count: 1, URL: "https://pl.example/ui/issues/x"})
	}
	m := buildMessage(AlertEvent{Kind: "daily_recap", Rule: recapRule("UTC", 8), Recap: &rc, WindowStart: time.Now(), WindowEnd: time.Now()})
	text, mode := telegramText(m, true)
	if mode != "HTML" || utf16Len(text) > telegramLimit {
		t.Fatalf("long recap: %q, %d units", mode, utf16Len(text))
	}
}

func TestChangeWords(t *testing.T) {
	for _, c := range []struct {
		cur, prev uint64
		want      string
	}{{0, 0, "none the day before either"}, {5, 0, "up from none the day before"}, {5, 5, "the same as the day before"}, {150, 100, "up 50% from the day before"},
		{97, 100, "down 3% from the day before"}, {2286, 98, "up from 98 the day before"}, {199, 100, "up 99% from the day before"}, {104, 100, "up 4% from the day before"}, {1005, 1000, "up 0.5% from the day before"}} {
		if got := changeWords(c.cur, c.prev); got != c.want {
			t.Fatalf("changeWords(%d, %d) = %q, want %q", c.cur, c.prev, got, c.want)
		}
	}
}

func TestPreviewRecap(t *testing.T) {
	text, err := PreviewRecap(context.Background(), config.Config{PublicURL: "https://pl.example"}, recapStore(), recapRule("UTC", 8), time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC))
	if err != nil || !strings.HasPrefix(text, "Yesterday in Shop: Morning recap\n") || !strings.Contains(text, "Oct 10: 142 errors") {
		t.Fatalf("preview: %v\n%s", err, text)
	}
}
