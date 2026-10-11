package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
	"pyloggle/internal/issue"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/tenant"
)

// fakeStore records the queries it receives and returns canned data.
type fakeStore struct {
	lastLogQuery   clickhouse.LogQuery
	lastIssueQuery clickhouse.IssueListQuery
	states         []api.IssueStateChange
	projects       []clickhouse.ProjectView
	created        []clickhouse.ProjectUpsert
}

func (f *fakeStore) QueryIssues(_ context.Context, q clickhouse.IssueListQuery) ([]api.IssueSummary, error) {
	f.lastIssueQuery = q
	return []api.IssueSummary{{IssueID: "i1", Title: "ValueError: boom", Status: "open", Service: "api", Env: "prod"}}, nil
}
func (f *fakeStore) CountIssuesByStatus(context.Context, clickhouse.IssueListQuery) (map[string]uint64, error) {
	return map[string]uint64{"open": 1, "all": 1}, nil
}
func (f *fakeStore) GetIssueSummary(_ context.Context, _, id string) (api.IssueSummary, bool, error) {
	if id == "missing" {
		return api.IssueSummary{}, false, nil
	}
	return api.IssueSummary{IssueID: id, Service: "api", Env: "prod", Status: "open"}, true, nil
}
func (f *fakeStore) IssueSparklines(_ context.Context, _ string, ids []string, hours int) (map[string][]uint64, error) {
	out := map[string][]uint64{}
	for _, id := range ids {
		s := make([]uint64, hours)
		s[hours-1] = 7
		out[id] = s
	}
	return out, nil
}
func (f *fakeStore) IssueTrend(context.Context, string, string, time.Time, time.Time, int) ([]api.TimeSeriesPoint, error) {
	return []api.TimeSeriesPoint{{Count: 1}}, nil
}
func (f *fakeStore) IssueBreakdown(_ context.Context, _, _, field string, _, _ time.Time, _ int) ([]api.FacetCount, error) {
	if field == "env" {
		return []api.FacetCount{{Value: "prod", Count: 3}}, nil
	}
	return nil, nil
}
func (f *fakeStore) GetOccurrence(_ context.Context, _, id string) (api.ErrorOccurrence, bool, error) {
	return api.ErrorOccurrence{EventID: id}, id != "nope", nil
}
func (f *fakeStore) LatestOccurrence(context.Context, string, string) (api.ErrorOccurrence, bool, error) {
	return api.ErrorOccurrence{EventID: "latest"}, true, nil
}
func (f *fakeStore) QueryOccurrences(context.Context, string, string, time.Time, time.Time, time.Time, int) ([]api.ErrorOccurrence, error) {
	return []api.ErrorOccurrence{{EventID: "e1"}}, nil
}
func (f *fakeStore) SetIssueState(_ context.Context, st api.IssueStateChange) error {
	f.states = append(f.states, st)
	return nil
}
func (f *fakeStore) QueryLogs(_ context.Context, q clickhouse.LogQuery) ([]api.LogEvent, error) {
	f.lastLogQuery = q
	out := make([]api.LogEvent, 0, q.Limit)
	for i := 0; i < q.Limit && i < 3; i++ {
		out = append(out, api.LogEvent{EventID: "ev" + string(rune('a'+i)), TS: time.Unix(1700000000-int64(i), 0).UTC(), Message: "m"})
	}
	return out, nil
}
func (f *fakeStore) LogHistogram(_ context.Context, q clickhouse.LogQuery, groupBy string, bucket time.Duration) (api.Histogram, error) {
	f.lastLogQuery = q
	return api.Histogram{BucketSeconds: int(bucket / time.Second), GroupBy: groupBy, Series: []string{"info"}}, nil
}
func (f *fakeStore) LogFacets(_ context.Context, q clickhouse.LogQuery, _ []string, _ int, key string) (api.LogFacets, error) {
	f.lastLogQuery = q
	lf := api.LogFacets{Fields: map[string][]api.FacetCount{"level": {{Value: "info", Count: 2}}}}
	if key != "" {
		lf.Values = []api.FacetCount{{Value: "u1", Count: 1}}
	}
	return lf, nil
}
func (f *fakeStore) LogContext(_ context.Context, _, id string, _, _ int) (api.LogContext, bool, error) {
	return api.LogContext{Anchor: api.LogEvent{EventID: id}}, id != "nope", nil
}
func (f *fakeStore) LogPatterns(context.Context, clickhouse.LogQuery, int) ([]api.LogPattern, error) {
	return []api.LogPattern{{Pattern: "x <n>", Count: 2}}, nil
}
func (f *fakeStore) Overview(context.Context, string, string, string, time.Time, time.Time) (api.Overview, error) {
	return api.Overview{Events: 10}, nil
}
func (f *fakeStore) ErrorTrend(context.Context, string, string, string, string, string, time.Time, time.Time, int) ([]api.TimeSeriesPoint, error) {
	return nil, nil
}
func (f *fakeStore) LogVolumeByLevel(context.Context, string, string, string, time.Time, time.Time, int) (api.Histogram, error) {
	return api.Histogram{}, nil
}
func (f *fakeStore) TopIssues(context.Context, string, string, string, time.Time, time.Time, int) ([]api.IssueSummary, error) {
	return nil, nil
}
func (f *fakeStore) ListSavedSearches(context.Context, string, string, bool, bool, int) ([]api.SavedSearch, error) {
	return nil, nil
}
func (f *fakeStore) UpsertSavedSearch(_ context.Context, s api.SavedSearch) (api.SavedSearch, error) {
	s.ID = "sv1"
	return s, nil
}
func (f *fakeStore) SoftDeleteSavedSearch(context.Context, string, string, string) error { return nil }
func (f *fakeStore) ListProjects(context.Context, int) ([]clickhouse.ProjectView, error) {
	return f.projects, nil
}
func (f *fakeStore) GetProjectByID(_ context.Context, id string) (clickhouse.ProjectView, bool, error) {
	for _, p := range f.projects {
		if p.ID == id {
			return p, true, nil
		}
	}
	return clickhouse.ProjectView{}, false, nil
}
func (f *fakeStore) CreateProject(_ context.Context, p clickhouse.ProjectUpsert) (clickhouse.ProjectView, string, error) {
	f.created = append(f.created, p)
	return clickhouse.ProjectView{ID: p.ID, Name: p.Name}, "secret-key", nil
}
func (f *fakeStore) UpsertProject(_ context.Context, p clickhouse.ProjectUpsert) (clickhouse.ProjectView, error) {
	return clickhouse.ProjectView{ID: p.ID, Name: p.Name}, nil
}
func (f *fakeStore) RotateProjectAPIKey(_ context.Context, id string) (clickhouse.ProjectView, string, error) {
	return clickhouse.ProjectView{ID: id}, "new-key", nil
}
func (f *fakeStore) ListAlertRules(context.Context, string) ([]api.AlertRule, error) { return nil, nil }
func (f *fakeStore) UpsertAlertRule(_ context.Context, r api.AlertRule) (api.AlertRule, error) {
	r.ID = "r1"
	return r, nil
}
func (f *fakeStore) ListAlertFirings(context.Context, string, int) ([]api.AlertFiring, error) {
	return nil, nil
}

type fakeKeys struct{}

func (fakeKeys) ResolveByAPIKey(_ context.Context, key string) (tenant.Project, bool, error) {
	if key == "k-good" {
		return tenant.Project{ID: "p2", Name: "Second"}, true, nil
	}
	return tenant.Project{}, false, nil
}

func newTestUI(mode string) (*UI, *fakeStore, *issue.StateCache) {
	cfg := config.FromEnv()
	cfg.UIAuthMode = mode
	cfg.DefaultProjID = "default"
	store := &fakeStore{projects: []clickhouse.ProjectView{{ID: "default", Name: "Default"}, {ID: "p2", Name: "Second"}}}
	states := issue.NewStateCache(time.Minute, nil)
	return NewForTest(cfg, store, fakeKeys{}, states), store, states
}

func do(t *testing.T, u *UI, method, path string, body any, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestMeAndDefaultProject(t *testing.T) {
	u, _, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/me", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	if out["project"].(map[string]any)["id"] != "default" || len(out["projects"].([]any)) != 2 || out["auth_mode"] != "disabled" {
		t.Fatalf("me: %s", rec.Body.String())
	}
}

func TestLogsQueryLanguageIsApplied(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/logs?q=service:api+level:error+%40user_id:42&from=-1h&limit=2", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("logs: %d %s", rec.Code, rec.Body.String())
	}
	q := store.lastLogQuery
	if !strings.Contains(q.Where, "service = ?") || !strings.Contains(q.Where, "has(?, level)") || !strings.Contains(q.Where, "attrs[?] = ?") {
		t.Fatalf("where not rendered: %q", q.Where)
	}
	if q.ProjectID != "default" || q.Limit != 2 || q.From.IsZero() {
		t.Fatalf("query: %+v", q)
	}
	if out["next_cursor"] == nil {
		t.Fatalf("a full page must carry next_cursor: %s", rec.Body.String())
	}
	cur := out["next_cursor"].(string)
	rec, _ = do(t, u, "GET", "/ui/api/logs?cursor="+cur, nil, nil)
	if rec.Code != 200 || store.lastLogQuery.BeforeID == "" || store.lastLogQuery.BeforeTS.IsZero() {
		t.Fatalf("cursor not applied: %+v", store.lastLogQuery)
	}
	rec, out = do(t, u, "GET", "/ui/api/logs?q=level:%3Eerror", nil, nil)
	if rec.Code != 400 || out["error"] != "bad_query" || out["pos"] == nil {
		t.Fatalf("bad query must 400 with a position: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "GET", "/ui/api/logs?after=2025-01-01T00:00:00Z", nil, nil)
	if rec.Code != 200 || !store.lastLogQuery.Ascending || store.lastLogQuery.After.IsZero() {
		t.Fatalf("live tail: %+v", store.lastLogQuery)
	}
	rec, out = do(t, u, "GET", "/ui/api/logs/histogram?q=env:prod&from=-6h&group_by=service", nil, nil)
	if rec.Code != 200 || out["group_by"] != "service" || out["bucket_seconds"].(float64) != 300 {
		t.Fatalf("histogram: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/logs/facets?key=user_id", nil, nil)
	if rec.Code != 200 || len(out["values"].([]any)) != 1 {
		t.Fatalf("facets: %s", rec.Body.String())
	}
	rec, _ = do(t, u, "GET", "/ui/api/logs/context?event_id=nope", nil, nil)
	if rec.Code != 404 {
		t.Fatalf("context 404: %d", rec.Code)
	}
	rec, out = do(t, u, "GET", "/ui/api/logs/fields", nil, nil)
	if rec.Code != 200 || len(out["fields"].([]any)) < 10 {
		t.Fatalf("fields: %s", rec.Body.String())
	}
}

func TestIssuesAndState(t *testing.T) {
	u, store, states := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/issues?status=open&sort=count&sparklines=1&q=boom", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("issues: %d %s", rec.Code, rec.Body.String())
	}
	if store.lastIssueQuery.Status != "open" || store.lastIssueQuery.Sort != "count" || store.lastIssueQuery.Q != "boom" {
		t.Fatalf("issue query: %+v", store.lastIssueQuery)
	}
	items := out["items"].([]any)
	if sp := items[0].(map[string]any)["sparkline"].([]any); len(sp) != 24 || sp[23].(float64) != 7 {
		t.Fatalf("sparkline: %v", sp)
	}
	rec, out = do(t, u, "GET", "/ui/api/issues/i1", nil, nil)
	if rec.Code != 200 || out["latest"] == nil || out["breakdowns"].(map[string]any)["env"] == nil {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "GET", "/ui/api/issues/missing", nil, nil)
	if rec.Code != 404 {
		t.Fatalf("missing issue: %d", rec.Code)
	}
	// CSRF: mutation without the header is refused.
	rec, _ = do(t, u, "POST", "/ui/api/issues/i1/state", map[string]any{"status": "resolved"}, nil)
	if rec.Code != 403 {
		t.Fatalf("csrf: %d", rec.Code)
	}
	rec, _ = do(t, u, "POST", "/ui/api/issues/i1/state", map[string]any{"status": "resolved", "comment": "fixed"}, map[string]string{csrfHeader: "1"})
	if rec.Code != 200 || len(store.states) != 1 || store.states[0].Status != "resolved" || store.states[0].Service != "api" {
		t.Fatalf("state: %d %s %+v", rec.Code, rec.Body.String(), store.states)
	}
	if st, known, _ := states.Get(context.Background(), "default", "i1"); !known || st.Status != "resolved" {
		t.Fatalf("ingest cache must learn the new state: %+v %v", st, known)
	}
	rec, _ = do(t, u, "POST", "/ui/api/issues/i1/state", map[string]any{"status": "bogus"}, map[string]string{csrfHeader: "1"})
	if rec.Code != 400 {
		t.Fatalf("bad status: %d", rec.Code)
	}
	rec, out = do(t, u, "POST", "/ui/api/issues/state", map[string]any{"issue_ids": []string{"a", "b"}, "status": "ignored"}, map[string]string{csrfHeader: "1", "Sec-Fetch-Site": "same-origin"})
	if rec.Code != 200 || out["updated"].(float64) != 2 {
		t.Fatalf("bulk: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "POST", "/ui/api/issues/state", map[string]any{"issue_ids": []string{"a"}, "status": "ignored"}, map[string]string{csrfHeader: "1", "Sec-Fetch-Site": "cross-site"})
	if rec.Code != 403 {
		t.Fatalf("cross-site must be refused: %d", rec.Code)
	}
}

func TestProjectSelectionSources(t *testing.T) {
	u, _, _ := newTestUI("disabled")
	// explicit query param wins and is remembered via cookie
	rec, out := do(t, u, "GET", "/ui/api/me?project_id=p2", nil, nil)
	if rec.Code != 200 || out["project"].(map[string]any)["id"] != "p2" || !strings.Contains(rec.Header().Get("Set-Cookie"), uiProjectIDCookie) {
		t.Fatalf("project param: %d %s %v", rec.Code, rec.Body.String(), rec.Header())
	}
	// API key header resolves a project
	rec, out = do(t, u, "GET", "/ui/api/me", nil, map[string]string{"X-API-Key": "k-good"})
	if rec.Code != 200 || out["project"].(map[string]any)["id"] != "p2" {
		t.Fatalf("api key: %s", rec.Body.String())
	}
	// unknown project id -> 400
	rec, _ = do(t, u, "GET", "/ui/api/issues?project_id=zzz", nil, nil)
	if rec.Code != 400 {
		t.Fatalf("unknown project: %d", rec.Code)
	}
	// login with api key sets cookies
	rec, _ = do(t, u, "POST", "/ui/api/login", map[string]any{"api_key": "k-good"}, map[string]string{csrfHeader: "1"})
	if rec.Code != 200 || len(rec.Header().Values("Set-Cookie")) < 2 {
		t.Fatalf("login: %d %v", rec.Code, rec.Header())
	}
	rec, _ = do(t, u, "POST", "/ui/api/login", map[string]any{"api_key": "bad"}, map[string]string{csrfHeader: "1"})
	if rec.Code != 401 {
		t.Fatalf("bad login: %d", rec.Code)
	}
}

func TestAzureModeRequiresSession(t *testing.T) {
	u, store, _ := newTestUI("azure")
	rec, out := do(t, u, "GET", "/ui/api/me", nil, nil)
	if rec.Code != 401 || out["login_url"] != "/ui/login" {
		t.Fatalf("azure without session: %d %s", rec.Code, rec.Body.String())
	}
	// Admin allowlist: no session -> forbidden even in disabled mode? No: disabled mode is open.
	u2, store2, _ := newTestUI("disabled")
	u2.cfg.UIAdmins = []string{"admin@example.com"}
	rec, out = do(t, u2, "POST", "/ui/api/projects", map[string]any{"id": "newp", "name": "New"}, map[string]string{csrfHeader: "1"})
	if rec.Code != 201 || out["api_key"] != "secret-key" || len(store2.created) != 1 {
		t.Fatalf("create project (disabled mode ignores allowlist): %d %s", rec.Code, rec.Body.String())
	}
	_ = store
}

func TestAPIKeyMirror(t *testing.T) {
	u, store, _ := newTestUI("azure")
	h := u.APIKeyAPIHandler()
	req := httptest.NewRequest("GET", "/v1/q/logs?q=service:api", nil)
	req.Header.Set("X-API-Key", "k-good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || store.lastLogQuery.ProjectID != "p2" {
		t.Fatalf("mirror with key: %d %s (%+v)", rec.Code, rec.Body.String(), store.lastLogQuery)
	}
	req = httptest.NewRequest("GET", "/v1/q/logs?q=service:api", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("unknown bearer key: %d", rec.Code)
	}
	// Without a key the default project applies to reads.
	req = httptest.NewRequest("GET", "/v1/q/logs?q=service:api", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || store.lastLogQuery.ProjectID != "default" {
		t.Fatalf("mirror: %d %s (%+v)", rec.Code, rec.Body.String(), store.lastLogQuery)
	}
	req = httptest.NewRequest("POST", "/v1/q/issues/i1/state", strings.NewReader(`{"status":"resolved"}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("mutations without an explicit key must be refused: %d", rec.Code)
	}
}
