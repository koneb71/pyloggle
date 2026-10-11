//go:build integration

package clickhouse_test

// Issue workflow: assignee survives a system regression, resolve-in-release only regresses on a
// newer release, snoozes round-trip, activity and the releases MV are populated.

import (
	"context"
	"strings"
	"testing"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/issue"
	"pyloggle/internal/model"
	"pyloggle/internal/storage/clickhouse"
)

func TestIntegrationIssueWorkflowState(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()

	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.SampleDebug, cfg.SampleWarn = 1, 1, 1
	cfg.RateLimitRPS = 0
	tn := ingest.Tenant{ProjectID: "p1"}
	crash := func(release string, at time.Time) model.Event {
		return model.Event{Timestamp: model.At(at), Service: "web", Env: "prod", Level: "error", Logger: "web.orders", Message: "charge failed", Release: release,
			Exception: &model.Exception{Type: "ValueError", Module: "builtins", Message: "bad amount", Frames: []model.Frame{
				{Filename: "/srv/app/orders.py", Module: "app.orders", Function: "charge", Lineno: 12, Context: "raise ValueError"},
			}}}
	}
	// Each step uses a fresh pipeline so its state cache reflects the rows written by the API path.
	send := func(ev model.Event) {
		t.Helper()
		p := ingest.NewPipeline(cfg, c, c, nil)
		if o := p.Process(ctx, tn, ev, 500); o != ingest.Accepted {
			t.Fatalf("event dropped: %v", o)
		}
		if err := c.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	state := func() issue.State {
		t.Helper()
		st, ok, err := c.GetIssueState(ctx, "p1", issueIDOf(t, c))
		if err != nil || !ok {
			t.Fatalf("GetIssueState: ok=%v err=%v", ok, err)
		}
		return st
	}
	str := func(s string) *string { return &s }

	now := time.Now().UTC().Truncate(time.Millisecond)
	send(crash("build-1", now.Add(-5*time.Minute)))
	id := issueIDOf(t, c)

	// Assign.
	if err := c.SetIssueState(ctx, api.IssueStateChange{ProjectID: "p1", IssueID: id, UpdatedBy: "alice", Assignee: str("bob")}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if st := state(); st.Status != issue.StatusOpen || st.Assignee != "bob" {
		t.Fatalf("after assign: %+v", st)
	}

	// Resolve in a release that is newer than build-1 (first-seen order, not semver).
	send(crash("build-2", now.Add(-4*time.Minute)))
	if err := c.SetIssueState(ctx, api.IssueStateChange{ProjectID: "p1", IssueID: id, UpdatedBy: "alice", Status: issue.StatusResolved, ResolvedInRelease: str("build-2")}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if st := state(); st.Status != issue.StatusResolved || st.ResolvedInRelease != "build-2" || st.Assignee != "bob" {
		t.Fatalf("after resolve: %+v", st)
	}

	// An occurrence from the older build is not a regression; one from a newer build is, and the
	// system transition keeps the assignee (read-modify-write).
	send(crash("build-1", now.Add(-3*time.Minute)))
	if st := state(); st.Status != issue.StatusResolved {
		t.Fatalf("old release must not regress: %+v", st)
	}
	send(crash("build-3", now.Add(-2*time.Minute)))
	if st := state(); st.Status != issue.StatusOpen || !st.Regressed || st.RegressedFrom != issue.StatusResolved || st.Assignee != "bob" || st.ResolvedInRelease != "" {
		t.Fatalf("after regression: %+v", st)
	}

	// Snooze with both an expiry and an occurrence budget.
	until := now.Add(time.Hour).Truncate(time.Second)
	n := uint64(50)
	if err := c.SetIssueState(ctx, api.IssueStateChange{ProjectID: "p1", IssueID: id, UpdatedBy: "alice", Status: issue.StatusIgnored, SnoozeUntil: &until, SnoozeCount: &n}); err != nil {
		t.Fatalf("snooze: %v", err)
	}
	st := state()
	if st.Status != issue.StatusIgnored || !st.SnoozeUntil.Equal(until) || st.SnoozeCount != 50 || st.OccurrenceCount != 4 || st.Assignee != "bob" {
		t.Fatalf("after snooze: %+v", st)
	}
	sum, ok, err := c.GetIssueSummary(ctx, "p1", id)
	if err != nil || !ok || sum.SnoozeUntil == nil || !sum.SnoozeUntil.Equal(until) || sum.SnoozeCount != 50 || sum.Status != issue.StatusIgnored {
		t.Fatalf("summary: ok=%v err=%v %+v", ok, err, sum)
	}

	// Comment + activity log (oldest first after reversing the newest-first list).
	if err := c.AddIssueComment(ctx, "p1", id, "carol", "looking into it"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	acts, err := c.ListIssueActivity(ctx, "p1", id, 50)
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	var kinds []string
	for i := len(acts) - 1; i >= 0; i-- {
		kinds = append(kinds, acts[i].Kind)
	}
	if got := strings.Join(kinds, ","); got != "assign,resolve_in_release,regression,snooze,comment" {
		t.Fatalf("activity kinds: %s (%+v)", got, acts)
	}
	if acts[0].Comment != "looking into it" || acts[0].Actor != "carol" {
		t.Fatalf("latest activity: %+v", acts[0])
	}
	for i := 1; i < len(acts); i++ {
		if acts[i].TS.After(acts[i-1].TS) {
			t.Fatalf("activity must be newest first: %v then %v", acts[i-1].TS, acts[i].TS)
		}
	}
	for _, a := range acts {
		if a.Kind == "regression" && a.Release != "build-3" {
			t.Fatalf("regression activity should carry the release: %+v", a)
		}
		if a.Kind == "resolve_in_release" && a.Release != "build-2" {
			t.Fatalf("resolve activity should carry the release: %+v", a)
		}
	}

	// Assignee suggestions: actors and assignees, never "system".
	people, err := c.ListAssignees(ctx, "p1")
	if err != nil || strings.Join(people, ",") != "alice,bob,carol" {
		t.Fatalf("assignees: %v %v", people, err)
	}

	// releases MV: first-seen per release, in ingest order.
	b1, ok1, err1 := c.ReleaseFirstSeen(ctx, "p1", "build-1")
	b3, ok3, err3 := c.ReleaseFirstSeen(ctx, "p1", "build-3")
	if err1 != nil || err3 != nil || !ok1 || !ok3 || !b1.Before(b3) {
		t.Fatalf("releases: %v %v %v %v %v %v", b1, ok1, err1, b3, ok3, err3)
	}
	if _, ok, err := c.ReleaseFirstSeen(ctx, "p1", "never"); err != nil || ok {
		t.Fatalf("unknown release: ok=%v err=%v", ok, err)
	}

	// Assignee filter on the list.
	from, to := now.Add(-time.Hour), now.Add(time.Minute)
	for _, tc := range []struct {
		assignee string
		want     int
	}{{"bob", 1}, {"none", 0}, {"carol", 0}, {"", 1}} {
		got, err := c.QueryIssues(ctx, clickhouse.IssueListQuery{ProjectID: "p1", From: from, To: to, Limit: 10, Status: "all", Assignee: tc.assignee})
		if err != nil || len(got) != tc.want {
			t.Fatalf("assignee=%q: %d issues, err=%v", tc.assignee, len(got), err)
		}
	}
}

func issueIDOf(t *testing.T, c *clickhouse.Client) string {
	t.Helper()
	issues, err := c.QueryIssues(context.Background(), clickhouse.IssueListQuery{ProjectID: "p1", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Minute), Limit: 10, Status: "all"})
	if err != nil || len(issues) != 1 {
		t.Fatalf("expected exactly one issue: %d %v", len(issues), err)
	}
	return issues[0].IssueID
}
