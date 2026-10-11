package ingest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
	"pyloggle/internal/issue"
	"pyloggle/internal/model"
	"pyloggle/internal/tenant"
)

type memSink struct {
	mu    sync.Mutex
	logs  []model.Event
	occs  []issue.ErrorOccurrence
	spans []model.Span
}

func (m *memSink) EnqueueLog(_ context.Context, ev model.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, ev)
	return nil
}

func (m *memSink) EnqueueErrorOccurrence(_ context.Context, occ issue.ErrorOccurrence) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.occs = append(m.occs, occ)
	return nil
}
func (m *memSink) EnqueueSpans(_ context.Context, spans []model.Span) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spans = append(m.spans, spans...)
	return nil
}

type memStates struct {
	mu       sync.Mutex
	states   map[string]issue.State
	changes  []api.IssueStateChange
	releases map[string]time.Time
}

func (s *memStates) GetIssueState(_ context.Context, projectID, issueID string) (issue.State, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[projectID+"|"+issueID]
	return st, ok, nil
}

// SetIssueState mirrors the storage semantics: overlay the change on the current state.
func (s *memStates) SetIssueState(_ context.Context, ch api.IssueStateChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		s.states = map[string]issue.State{}
	}
	key := ch.ProjectID + "|" + ch.IssueID
	next := s.states[key]
	if ch.Status != "" {
		if ch.Status != next.Status {
			next.SnoozeUntil, next.SnoozeCount, next.ResolvedInRelease = time.Time{}, 0, ""
		}
		next.Status, next.Regressed, next.RegressedFrom = ch.Status, ch.Regressed, ch.RegressedFrom
	}
	if ch.Assignee != nil {
		next.Assignee = *ch.Assignee
	}
	if ch.SnoozeUntil != nil {
		next.SnoozeUntil = *ch.SnoozeUntil
	}
	if ch.SnoozeCount != nil {
		next.SnoozeCount = *ch.SnoozeCount
	}
	if ch.ResolvedInRelease != nil {
		next.ResolvedInRelease = *ch.ResolvedInRelease
	}
	s.states[key] = next
	s.changes = append(s.changes, ch)
	return nil
}

func (s *memStates) ReleaseFirstSeen(_ context.Context, projectID, release string) (time.Time, bool, error) {
	t, ok := s.releases[projectID+"|"+release]
	return t, ok, nil
}

type memResolver struct{ keys map[string]tenant.Project }

func (r memResolver) ResolveByAPIKey(_ context.Context, key string) (tenant.Project, bool, error) {
	p, ok := r.keys[key]
	return p, ok, nil
}

func testConfig() config.Config {
	cfg := config.FromEnv()
	cfg.DefaultProjID = "default"
	cfg.SampleInfo, cfg.SampleDebug, cfg.SampleWarn = 1, 1, 1
	cfg.RateLimitRPS, cfg.RateLimitBurst = 0, 0
	return cfg
}

func fixtureLines(t *testing.T, path string) [][]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if l := bytes.TrimSpace(sc.Bytes()); len(l) > 0 {
			out = append(out, append([]byte(nil), l...))
		}
	}
	return out
}

func TestHandlerAcceptsV01Fixture(t *testing.T) {
	sink := &memSink{}
	p := NewPipeline(testConfig(), sink, &memStates{}, memResolver{keys: map[string]tenant.Project{"k1": {ID: "proj1"}}})
	h := NewHandlerFromPipeline(p)

	lines := fixtureLines(t, "../model/testdata/sdk_v0.1.ndjson")
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(bytes.Join(lines, []byte("\n"))))
	req.Header.Set("X-API-Key", "k1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Accepted int      `json:"accepted"`
		Rejected int      `json:"rejected"`
		Errors   []string `json:"errors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Rejected != 0 || resp.Accepted != len(lines) {
		t.Fatalf("every v0.1 line must be accepted: %+v", resp)
	}
	if len(sink.logs) != len(lines) {
		t.Fatalf("logs stored: %d want %d", len(sink.logs), len(lines))
	}
	var errLines int
	for _, l := range sink.logs {
		if l.ProjectID != "proj1" {
			t.Fatalf("project must come from the key: %q", l.ProjectID)
		}
		if l.EventID == "" || l.Timestamp.IsZero() {
			t.Fatalf("event_id/ts must be filled")
		}
		if l.Level == "warning" || l.Level == "critical" {
			t.Fatalf("levels must be canonical: %q", l.Level)
		}
		if l.IsError() {
			errLines++
			if l.Attrs.Get("pyloggle.issue_id") == "" {
				t.Fatalf("error log rows carry their issue id")
			}
		}
	}
	// critical line + logger.exception + flask boom + asgi boom = 4 occurrences
	if len(sink.occs) != 4 || errLines != 4 {
		t.Fatalf("occurrences %d, error log lines %d (want 4/4)", len(sink.occs), errLines)
	}
	for _, o := range sink.occs {
		if o.IssueID == "" || o.Title == "" || o.FingerprintVersion != 2 {
			t.Fatalf("bad occurrence: %+v", o)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	cfg := testConfig()
	p := NewPipeline(cfg, &memSink{}, nil, memResolver{keys: map[string]tenant.Project{"good": {ID: "p1", MaxEventsPerSec: 10}}})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	tn, err := p.Authenticate(context.Background(), req)
	if err != nil || tn.ProjectID != "default" {
		t.Fatalf("no key -> default project: %+v %v", tn, err)
	}
	req.Header.Set("Authorization", "Bearer good")
	tn, err = p.Authenticate(context.Background(), req)
	if err != nil || tn.ProjectID != "p1" || tn.RPS != 10 || tn.Burst < 10 {
		t.Fatalf("bearer: %+v %v", tn, err)
	}
	req.Header.Del("Authorization")
	req.Header.Set("X-API-Key", "bad")
	if _, err := p.Authenticate(context.Background(), req); err != ErrUnauthorized {
		t.Fatalf("unknown key must be unauthorized, got %v", err)
	}
	cfg.DefaultProjID = ""
	p = NewPipeline(cfg, &memSink{}, nil, memResolver{})
	req.Header.Del("X-API-Key")
	if _, err := p.Authenticate(context.Background(), req); err != ErrUnauthorized {
		t.Fatalf("no key and no default project must be unauthorized")
	}
}

func TestSkipIssueAndRegression(t *testing.T) {
	sink := &memSink{}
	states := &memStates{}
	p := NewPipeline(testConfig(), sink, states, nil)
	tn := Tenant{ProjectID: "p"}
	ev := model.Event{Service: "api", Env: "prod", Level: "error", Logger: "app", Message: "boom",
		Exception: &model.Exception{Type: "ValueError", Message: "boom", Frames: []model.Frame{{Filename: "/srv/app/x.py", Function: "f", Lineno: 1}}}}

	if o := p.Process(context.Background(), tn, ev, 100); o != Accepted || len(sink.occs) != 1 {
		t.Fatalf("first occurrence: %v occs=%d", o, len(sink.occs))
	}
	issueID := sink.occs[0].IssueID

	dup := ev
	dup.SkipIssue = true
	if o := p.Process(context.Background(), tn, dup, 100); o != Accepted || len(sink.occs) != 1 || len(sink.logs) != 2 {
		t.Fatalf("skip_issue stores the log but no occurrence: %v occs=%d logs=%d", o, len(sink.occs), len(sink.logs))
	}

	// Resolve, then a new occurrence must reopen as regressed.
	_ = states.SetIssueState(context.Background(), api.IssueStateChange{ProjectID: "p", IssueID: issueID, Status: issue.StatusResolved})
	p.States().Set("p", issueID, issue.State{Status: issue.StatusResolved})
	p.Process(context.Background(), tn, ev, 100)
	st := states.states["p|"+issueID]
	if st.Status != issue.StatusOpen || !st.Regressed || st.RegressedFrom != issue.StatusResolved {
		t.Fatalf("expected regression, got %+v", st)
	}
	// Ignored stays ignored.
	_ = states.SetIssueState(context.Background(), api.IssueStateChange{ProjectID: "p", IssueID: issueID, Status: issue.StatusIgnored})
	p.States().Set("p", issueID, issue.State{Status: issue.StatusIgnored})
	p.Process(context.Background(), tn, ev, 100)
	if states.states["p|"+issueID].Status != issue.StatusIgnored {
		t.Fatalf("ignored issues must not be reopened")
	}
}

func TestSamplingKeepsRequestsTogether(t *testing.T) {
	cfg := testConfig()
	cfg.SampleInfo = 0.5
	cfg.SampleByRequest = true
	sink := &memSink{}
	p := NewPipeline(cfg, sink, nil, nil)
	tn := Tenant{ProjectID: "p"}
	kept := map[string]int{}
	for _, rid := range []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8"} {
		for i, msg := range []string{"start", "query", "done"} {
			ev := model.Event{Service: "api", Level: "info", Logger: "app", Message: msg, RequestID: rid}
			if i == 2 {
				ev.Message = "done " + rid
			}
			if p.Process(context.Background(), tn, ev, 10) == Accepted {
				kept[rid]++
			}
		}
	}
	for rid, n := range kept {
		if n != 3 {
			t.Fatalf("request %s partially sampled: %d lines", rid, n)
		}
	}
	if len(kept) == 0 || len(kept) == 8 {
		t.Fatalf("expected roughly half of the requests kept, got %d", len(kept))
	}
	// Errors are never sampled.
	ev := model.Event{Service: "api", Level: "error", Message: "x", RequestID: "zzz"}
	if p.Process(context.Background(), tn, ev, 10) != Accepted {
		t.Fatalf("errors are exempt from sampling")
	}
}

func TestRejectedLinesAreReported(t *testing.T) {
	p := NewPipeline(testConfig(), &memSink{}, nil, nil)
	h := NewHandlerFromPipeline(p)
	body := strings.Join([]string{`{"message":"ok","level":"info"}`, `{not json`, `{"ts":"nonsense","message":"x"}`}, "\n")
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["accepted"].(float64) != 1 || resp["rejected"].(float64) != 2 {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if errs, _ := resp["errors"].([]any); len(errs) != 2 {
		t.Fatalf("errors must be listed: %s", rec.Body.String())
	}
}

func TestLatencyAttrIsPromotedToDuration(t *testing.T) {
	sink := &memSink{}
	p := NewPipeline(testConfig(), sink, nil, nil)
	tn := Tenant{ProjectID: "p"}
	ev := model.Event{Service: "api", Env: "prod", Level: "info", Message: "request", Attrs: model.Attrs{"latency_ms": "42"}}
	if o := p.Process(context.Background(), tn, ev, 50); o != Accepted || len(sink.logs) != 1 {
		t.Fatalf("outcome %v logs=%d", o, len(sink.logs))
	}
	if sink.logs[0].DurationMS != 42 {
		t.Fatalf("duration_ms not promoted: %v", sink.logs[0].DurationMS)
	}
	// An explicit duration wins over the attribute.
	ev.DurationMS = 7
	_ = p.Process(context.Background(), tn, ev, 50)
	if sink.logs[1].DurationMS != 7 {
		t.Fatalf("explicit duration overwritten: %v", sink.logs[1].DurationMS)
	}
}

func TestSnoozeAndReleaseRules(t *testing.T) {
	sink := &memSink{}
	states := &memStates{states: map[string]issue.State{}, releases: map[string]time.Time{"p|build-1": time.Unix(1000, 0), "p|build-2": time.Unix(2000, 0)}}
	p := NewPipeline(testConfig(), sink, states, nil)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	tn := Tenant{ProjectID: "p"}
	ev := model.Event{Service: "api", Env: "prod", Level: "error", Logger: "app", Message: "boom",
		Exception: &model.Exception{Type: "ValueError", Message: "boom", Frames: []model.Frame{{Filename: "/srv/app/x.py", Function: "f", Lineno: 1}}}}
	_ = p.Process(context.Background(), tn, ev, 100)
	issueID := sink.occs[0].IssueID
	key := "p|" + issueID
	set := func(st issue.State) {
		states.mu.Lock()
		states.states[key] = st
		states.mu.Unlock()
		p.states.Set("p", issueID, st)
	}
	get := func() issue.State {
		states.mu.Lock()
		defer states.mu.Unlock()
		return states.states[key]
	}

	// Resolved in release 1.9.0: an occurrence from 1.9.0 is not a regression, 1.10.0 is.
	set(issue.State{Status: issue.StatusResolved, ResolvedInRelease: "1.9.0", Assignee: "alice"})
	ev.Release = "1.9.0"
	_ = p.Process(context.Background(), tn, ev, 100)
	if st := get(); st.Status != issue.StatusResolved {
		t.Fatalf("same release must not regress: %+v", st)
	}
	ev.Release = ""
	_ = p.Process(context.Background(), tn, ev, 100)
	if st := get(); st.Status != issue.StatusResolved {
		t.Fatalf("occurrence without release must not regress a release-resolved issue: %+v", st)
	}
	ev.Release = "1.10.0"
	_ = p.Process(context.Background(), tn, ev, 100)
	if st := get(); st.Status != issue.StatusOpen || !st.Regressed || st.Assignee != "alice" || st.ResolvedInRelease != "" {
		t.Fatalf("newer release must regress and keep the assignee: %+v", st)
	}
	last := states.changes[len(states.changes)-1]
	if last.Kind != "regression" || last.Release != "1.10.0" || last.UpdatedBy != "system" {
		t.Fatalf("regression change: %+v", last)
	}

	// Opaque releases are ordered by first-seen.
	set(issue.State{Status: issue.StatusResolved, ResolvedInRelease: "build-2"})
	ev.Release = "build-1"
	_ = p.Process(context.Background(), tn, ev, 100)
	if st := get(); st.Status != issue.StatusResolved {
		t.Fatalf("older build must not regress: %+v", st)
	}
	set(issue.State{Status: issue.StatusResolved, ResolvedInRelease: "build-1"})
	ev.Release = "build-2"
	_ = p.Process(context.Background(), tn, ev, 100)
	if st := get(); st.Status != issue.StatusOpen {
		t.Fatalf("newer build must regress: %+v", st)
	}

	// Snooze until a time: stays ignored before, reopens after.
	set(issue.State{Status: issue.StatusIgnored, SnoozeUntil: now.Add(time.Hour)})
	_ = p.Process(context.Background(), tn, ev, 100)
	if st := get(); st.Status != issue.StatusIgnored {
		t.Fatalf("snooze still active: %+v", st)
	}
	p.now = func() time.Time { return now.Add(2 * time.Hour) }
	_ = p.Process(context.Background(), tn, ev, 100)
	if st := get(); st.Status != issue.StatusOpen || st.RegressedFrom != issue.StatusIgnored {
		t.Fatalf("expired snooze must reopen: %+v", st)
	}
	if last := states.changes[len(states.changes)-1]; last.Kind != "unsnooze" {
		t.Fatalf("unsnooze activity: %+v", last)
	}

	// Snooze for 3 more occurrences (absolute target 13 with 10 already counted).
	set(issue.State{Status: issue.StatusIgnored, SnoozeCount: 13, OccurrenceCount: 10})
	for i := 0; i < 2; i++ {
		_ = p.Process(context.Background(), tn, ev, 100)
	}
	if st := get(); st.Status != issue.StatusIgnored {
		t.Fatalf("budget not yet spent: %+v", st)
	}
	_ = p.Process(context.Background(), tn, ev, 100)
	if st := get(); st.Status != issue.StatusOpen {
		t.Fatalf("budget spent must reopen: %+v", st)
	}

	// A plain ignore stays ignored forever.
	set(issue.State{Status: issue.StatusIgnored})
	for i := 0; i < 5; i++ {
		_ = p.Process(context.Background(), tn, ev, 100)
	}
	if st := get(); st.Status != issue.StatusIgnored {
		t.Fatalf("plain ignore must stay ignored: %+v", st)
	}
}
