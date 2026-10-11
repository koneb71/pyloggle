//go:build integration

package clickhouse_test

// End-to-end: fixtures -> ingest.Pipeline -> ClickHouse -> every query the UI uses.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"testing"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/issue"
	"pyloggle/internal/model"
	"pyloggle/internal/storage/clickhouse"
)

func e2eClient(t *testing.T) (*clickhouse.Client, func()) {
	t.Helper()
	dsn := os.Getenv("PYLOGGLE_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("PYLOGGLE_TEST_CLICKHOUSE_DSN not set")
	}
	ctx := context.Background()
	admin, err := clickhouse.New(dsn, 100, 10, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	db := fmt.Sprintf("pyloggle_e2e_%d", rand.Int63())
	if err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	u.Path = "/" + db
	c, err := clickhouse.New(u.String(), 1000, 50, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Migrate(ctx, clickhouse.MigrateOptions{Rebuild: true, Logf: func(string, ...any) {}}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return c, func() {
		_ = c.Close()
		_ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+db)
		_ = admin.Close()
	}
}

func fixtureEvents(t *testing.T, path string) []model.Event {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []model.Event
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev model.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, ev)
	}
	return out
}

func TestIntegrationPipelineEndToEnd(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	cfg := config.FromEnv()
	cfg.SampleInfo, cfg.SampleDebug, cfg.SampleWarn = 1, 1, 1
	cfg.RateLimitRPS = 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	tn := ingest.Tenant{ProjectID: "p1"}

	// Fixtures, re-timestamped into the last few minutes so retention and default ranges apply.
	var events []model.Event
	events = append(events, fixtureEvents(t, "../../model/testdata/sdk_v0.1.ndjson")...)
	events = append(events, fixtureEvents(t, "../../model/testdata/sdk_v0.2.ndjson")...)
	for i := range events {
		events[i].Timestamp = model.At(now.Add(-time.Duration(len(events)-i) * time.Second))
	}
	// A synthetic request burst: 3 requests, each start/done + one exception, same code path.
	var burstErrorIDs []string
	for r := 1; r <= 3; r++ {
		rid := fmt.Sprintf("req-%d", r)
		base := now.Add(-time.Duration(60-r*3) * time.Second)
		events = append(events,
			model.Event{Timestamp: model.At(base), Service: "web", Env: "prod", Level: "info", Logger: "web.http", Message: fmt.Sprintf("handled order %d", 100+r), RequestID: rid, Attrs: model.Attrs{"user_id": "u1", "latency_ms": "12"}, Tags: model.Attrs{"team": "core"}},
			model.Event{Timestamp: model.At(base.Add(time.Second)), Service: "web", Env: "prod", Level: "error", Logger: "web.orders", Message: "charge failed", RequestID: rid, EventID: fmt.Sprintf("burst-%d", r),
				User: &model.UserInfo{IDHash: fmt.Sprintf("user-%d", r), Email: "a@b.c"},
				HTTP: &model.HTTPInfo{Method: "POST", Route: "/orders", Status: 500, URL: "http://h/orders?x=1", Headers: model.Attrs{"user-agent": "ua/1"}},
				Exception: &model.Exception{Type: "ValueError", Module: "builtins", Message: fmt.Sprintf("bad amount %d", r), Frames: []model.Frame{
					{Filename: "/srv/app/orders.py", Module: "app.orders", Function: "charge", Lineno: uint32(10 + r), Context: "raise ValueError", Vars: model.Attrs{"amount": "-1"}},
				}}},
			model.Event{Timestamp: model.At(base.Add(2 * time.Second)), Service: "web", Env: "prod", Level: "info", Logger: "web.http", Message: "request done", RequestID: rid, Attrs: model.Attrs{"user_id": "u1"}},
		)
		burstErrorIDs = append(burstErrorIDs, fmt.Sprintf("burst-%d", r))
	}
	for _, ev := range events {
		if o := p.Process(ctx, tn, ev, 500); o != ingest.Accepted {
			t.Fatalf("event dropped: %v", o)
		}
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	from, to := now.Add(-time.Hour), now.Add(time.Minute)

	// --- issues list, counts, sparklines ---
	issues, err := c.QueryIssues(ctx, clickhouse.IssueListQuery{ProjectID: "p1", From: from, To: to, Limit: 50})
	if err != nil {
		t.Fatalf("QueryIssues: %v", err)
	}
	if len(issues) < 4 {
		t.Fatalf("expected several issues, got %d", len(issues))
	}
	var burst api.IssueSummary
	var ids []string
	var totalOcc uint64
	for _, is := range issues {
		ids = append(ids, is.IssueID)
		totalOcc += is.OccurrenceCount
		if is.Title == "" || is.Status != "open" {
			t.Fatalf("bad summary: %+v", is)
		}
		if is.ExceptionType == "ValueError" && is.Service == "web" {
			burst = is
		}
	}
	if burst.IssueID == "" || burst.OccurrenceCount != 3 || burst.AffectedUsers != 3 || burst.Culprit != "app.orders in charge" {
		t.Fatalf("burst issue (line numbers differ, same code path): %+v", burst)
	}
	if burst.Title != "ValueError: bad amount 3" {
		t.Fatalf("title should follow the latest occurrence: %q", burst.Title)
	}
	counts, err := c.CountIssuesByStatus(ctx, clickhouse.IssueListQuery{ProjectID: "p1", From: from, To: to})
	if err != nil || counts["open"] != uint64(len(issues)) || counts["all"] != uint64(len(issues)) {
		t.Fatalf("counts: %v %v", counts, err)
	}
	if got, _ := c.QueryIssues(ctx, clickhouse.IssueListQuery{ProjectID: "p1", Q: "bad amount", Limit: 10}); len(got) != 1 || got[0].IssueID != burst.IssueID {
		t.Fatalf("search by message: %+v", got)
	}
	if got, _ := c.QueryIssues(ctx, clickhouse.IssueListQuery{ProjectID: "p1", Sort: "count", Limit: 1}); len(got) != 1 || got[0].IssueID != burst.IssueID {
		t.Fatalf("sort by count: %+v", got)
	}
	sp, err := c.IssueSparklines(ctx, "p1", ids, 24)
	if err != nil {
		t.Fatalf("sparklines: %v", err)
	}
	var spSum uint64
	for _, s := range sp {
		for _, n := range s {
			spSum += n
		}
	}
	if spSum != totalOcc {
		t.Fatalf("sparkline sum %d != occurrences %d", spSum, totalOcc)
	}
	trend, err := c.IssueTrend(ctx, "p1", burst.IssueID, from, to, 60)
	if err != nil {
		t.Fatalf("trend: %v", err)
	}
	var trendSum uint64
	for _, pt := range trend {
		trendSum += pt.Count
	}
	if trendSum != 3 || len(trend) < 50 {
		t.Fatalf("trend sum %d buckets %d", trendSum, len(trend))
	}
	bd, err := c.IssueBreakdown(ctx, "p1", burst.IssueID, "http_route", from, to, 5)
	if err != nil || len(bd) != 1 || bd[0].Value != "/orders" || bd[0].Count != 3 || bd[0].Users != 3 {
		t.Fatalf("breakdown: %+v %v", bd, err)
	}

	// --- occurrence detail with v2 report fields and prev/next ---
	// The v0.2 fixture is generated by the real SDK (tests/gen_golden.py); compare the stored
	// occurrence against the decoded fixture so regenerating it never silently breaks this test.
	var want model.Event
	for _, ev := range fixtureEvents(t, "../../model/testdata/sdk_v0.2.ndjson") {
		if ev.Exception != nil && len(ev.Exception.Frames) > 0 {
			want = ev
			break
		}
	}
	if want.EventID == "" {
		t.Fatal("sdk_v0.2.ndjson has no event with an exception")
	}
	occ, ok, err := c.GetOccurrence(ctx, "p1", want.EventID)
	if err != nil || !ok {
		t.Fatalf("GetOccurrence v0.2: %v %v", ok, err)
	}
	wf := want.Exception.Frames[len(want.Exception.Frames)-1]
	if len(occ.Frames) != len(want.Exception.Frames) {
		t.Fatalf("frames: got %d want %d", len(occ.Frames), len(want.Exception.Frames))
	}
	of := occ.Frames[len(occ.Frames)-1]
	if len(wf.Vars) == 0 || len(of.Vars) != len(wf.Vars) || len(of.PreContext) != len(wf.PreContext) || of.Function != wf.Function || of.Module != wf.Module {
		t.Fatalf("frame lost detail: got %+v want %+v", of, wf)
	}
	for k, v := range wf.Vars {
		if of.Vars[k] != v {
			t.Fatalf("local %q: got %q want %q", k, of.Vars[k], v)
		}
	}
	if len(occ.Chain) != len(want.Exception.Chain) || (len(occ.Chain) > 0 && occ.Chain[0].IsCause != want.Exception.Chain[0].IsCause) {
		t.Fatalf("chain: got %+v want %+v", occ.Chain, want.Exception.Chain)
	}
	if len(occ.Breadcrumbs) != len(want.Breadcrumbs) || occ.HTTP == nil || len(occ.HTTP.Headers) != len(want.HTTP.Headers) {
		t.Fatalf("v2 detail incomplete: crumbs=%d/%d http=%+v", len(occ.Breadcrumbs), len(want.Breadcrumbs), occ.HTTP)
	}
	for k, v := range want.HTTP.Headers {
		if occ.HTTP.Headers[k] != v {
			t.Fatalf("header %q: got %q want %q", k, occ.HTTP.Headers[k], v)
		}
	}
	if occ.User == nil || occ.User.Email != want.User.Email || occ.Runtime == nil || occ.Runtime.Version != want.Runtime.Version || occ.SDK == nil || occ.SDK.Version != want.SDK.Version {
		t.Fatalf("user/runtime/sdk: %+v %+v %+v", occ.User, occ.Runtime, occ.SDK)
	}
	if occ.Mechanism == nil || occ.Mechanism.Type != want.Mechanism.Type || occ.Mechanism.IsHandled() != (want.Mechanism.Handled == nil || *want.Mechanism.Handled) {
		t.Fatalf("mechanism: got %+v want %+v", occ.Mechanism, want.Mechanism)
	}
	mid, ok, err := c.GetOccurrence(ctx, "p1", burstErrorIDs[1])
	if err != nil || !ok || mid.PrevEventID != burstErrorIDs[0] || mid.NextEventID != burstErrorIDs[2] {
		t.Fatalf("prev/next: %q %q (%v %v)", mid.PrevEventID, mid.NextEventID, ok, err)
	}
	latest, ok, _ := c.LatestOccurrence(ctx, "p1", burst.IssueID)
	if !ok || latest.EventID != burstErrorIDs[2] || latest.NextEventID != "" {
		t.Fatalf("latest: %+v", latest)
	}
	occs, err := c.QueryOccurrences(ctx, "p1", burst.IssueID, from, to, time.Time{}, 10)
	if err != nil || len(occs) != 3 {
		t.Fatalf("QueryOccurrences: %d %v", len(occs), err)
	}
	// A v0.1 occurrence (legacy tuple frames only) still renders frames.
	v01, ok, _ := c.LatestOccurrence(ctx, "p1", func() string {
		for _, is := range issues {
			if is.Service == "svc-v01" && is.ExceptionType == "RuntimeError" {
				return is.IssueID
			}
		}
		return ""
	}())
	if !ok || len(v01.Frames) == 0 || v01.Frames[len(v01.Frames)-1].Function == "" {
		t.Fatalf("v0.1 frames: %+v", v01.Frames)
	}

	// --- logs explorer ---
	logs, err := c.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to, Limit: 500})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(logs) != len(events) {
		t.Fatalf("logs %d != events %d", len(logs), len(events))
	}
	warnRows, _ := c.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to, Levels: []string{"warning"}})
	if len(warnRows) != 1 || warnRows[0].Level != "warn" {
		t.Fatalf("level alias query: %+v", warnRows)
	}
	userRows, _ := c.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to, Where: "attrs[?] = ?", Args: []any{"user_id", "u1"}})
	if len(userRows) != 6 {
		t.Fatalf("where clause on attrs: %d", len(userRows))
	}
	errorRows, _ := c.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to, IssueID: burst.IssueID})
	if len(errorRows) != 3 {
		t.Fatalf("logs by issue_id: %d", len(errorRows))
	}
	page1, _ := c.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to, Limit: 5})
	page2, _ := c.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to, Limit: 5, BeforeTS: page1[4].TS, BeforeID: page1[4].EventID})
	if len(page2) != 5 || !page2[0].TS.Before(page1[4].TS) && page2[0].EventID >= page1[4].EventID {
		t.Fatalf("keyset paging: %v / %v", page1[4].TS, page2[0].TS)
	}
	tail, _ := c.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to, After: now.Add(-time.Second), Ascending: true})
	if len(tail) != 0 {
		t.Fatalf("live tail after now must be empty: %d", len(tail))
	}

	hist, err := c.LogHistogram(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to}, "level", 0)
	if err != nil {
		t.Fatalf("histogram: %v", err)
	}
	var histSum uint64
	for _, b := range hist.Buckets {
		histSum += b.Total
	}
	if histSum != uint64(len(events)) || hist.BucketSeconds != 60 || len(hist.Series) < 3 {
		t.Fatalf("histogram sum %d bucket %d series %v", histSum, hist.BucketSeconds, hist.Series)
	}
	facets, err := c.LogFacets(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to}, nil, 10, "user_id")
	if err != nil {
		t.Fatalf("facets: %v", err)
	}
	if len(facets.Fields["service"]) < 3 || len(facets.Fields["level"]) < 3 {
		t.Fatalf("field facets: %+v", facets.Fields)
	}
	foundKey := false
	for _, k := range facets.AttrKeys {
		if k.Value == "user_id" {
			foundKey = true
		}
	}
	if !foundKey || len(facets.Values) != 1 || facets.Values[0].Value != "u1" || facets.Values[0].Count != 6 {
		t.Fatalf("attr facets: keys=%+v values=%+v", facets.AttrKeys, facets.Values)
	}
	if len(facets.TagKeys) != 1 || facets.TagKeys[0].Value != "team" {
		t.Fatalf("tag keys: %+v", facets.TagKeys)
	}
	lc, ok, err := c.LogContext(ctx, "p1", burstErrorIDs[1], 10, 10)
	if err != nil || !ok || lc.Anchor.EventID != burstErrorIDs[1] || len(lc.Before) == 0 || len(lc.After) == 0 {
		t.Fatalf("context: ok=%v err=%v before=%d after=%d", ok, err, len(lc.Before), len(lc.After))
	}
	if !lc.Before[len(lc.Before)-1].TS.Before(lc.Anchor.TS) || !lc.After[0].TS.After(lc.Anchor.TS) {
		t.Fatalf("context ordering wrong")
	}
	pats, err := c.LogPatterns(ctx, clickhouse.LogQuery{ProjectID: "p1", From: from, To: to, Service: "web"}, 10)
	if err != nil {
		t.Fatalf("patterns: %v", err)
	}
	found := false
	for _, pt := range pats {
		if pt.Pattern == "handled order <n>" && pt.Count == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("pattern clustering: %+v", pats)
	}

	// --- metrics ---
	ov, err := c.Overview(ctx, "p1", "", "", from, to)
	if err != nil || ov.Events != uint64(len(events)) || ov.Errors != totalOcc || ov.OpenIssues != uint64(len(issues)) || ov.Services < 3 {
		t.Fatalf("overview: %+v %v (events=%d occ=%d issues=%d)", ov, err, len(events), totalOcc, len(issues))
	}
	top, err := c.TopIssues(ctx, "p1", "", "", from, to, 2)
	if err != nil || len(top) != 2 || top[0].IssueID != burst.IssueID || top[0].OccurrenceCount != 3 {
		t.Fatalf("top issues: %+v %v", top, err)
	}
	vol, err := c.LogVolumeByLevel(ctx, "p1", "", "", from, to, 60)
	if err != nil {
		t.Fatalf("volume: %v", err)
	}
	var volSum uint64
	for _, b := range vol.Buckets {
		volSum += b.Total
	}
	if volSum != uint64(len(events)) {
		t.Fatalf("volume sum %d", volSum)
	}
	et, _ := c.ErrorTrend(ctx, "p1", "", "", "", "", from, to, 60)
	var etSum uint64
	for _, pt := range et {
		etSum += pt.Count
	}
	if etSum != totalOcc {
		t.Fatalf("error trend sum %d", etSum)
	}

	// --- lifecycle: resolve, regress, ignore ---
	if err := c.SetIssueState(ctx, api.IssueStateChange{ProjectID: "p1", IssueID: burst.IssueID, Status: issue.StatusResolved, UpdatedBy: "tester"}); err != nil {
		t.Fatalf("set state: %v", err)
	}
	p.States().Set("p1", burst.IssueID, issue.State{Status: issue.StatusResolved})
	if got, _ := c.QueryIssues(ctx, clickhouse.IssueListQuery{ProjectID: "p1", Status: "resolved"}); len(got) != 1 || got[0].IssueID != burst.IssueID {
		t.Fatalf("resolved filter: %+v", got)
	}
	again := events[len(events)-2] // the last burst error
	again.EventID = "burst-4"
	again.Timestamp = model.At(now)
	p.Process(ctx, tn, again, 500)
	_ = c.Flush(ctx)
	status, regressed, found, _ := c.GetIssueStatus(ctx, "p1", burst.IssueID)
	if !found || status != issue.StatusOpen || !regressed {
		t.Fatalf("regression not recorded: %s %v %v", status, regressed, found)
	}
	if got, _ := c.QueryIssues(ctx, clickhouse.IssueListQuery{ProjectID: "p1", Status: "regressed"}); len(got) != 1 || !got[0].IsRegressed {
		t.Fatalf("regressed filter: %+v", got)
	}
	counts, _ = c.CountIssuesByStatus(ctx, clickhouse.IssueListQuery{ProjectID: "p1"})
	if counts["regressed"] != 1 || counts["resolved"] != 0 {
		t.Fatalf("counts after regression: %v", counts)
	}
}
