//go:build integration

package clickhouse_test

// Analytics → Errors and Usage against real tables: problems opened, fixed and coming back, time
// to fix, releases next to their traffic, types, apps and people; usage per endpoint and version.

import (
	"context"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/ingest"
	"github.com/koneb71/pyloggle/internal/issue"
	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

func TestIntegrationErrorAndUsageAnalytics(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.RateLimitRPS = 1, 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	tn := ingest.Tenant{ProjectID: "p1"}
	now := time.Now().UTC().Truncate(time.Second)

	crash := func(typ, svc, release, user, email string, at time.Time) model.Event {
		ev := model.Event{Timestamp: model.At(at), Service: svc, Env: "prod", Release: release, Level: "error", Message: typ + " here",
			Exception: &model.Exception{Type: typ, Message: "x", Frames: []model.Frame{{Filename: "app/" + typ + ".py", Function: "f", Lineno: 1}}}}
		if user != "" {
			ev.User = &model.UserInfo{IDHash: user, Email: email}
		}
		return ev
	}
	send := func(ev model.Event) {
		t.Helper()
		if o := p.Process(ctx, tn, ev, 100); o != ingest.Accepted {
			t.Fatalf("process: %v", o)
		}
	}
	send(crash("ValueError", "shop", "1.3.0", "u1", "ada@example.com", now.Add(-50*time.Hour)))
	send(crash("ValueError", "shop", "1.4.0", "u2", "", now.Add(-2*time.Hour)))
	send(crash("KeyError", "shop", "1.4.0", "u1", "ada@example.com", now.Add(-90*time.Minute)))
	send(crash("TimeoutError", "billing", "1.4.0", "", "", now.Add(-time.Hour)))
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	issues, err := c.QueryIssues(ctx, clickhouse.IssueListQuery{ProjectID: "p1", Limit: 10})
	if err != nil || len(issues) != 3 {
		t.Fatalf("issues: %d %v", len(issues), err)
	}
	ids := map[string]string{}
	for _, is := range issues {
		ids[is.ExceptionType] = is.IssueID
	}
	// KeyError is fixed, then comes back with another crash (a regression), then is fixed again.
	resolve := func(id string) {
		t.Helper()
		if err := c.SetIssueState(ctx, api.IssueStateChange{ProjectID: "p1", IssueID: id, UpdatedBy: "alice", Status: issue.StatusResolved}); err != nil {
			t.Fatal(err)
		}
	}
	resolve(ids["KeyError"])
	p = ingest.NewPipeline(cfg, c, c, nil) // a fresh state cache sees the resolve
	send(crash("KeyError", "shop", "1.4.0", "u3", "", now.Add(-time.Minute)))
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	resolve(ids["KeyError"])
	resolve(ids["TimeoutError"])

	// Traffic: 1.4.0 got 200 requests, 1.3.0 got 50.
	minute := now.Truncate(time.Minute).Add(-30 * time.Minute)
	counts := []model.RequestCount{
		{Minute: model.At(minute), Service: "shop", Env: "prod", Release: "1.4.0", Method: "GET", Route: "/cart", Requests: 150, Errors: 2, Users: []string{"u1", "u2", "u3"}},
		{Minute: model.At(minute), Service: "shop", Env: "prod", Release: "1.4.0", Method: "POST", Route: "/orders", Requests: 50, Users: []string{"u1"}},
		{Minute: model.At(minute.Add(-49 * time.Hour)), Service: "shop", Env: "prod", Release: "1.3.0", Method: "GET", Route: "/cart", Requests: 50, Users: []string{"u4"}},
	}
	if n, _, _ := p.ProcessRequestCounts(ctx, tn, counts, 100); n != 3 {
		t.Fatal("counts")
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	scope := clickhouse.AnalyticsScope{ProjectID: "p1", From: now.Add(-7 * 24 * time.Hour), To: now.Add(time.Minute)}
	ea, err := c.ErrorAnalytics(ctx, scope)
	if err != nil {
		t.Fatalf("errors: %v", err)
	}
	if ea.Totals.Opened != 3 || ea.Totals.Fixed != 3 || ea.Totals.CameBack != 1 || ea.BucketSeconds != 86400 || len(ea.Points) < 7 {
		t.Fatalf("flow: %+v (%d points)", ea.Totals, len(ea.Points))
	}
	var opened, fixed uint64
	for _, pt := range ea.Points {
		opened += pt.Opened
		fixed += pt.Fixed
	}
	if opened != 3 || fixed != 3 {
		t.Fatalf("points add up to the totals: %d %d", opened, fixed)
	}
	if ea.TimeToFix.Fixed != 3 || ea.TimeToFix.WithinHour != 1 || ea.TimeToFix.WithinDay != 2 {
		// TimeoutError: ~1 h; KeyError first fix ~1.5 h; second fix ~1 min after it came back.
		t.Fatalf("time to fix: %+v", ea.TimeToFix)
	}
	byRelease := map[string]api.ReleaseHealth{}
	for _, r := range ea.Releases {
		byRelease[r.Release] = r
	}
	if r := byRelease["1.4.0"]; r.Errors != 4 || r.NewProblems != 2 || r.People != 3 || r.Requests != 200 || r.Problems != 3 {
		t.Fatalf("1.4.0: %+v", r)
	}
	if r := byRelease["1.3.0"]; r.Errors != 1 || r.NewProblems != 1 || r.Requests != 50 {
		t.Fatalf("1.3.0: %+v", r)
	}
	if len(ea.Types) != 3 || ea.Types[0].Key != "KeyError" || ea.Types[0].Errors != 2 || ea.Types[0].People != 2 {
		t.Fatalf("types: %+v", ea.Types)
	}
	if len(ea.Apps) != 2 || ea.Apps[0].Key != "shop" || ea.Apps[0].Errors != 4 || ea.Apps[0].Problems != 2 {
		t.Fatalf("apps: %+v", ea.Apps)
	}
	if len(ea.People) != 3 || ea.People[0].IDHash != "u1" || ea.People[0].Errors != 2 || ea.People[0].Label != "ada@example.com" || ea.People[1].Label != "" {
		t.Fatalf("people: %+v", ea.People)
	}
	// One app at a time.
	shopOnly := scope
	shopOnly.Service = "billing"
	if eb, err := c.ErrorAnalytics(ctx, shopOnly); err != nil || eb.Totals.Opened != 1 || len(eb.Types) != 1 || eb.Types[0].Key != "TimeoutError" {
		t.Fatalf("billing only: %+v %v", eb.Totals, err)
	}

	ua, err := c.UsageAnalytics(ctx, scope, "")
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if ua.Totals.Requests != 250 || ua.Totals.Errors != 2 || ua.Totals.People != 4 {
		t.Fatalf("usage totals: %+v", ua.Totals)
	}
	var reqs uint64
	for _, pt := range ua.Points {
		reqs += pt.Requests
	}
	if reqs != 250 || len(ua.Weeks) == 0 {
		t.Fatalf("usage points: %d, weeks %d", reqs, len(ua.Weeks))
	}
	if len(ua.Endpoints) != 2 || ua.Endpoints[0].Route != "/cart" || ua.Endpoints[0].Requests != 200 || ua.Endpoints[0].People != 4 {
		t.Fatalf("endpoints: %+v", ua.Endpoints)
	}
	if len(ua.Versions) != 2 || ua.Versions[0].Release != "1.4.0" || ua.Versions[0].Requests != 200 || ua.Versions[0].FirstSeen == nil {
		t.Fatalf("versions: %+v", ua.Versions)
	}
	if len(ua.VersionSeries) != 2 || ua.VersionSeries[0].Name != "1.4.0" {
		t.Fatalf("version series: %+v", ua.VersionSeries)
	}
	if byPeople, err := c.UsageAnalytics(ctx, scope, "people"); err != nil || byPeople.Endpoints[0].Route != "/cart" {
		t.Fatalf("by people: %v", err)
	}
	empty, err := c.UsageAnalytics(ctx, clickhouse.AnalyticsScope{ProjectID: "nobody"}, "")
	if err != nil || empty.Totals.Requests != 0 || len(empty.Points) == 0 || empty.Versions == nil {
		t.Fatalf("an empty project still answers: %+v %v", empty, err)
	}
}
