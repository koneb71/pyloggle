package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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
	lastLogQuery    clickhouse.LogQuery
	lastAggregate   clickhouse.AggregateSpec
	lastSpanQuery   clickhouse.SpanQuery
	lastMetricQuery clickhouse.MetricQuery
	mu              sync.Mutex
	deleted         []string
	purged          []string
	audit           []api.AuditEntry
	lastTraceAt     time.Time
	lastService     string
	logQueries      []clickhouse.LogQuery
	exportRows      int // rows the fake export source holds
	lastIssueQuery  clickhouse.IssueListQuery
	states          []api.IssueStateChange
	comments        []string
	projects        []clickhouse.ProjectView
	created         []clickhouse.ProjectUpsert
	keys            []api.APIKey
	revoked         []string
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
func (f *fakeStore) GetIssueState(_ context.Context, _, id string) (issue.State, bool, error) {
	if id == "missing" {
		return issue.State{}, false, nil
	}
	st := issue.State{Status: "open"}
	for _, c := range f.states {
		if c.IssueID != id {
			continue
		}
		if c.Status != "" {
			st.Status, st.Regressed = c.Status, c.Regressed
		}
		if c.Assignee != nil {
			st.Assignee = *c.Assignee
		}
		if c.SnoozeUntil != nil {
			st.SnoozeUntil = *c.SnoozeUntil
		}
		if c.SnoozeCount != nil {
			st.SnoozeCount = *c.SnoozeCount
		}
		if c.ResolvedInRelease != nil {
			st.ResolvedInRelease = *c.ResolvedInRelease
		}
	}
	return st, true, nil
}
func (f *fakeStore) AddIssueComment(_ context.Context, _, id, actor, body string) error {
	f.comments = append(f.comments, actor+": "+body)
	return nil
}
func (f *fakeStore) ListIssueActivity(_ context.Context, _, id string, _ int) ([]api.IssueActivity, error) {
	return []api.IssueActivity{{ID: "a1", IssueID: id, Kind: "status", Actor: "alice", From: "open", To: "resolved", TS: time.Unix(1700000000, 0).UTC()}}, nil
}
func (f *fakeStore) ListAssignees(context.Context, string) ([]string, error) {
	return []string{"alice", "bob"}, nil
}
func (f *fakeStore) QueryLogs(_ context.Context, q clickhouse.LogQuery) ([]api.LogEvent, error) {
	f.lastLogQuery = q
	f.logQueries = append(f.logQueries, q)
	total := 3
	if f.exportRows > 0 {
		total = f.exportRows
	}
	// Rows are numbered newest first; a keyset cursor resumes after the given event id.
	start := 0
	if q.BeforeID != "" {
		n, _ := strconv.Atoi(strings.TrimPrefix(q.BeforeID, "ev"))
		start = n + 1
	}
	out := make([]api.LogEvent, 0, q.Limit)
	for i := start; i < total && len(out) < q.Limit; i++ {
		out = append(out, api.LogEvent{EventID: "ev" + strconv.Itoa(i), TS: time.Unix(1700000000-int64(i), 0).UTC(), Message: "=SUM(A1)", Service: "api", Attrs: map[string]string{"k": "v"}})
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
func (f *fakeStore) LogAggregate(_ context.Context, q clickhouse.LogQuery, spec clickhouse.AggregateSpec) (api.LogAggregate, error) {
	f.lastLogQuery = q
	f.lastAggregate = spec
	v := 12.5
	return api.LogAggregate{BucketSeconds: int(spec.Bucket / time.Second), GroupBy: spec.GroupBy, Metric: spec.Metric, Of: spec.Of,
		Total: api.AggregateGroup{Key: "total", Rows: 3, Value: &v}, Groups: []api.AggregateGroup{{Key: "/orders", Rows: 3, Value: &v}}}, nil
}
func (f *fakeStore) RequestStats(_ context.Context, _, service, _ string, _, _ time.Time, bucket, limit int) (api.RequestStats, error) {
	f.lastService = service
	return api.RequestStats{BucketSeconds: bucket, Totals: api.RouteStats{Requests: 10, Errors: 1, ErrorPct: 10}, Routes: make([]api.RouteStats, limit)}, nil
}
func (f *fakeStore) TraceList(_ context.Context, q clickhouse.SpanQuery) ([]api.TraceSummary, error) {
	f.lastSpanQuery = q
	out := make([]api.TraceSummary, 0, q.Limit)
	for i := 0; i < q.Limit && i < 2; i++ {
		out = append(out, api.TraceSummary{TraceID: "t" + strconv.Itoa(i), RootSpanID: "s0", Service: "api", Name: "GET /x", Kind: "server", Status: "ok",
			StartTS: time.Unix(1700000000-int64(i), 123456000).UTC(), DurationUS: 1500, SpanCount: 3, Services: []string{"api", "db"}})
	}
	return out, nil
}
func (f *fakeStore) TraceStats(_ context.Context, q clickhouse.SpanQuery) (api.TraceStats, error) {
	f.lastSpanQuery = q
	return api.TraceStats{ByName: []api.SpanNameStats{{Name: "GET /x", Count: 2, P50US: 1000, P95US: 1500}}, Latency: []api.LatencyBucket{{FromUS: 1024, ToUS: 2048, Count: 2}}}, nil
}
func (f *fakeStore) TraceSpans(_ context.Context, _, id string, at time.Time) ([]api.Span, error) {
	f.lastTraceAt = at
	if id == "missing" {
		return nil, nil
	}
	t0 := time.Unix(1700000000, 0).UTC()
	return []api.Span{
		{TraceID: id, SpanID: "s0", Service: "api", Name: "GET /x", Kind: "server", Status: "ok", StartTS: t0, DurationUS: 2000, Attrs: map[string]string{}},
		{TraceID: id, SpanID: "s1", ParentSpanID: "s0", Service: "db", Name: "SELECT", Kind: "client", Status: "error", StartTS: t0.Add(500 * time.Microsecond), DurationUS: 1000, Attrs: map[string]string{"db.system": "postgres"}},
	}, nil
}
func (f *fakeStore) TraceErrors(_ context.Context, _, id string, _ time.Time) ([]api.ErrorOccurrence, error) {
	return []api.ErrorOccurrence{{EventID: "e-" + id, TraceID: id, IssueID: "i1"}}, nil
}
func (f *fakeStore) RequestStatsFromOTel(_ context.Context, _, service, _ string, _, _ time.Time, bucket, _ int) (api.RequestStats, bool, error) {
	f.lastService = service
	if service == "none" {
		return api.RequestStats{}, false, nil
	}
	return api.RequestStats{BucketSeconds: bucket, Totals: api.RouteStats{Requests: 7, P95MS: 120}}, true, nil
}
func (f *fakeStore) MetricNames(context.Context, string) ([]api.MetricInfo, error) {
	return []api.MetricInfo{{Name: "http.server.request.duration", Type: "histogram", Unit: "s", Temporality: "cumulative"}, {Name: "process.cpu.time", Type: "sum", Monotonic: true}}, nil
}
func (f *fakeStore) MetricSeries(_ context.Context, q clickhouse.MetricQuery) (api.MetricSeries, bool, error) {
	f.lastMetricQuery = q
	if q.Name == "nope" {
		return api.MetricSeries{}, false, nil
	}
	if q.Agg == "median" {
		return api.MetricSeries{}, true, clickhouse.ErrBadAgg
	}
	v := 1.5
	return api.MetricSeries{Name: q.Name, Type: "gauge", Agg: "avg", BucketSeconds: int(q.Bucket / time.Second), Series: []api.MetricSerie{{Label: "", Points: []api.MetricValue{{Value: &v}}}}}, true, nil
}
func (f *fakeStore) GetProjectIncludingDeleted(ctx context.Context, id string) (clickhouse.ProjectView, bool, error) {
	for _, d := range f.deleted {
		if d == id {
			return clickhouse.ProjectView{ID: id, Name: "gone", IsDeleted: true}, true, nil
		}
	}
	return f.GetProjectByID(ctx, id)
}
func (f *fakeStore) MarkProjectDeleted(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeStore) PurgeProjectData(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purged = append(f.purged, "project:"+id)
	return nil
}
func (f *fakeStore) PurgeUser(_ context.Context, id, hash, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purged = append(f.purged, "user:"+id+":"+hash+":"+email)
	return nil
}
func (f *fakeStore) ProjectUsage(_ context.Context, _ string, _ int) ([]api.UsageDay, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return []api.UsageDay{
		{Day: today, Source: "logs", Rows: 1000, BytesEst: 1 << 30},
		{Day: today.AddDate(0, 0, -1), Source: "errors", Rows: 10, BytesEst: 1 << 20},
		{Day: today.AddDate(0, 0, -200), Source: "logs", Rows: 5, BytesEst: 5 << 30}, // outside the window
	}, nil
}
func (f *fakeStore) TableCompression(context.Context) (map[string]float64, error) {
	return map[string]float64{"logs_raw": 0.5}, nil
}
func (f *fakeStore) RecomputeUsage(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purged = append(f.purged, "recompute:"+id)
	return nil
}
func (f *fakeStore) PendingMutations(_ context.Context, needle string) (int, error) {
	if needle == "p2" {
		return 2, nil
	}
	return 0, nil
}
func (f *fakeStore) AddAudit(_ context.Context, a api.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audit = append(f.audit, a)
	return nil
}
func (f *fakeStore) ListAudit(context.Context, string, int) ([]api.AuditEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.AuditEntry{}, f.audit...), nil
}
func (f *fakeStore) ListEnabledAlertRules(context.Context) ([]api.AlertRule, error) {
	return []api.AlertRule{{ID: "r1", Enabled: true}}, nil
}
func (f *fakeStore) GlobalLogVolume(_ context.Context, from, to time.Time, bucket int) ([]api.TimeSeriesPoint, error) {
	return []api.TimeSeriesPoint{{TS: from, Count: 12}, {TS: to, Count: uint64(bucket)}}, nil
}
func (f *fakeStore) Stats() clickhouse.Stats {
	return clickhouse.Stats{Queues: []clickhouse.BatcherStats{{Kind: "logs", Depth: 3, Capacity: 100, Written: 50}}}
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
func (f *fakeStore) ListAPIKeys(_ context.Context, id string) ([]api.APIKey, error) {
	var out []api.APIKey
	for _, k := range f.keys {
		if k.ProjectID == id {
			out = append(out, k)
		}
	}
	return out, nil
}
func (f *fakeStore) CreateAPIKey(_ context.Context, id, name string, scopes []string, by string) (api.APIKey, string, error) {
	k := api.APIKey{ID: fmt.Sprintf("k_%d", len(f.keys)+1), ProjectID: id, Name: name, KeyPrefix: "pl_abcdefgh", Scopes: scopes, CreatedAt: time.Now(), CreatedBy: by}
	f.keys = append(f.keys, k)
	return k, "pl_secret", nil
}
func (f *fakeStore) RevokeAPIKey(_ context.Context, id, keyID, by string) (string, error) {
	for i, k := range f.keys {
		if k.ProjectID == id && k.ID == keyID {
			now := time.Now()
			f.keys[i].RevokedAt, f.keys[i].RevokedBy = &now, by
			f.revoked = append(f.revoked, keyID)
			return "hash-" + keyID, nil
		}
	}
	return "", errors.New("key not found")
}
func (f *fakeStore) ListAlertRules(context.Context, string) ([]api.AlertRule, error) { return nil, nil }
func (f *fakeStore) UpsertAlertRule(_ context.Context, r api.AlertRule) (api.AlertRule, error) {
	r.ID = "r1"
	return r, nil
}
func (f *fakeStore) ListAlertFirings(context.Context, string, int) ([]api.AlertFiring, error) {
	return nil, nil
}
func (f *fakeStore) CountLogs(_ context.Context, q clickhouse.LogQuery) (uint64, error) {
	f.lastLogQuery = q
	return 7, nil
}

type fakeKeys struct{}

func (fakeKeys) ResolveByAPIKey(_ context.Context, key string) (tenant.Project, bool, error) {
	switch key {
	case "k-good":
		return tenant.Trusted(tenant.Project{ID: "p2", Name: "Second", KeyID: "k_all"}), true, nil
	case "k-read":
		return tenant.Project{ID: "p2", Name: "Second", KeyID: "k_read", Scopes: []string{tenant.ScopeRead}}, true, nil
	case "k-ingest":
		return tenant.Project{ID: "p2", Name: "Second", KeyID: "k_ingest", Scopes: []string{tenant.ScopeIngest}}, true, nil
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

func TestLogsAggregateParams(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/logs/aggregate?q=service:api&from=-6h&group_by=http.route&metric=p95&of=duration_ms&limit=5", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("aggregate: %d %s", rec.Code, rec.Body.String())
	}
	sp := store.lastAggregate
	if sp.GroupBy != "http.route" || sp.Metric != "p95" || sp.Of != "duration_ms" || sp.Limit != 5 || sp.Bucket != 10*time.Minute {
		t.Fatalf("spec: %+v", sp)
	}
	if !strings.Contains(store.lastLogQuery.Where, "service = ?") || out["metric"] != "p95" || len(out["groups"].([]any)) != 1 {
		t.Fatalf("aggregate response: %s", rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/logs/aggregate?metric=avg", nil, nil)
	if rec.Code != 400 || out["error"] != "bad_metric" {
		t.Fatalf("avg without a field must 400: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "GET", "/ui/api/logs/aggregate?metric=median&of=duration_ms", nil, nil)
	if rec.Code != 400 {
		t.Fatalf("unknown metric must 400: %d", rec.Code)
	}
	rec, _ = do(t, u, "GET", "/ui/api/logs/aggregate?bucket_seconds=3600", nil, nil)
	if rec.Code != 200 || store.lastAggregate.Metric != "count" || store.lastAggregate.Bucket != time.Hour {
		t.Fatalf("defaults: %d %+v", rec.Code, store.lastAggregate)
	}
}

func TestLogsExportStreamsPages(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	store.exportRows = 2500
	rec, _ := do(t, u, "GET", "/ui/api/logs/export?q=service:api&limit=2200&format=csv&cols=ts,event_id,message,service,attrs.k", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if h.Get("Content-Type") != "text/csv; charset=utf-8" || !strings.HasPrefix(h.Get("Content-Disposition"), `attachment; filename="pyloggle-logs-default-`) || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", h)
	}
	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\n"), "\n")
	if lines[0] != "ts,event_id,message,service,attrs.k" || len(lines) != 2201 {
		t.Fatalf("csv rows: header=%q n=%d", lines[0], len(lines))
	}
	if !strings.Contains(lines[1], `,'=SUM(A1),api,v`) {
		t.Fatalf("formula guard / attr column: %q", lines[1])
	}
	// 1000 + 1000 + 200: the keyset cursor carries the last event id of each page.
	if n := len(store.logQueries); n != 3 || store.logQueries[0].Limit != 1000 || store.logQueries[1].BeforeID != "ev999" || store.logQueries[2].Limit != 200 {
		t.Fatalf("paging: %d queries %+v", n, store.logQueries)
	}

	store.logQueries = nil
	rec, _ = do(t, u, "GET", "/ui/api/logs/export?format=ndjson&limit=5", nil, nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("ndjson: %d %v", rec.Code, rec.Header())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(rec.Body.String(), "\n", 2)[0]), &first); err != nil || first["event_id"] != "ev0" || first["message"] != "=SUM(A1)" {
		t.Fatalf("ndjson rows keep raw values: %v %v", first, err)
	}
	if n := strings.Count(rec.Body.String(), "\n"); n != 5 || len(store.logQueries) != 1 {
		t.Fatalf("ndjson limit: %d lines, %d queries", n, len(store.logQueries))
	}

	rec, out := do(t, u, "GET", "/ui/api/logs/export?format=xlsx", nil, nil)
	if rec.Code != 400 || out["error"] != "bad_format" {
		t.Fatalf("bad format: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/logs/export?q=level:%3Eerror", nil, nil)
	if rec.Code != 400 || out["error"] != "bad_query" {
		t.Fatalf("bad query must stay JSON: %d %s", rec.Code, rec.Body.String())
	}
	// The server cap wins over the requested limit.
	u.cfg.ExportMaxRows = 10
	store.logQueries = nil
	rec, _ = do(t, u, "GET", "/ui/api/logs/export?limit=500&format=ndjson", nil, nil)
	if n := strings.Count(rec.Body.String(), "\n"); rec.Code != 200 || n != 10 {
		t.Fatalf("cap: %d lines", n)
	}
}

func TestRequestStatsRoute(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/metrics/requests?from=-24h&service=api&limit=3", nil, nil)
	if rec.Code != 200 || store.lastService != "api" || len(out["routes"].([]any)) != 3 {
		t.Fatalf("requests: %d %s", rec.Code, rec.Body.String())
	}
	if out["totals"].(map[string]any)["error_pct"].(float64) != 10 || out["bucket_seconds"].(float64) != 1800 {
		t.Fatalf("totals/bucket: %s", rec.Body.String())
	}
}

func TestCSVSafe(t *testing.T) {
	for in, want := range map[string]string{"hello": "hello", "=1+1": "'=1+1", "+x": "'+x", "-5": "'-5", "@cmd": "'@cmd", "": ""} {
		if got := csvSafe(in); got != want {
			t.Fatalf("csvSafe(%q) = %q want %q", in, got, want)
		}
	}
	if n := exportFilename("My Project!", time.Date(2026, 10, 11, 7, 0, 0, 0, time.UTC), "csv"); n != "pyloggle-logs-My-Project--20261011-070000.csv" {
		t.Fatalf("filename: %s", n)
	}
}

func TestTracesAPI(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/traces?q=status:error+db.system:postgres&from=-6h&limit=2&include=stats", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("traces: %d %s", rec.Code, rec.Body.String())
	}
	sq := store.lastSpanQuery
	if !strings.Contains(sq.Where, "status = ?") || !strings.Contains(sq.Where, "attrs[?] = ?") || sq.Limit != 2 || sq.ProjectID != "default" {
		t.Fatalf("span query: %+v", sq)
	}
	if len(out["items"].([]any)) != 2 || out["next_cursor"] == nil || out["stats"] == nil {
		t.Fatalf("list: %s", rec.Body.String())
	}
	cur := out["next_cursor"].(string)
	rec, _ = do(t, u, "GET", "/ui/api/traces?cursor="+cur, nil, nil)
	if rec.Code != 200 || store.lastSpanQuery.BeforeID != "t1" || store.lastSpanQuery.BeforeTS.UnixMicro() != 1699999999123456 {
		t.Fatalf("cursor: %d %+v", rec.Code, store.lastSpanQuery)
	}
	rec, out = do(t, u, "GET", "/ui/api/traces?q=%22unterminated", nil, nil)
	if rec.Code != 400 || out["error"] != "bad_query" {
		t.Fatalf("bad query: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/traces/abc?at=2023-11-14T22:13:20Z", nil, nil)
	if rec.Code != 200 || store.lastTraceAt.IsZero() {
		t.Fatalf("trace: %d %s", rec.Code, rec.Body.String())
	}
	if out["trace_id"] != "abc" || out["duration_us"].(float64) != 2000 || len(out["spans"].([]any)) != 2 || len(out["errors"].([]any)) != 1 || out["logs"] == nil {
		t.Fatalf("trace detail: %s", rec.Body.String())
	}
	if svcs := out["services"].([]any); len(svcs) != 2 || svcs[0] != "api" || svcs[1] != "db" {
		t.Fatalf("services: %v", svcs)
	}
	if !strings.Contains(store.lastLogQuery.TraceID, "abc") || !store.lastLogQuery.Ascending {
		t.Fatalf("trace logs query: %+v", store.lastLogQuery)
	}
	rec, _ = do(t, u, "GET", "/ui/api/traces/missing", nil, nil)
	if rec.Code != 404 {
		t.Fatalf("missing trace: %d", rec.Code)
	}
	rec, out = do(t, u, "GET", "/ui/api/traces/fields", nil, nil)
	if rec.Code != 200 || len(out["fields"].([]any)) < 10 {
		t.Fatalf("fields: %s", rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/me", nil, nil)
	if rec.Code != 200 || out["features"].(map[string]any)["traces"] != true {
		t.Fatalf("me.features.traces: %s", rec.Body.String())
	}
}

func TestMetricsAPI(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/metrics/names", nil, nil)
	if rec.Code != 200 || len(out["items"].([]any)) != 2 || out["aggs"] == nil {
		t.Fatalf("names: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/metrics/series?name=process.cpu.time&agg=rate&group_by=host.name&from=-6h&limit=5&service=api", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("series: %d %s", rec.Code, rec.Body.String())
	}
	mq := store.lastMetricQuery
	if mq.Name != "process.cpu.time" || mq.Agg != "rate" || mq.GroupBy != "host.name" || mq.Limit != 5 || mq.Service != "api" || mq.Bucket != 10*time.Minute || mq.ProjectID != "default" {
		t.Fatalf("metric query: %+v", mq)
	}
	if len(out["series"].([]any)) != 1 {
		t.Fatalf("series body: %s", rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/metrics/series?name=nope", nil, nil)
	if rec.Code != 404 || out["error"] != "unknown_metric" {
		t.Fatalf("unknown: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/metrics/series?name=x&agg=median", nil, nil)
	if rec.Code != 400 || out["error"] != "bad_agg" {
		t.Fatalf("bad agg: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "GET", "/ui/api/metrics/series", nil, nil)
	if rec.Code != 400 {
		t.Fatalf("missing name: %d", rec.Code)
	}
	rec, out = do(t, u, "GET", "/ui/api/metrics/requests?source=otel&from=-24h", nil, nil)
	if rec.Code != 200 || out["source"] != "otel" || out["available"] != true || out["totals"].(map[string]any)["p95_ms"].(float64) != 120 {
		t.Fatalf("otel requests: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/metrics/requests?source=otel&service=none", nil, nil)
	if rec.Code != 200 || out["available"] != false || out["bucket_seconds"].(float64) == 0 || len(out["points"].([]any)) != 0 {
		t.Fatalf("otel unavailable: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "GET", "/ui/api/metrics/requests?source=prometheus", nil, nil)
	if rec.Code != 400 || out["error"] != "bad_source" {
		t.Fatalf("bad source: %d", rec.Code)
	}
	rec, out = do(t, u, "GET", "/ui/api/metrics/requests", nil, nil)
	if rec.Code != 200 || out["source"] != "logs" || out["available"] != true {
		t.Fatalf("logs source: %d %s", rec.Code, rec.Body.String())
	}
}

func (f *fakeStore) purgedList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.purged...)
}

func TestProjectUsageDeleteAndAudit(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/projects/default/usage", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body.String())
	}
	if out["window_days"].(float64) != float64(u.maxRetentionDays()) || out["stored_bytes_est"].(float64) != float64(1<<30+1<<20) {
		t.Fatalf("usage window must exclude rows older than the retention: %s", rec.Body.String())
	}
	logs := out["by_source"].(map[string]any)["logs"].(map[string]any)
	if logs["disk_est"].(float64) != float64(1<<29) || out["deleting"] != false || len(out["daily"].([]any)) != 3 {
		t.Fatalf("usage detail: %s", rec.Body.String())
	}
	rec, _ = do(t, u, "GET", "/ui/api/projects/nope/usage", nil, nil)
	if rec.Code != 404 {
		t.Fatalf("unknown project usage: %d", rec.Code)
	}

	// Retention override is capped by the server retention.
	rec, out = do(t, u, "POST", "/ui/api/projects/default", map[string]any{"name": "Default", "retention_days": 9999}, map[string]string{csrfHeader: "1"})
	if rec.Code != 400 || out["error"] != "bad_retention" {
		t.Fatalf("retention cap: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "POST", "/ui/api/projects/default", map[string]any{"name": "Default", "retention_days": 3}, map[string]string{csrfHeader: "1"})
	if rec.Code != 200 || store.created == nil && len(store.audit) == 0 {
		t.Fatalf("update with retention: %d %s", rec.Code, rec.Body.String())
	}

	// Purge a user (admin, CSRF).
	rec, _ = do(t, u, "POST", "/ui/api/projects/default/purge-user", map[string]any{"user_id_hash": "h1"}, map[string]string{csrfHeader: "1"})
	if rec.Code != 202 {
		t.Fatalf("purge user: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "POST", "/ui/api/projects/default/purge-user", map[string]any{}, map[string]string{csrfHeader: "1"})
	if rec.Code != 400 || out["error"] != "missing_user" {
		t.Fatalf("purge without a user: %d", rec.Code)
	}

	// Delete: tombstone, audit, background purge.
	rec, _ = do(t, u, "DELETE", "/ui/api/projects/p2", nil, nil)
	if rec.Code != 403 {
		t.Fatalf("delete needs the CSRF header: %d", rec.Code)
	}
	rec, out = do(t, u, "DELETE", "/ui/api/projects/p2", nil, map[string]string{csrfHeader: "1"})
	if rec.Code != 202 || out["status"] != "deleting" || len(store.deleted) != 1 || store.deleted[0] != "p2" {
		t.Fatalf("delete: %d %s %v", rec.Code, rec.Body.String(), store.deleted)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		p := store.purgedList()
		if len(p) >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	has := func(want string) bool {
		for _, x := range store.purgedList() {
			if x == want {
				return true
			}
		}
		return false
	}
	if !has("user:default:h1:") || !has("project:p2") || !has("recompute:p2") {
		t.Fatalf("background purge: %v", store.purgedList())
	}
	// While mutations run the project still answers with deleting=true.
	rec, out = do(t, u, "GET", "/ui/api/projects/p2", nil, nil)
	if rec.Code != 200 || out["project"].(map[string]any)["deleting"] != true {
		t.Fatalf("deleting project: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "DELETE", "/ui/api/projects/nope", nil, map[string]string{csrfHeader: "1"})
	if rec.Code != 404 {
		t.Fatalf("delete unknown: %d", rec.Code)
	}

	rec, out = do(t, u, "GET", "/ui/api/admin/audit", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("audit: %d %s", rec.Code, rec.Body.String())
	}
	var actions []string
	for _, it := range out["items"].([]any) {
		actions = append(actions, it.(map[string]any)["action"].(string))
	}
	if strings.Join(actions, ",") != "project.update,user.purge,project.delete" {
		t.Fatalf("audit actions: %v", actions)
	}
	// Non-admins cannot delete.
	cfg := u.cfg
	cfg.UIAuthMode = "azure"
	cfg.UIAdmins = []string{"root@example.com"}
	ua := NewForTest(cfg, store, fakeKeys{}, nil)
	rec, _ = do(t, ua, "DELETE", "/ui/api/projects/default", nil, map[string]string{csrfHeader: "1"})
	if rec.Code == 202 {
		t.Fatal("non-admin delete must be refused")
	}
}

func TestSystemAPI(t *testing.T) {
	u, _, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/system", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("system: %d %s", rec.Code, rec.Body.String())
	}
	if out["version"] == nil || out["go_version"] == nil || out["uptime_s"] == nil {
		t.Fatalf("process section: %s", rec.Body.String())
	}
	ch := out["clickhouse"].(map[string]any)
	if ch["up"] != false || ch["version"] != "" {
		t.Fatalf("without a collector ClickHouse reads as down: %v", ch)
	}
	ing := out["ingest"].(map[string]any)
	if len(ing["rate"].([]any)) != 2 || ing["queues"] == nil || ing["events"] == nil {
		t.Fatalf("ingest section: %v", ing)
	}
	al := out["alerting"].(map[string]any)
	if al["rules_enabled"].(float64) != 1 || al["enabled"] != false {
		t.Fatalf("alerting section: %v", al)
	}
	ret := out["retention"].(map[string]any)
	if ret["logs"].(float64) != float64(u.cfg.RetentionLogsDays) {
		t.Fatalf("retention: %v", ret)
	}
	cfg := u.cfg
	cfg.UIAuthMode = "azure"
	cfg.UIAdmins = []string{"root@example.com"}
	ua := NewForTest(cfg, &fakeStore{}, fakeKeys{}, nil)
	rec, _ = do(t, ua, "GET", "/ui/api/system", nil, nil)
	if rec.Code == 200 {
		t.Fatal("the System page is for admins")
	}
}

func TestAPIKeyScopes(t *testing.T) {
	u, _, _ := newTestUI("disabled")
	q := u.APIKeyAPIHandler()
	viaKey := func(method, path, key string, body string) (*httptest.ResponseRecorder, map[string]any) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		rec := httptest.NewRecorder()
		q.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}
	// /v1/q: a read-only key reads but cannot mutate; an ingest-only key cannot even read.
	rec, _ := viaKey("GET", "/v1/q/issues", "k-read", "")
	if rec.Code != 200 {
		t.Fatalf("read key GET: %d %s", rec.Code, rec.Body.String())
	}
	rec, out := viaKey("POST", "/v1/q/issues/i1/state", "k-read", `{"status":"resolved"}`)
	if rec.Code != 403 || out["error"] != "insufficient_scope" {
		t.Fatalf("read key POST: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = viaKey("GET", "/v1/q/issues", "k-ingest", "")
	if rec.Code != 403 || out["error"] != "insufficient_scope" {
		t.Fatalf("ingest key GET: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = viaKey("POST", "/v1/q/issues/i1/state", "k-good", `{"status":"resolved"}`)
	if rec.Code != 200 {
		t.Fatalf("full key POST: %d %s", rec.Code, rec.Body.String())
	}
	// Keyless reads on the default project are unchanged.
	rec, _ = viaKey("GET", "/v1/q/issues", "", "")
	if rec.Code != 200 {
		t.Fatalf("keyless GET: %d %s", rec.Code, rec.Body.String())
	}
	// The UI login with a key needs the read scope.
	rec, out = do(t, u, "POST", "/ui/api/login", map[string]any{"api_key": "k-ingest"}, map[string]string{csrfHeader: "1"})
	if rec.Code != 403 || out["error"] != "insufficient_scope" {
		t.Fatalf("login with ingest key: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "POST", "/ui/api/login", map[string]any{"api_key": "k-read"}, map[string]string{csrfHeader: "1"})
	if rec.Code != 200 {
		t.Fatalf("login with read key: %d %s", rec.Code, rec.Body.String())
	}
	// A browser session that logged in with a read-only key cannot mutate through /ui/api.
	cookie := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == apiKeyCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("login did not set the key cookie")
	}
	req := httptest.NewRequest("POST", "/ui/api/issues/i1/state", bytes.NewReader([]byte(`{"status":"resolved"}`)))
	req.Header.Set(csrfHeader, "1")
	req.AddCookie(&http.Cookie{Name: apiKeyCookie, Value: cookie})
	rr := httptest.NewRecorder()
	u.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("ui mutation with a read-only key cookie: %d %s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest("GET", "/ui/api/issues", nil)
	req.AddCookie(&http.Cookie{Name: apiKeyCookie, Value: cookie})
	rr = httptest.NewRecorder()
	u.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("ui read with a read-only key cookie: %d %s", rr.Code, rr.Body.String())
	}
}

func TestProjectKeysAPI(t *testing.T) {
	u, store, _ := newTestUI("disabled")
	rec, out := do(t, u, "GET", "/ui/api/projects/p2/keys", nil, nil)
	if rec.Code != 200 || len(out["items"].([]any)) != 0 || len(out["scopes"].([]any)) != 3 {
		t.Fatalf("list empty: %d %s", rec.Code, rec.Body.String())
	}
	rec, out = do(t, u, "POST", "/ui/api/projects/p2/keys", map[string]any{"name": "ci", "scopes": []string{"read"}}, map[string]string{csrfHeader: "1"})
	if rec.Code != 201 || out["api_key"] != "pl_secret" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	key := out["key"].(map[string]any)
	if key["name"] != "ci" || key["key_prefix"] != "pl_abcdefgh" || len(key["scopes"].([]any)) != 1 {
		t.Fatalf("created key: %v", key)
	}
	for _, bad := range []map[string]any{{"name": "", "scopes": []string{"read"}}, {"name": "x", "scopes": []string{}}, {"name": "x", "scopes": []string{"admin"}}} {
		rec, _ = do(t, u, "POST", "/ui/api/projects/p2/keys", bad, map[string]string{csrfHeader: "1"})
		if rec.Code != 400 {
			t.Fatalf("bad body %v: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
	rec, _ = do(t, u, "POST", "/ui/api/projects/nope/keys", map[string]any{"name": "x", "scopes": []string{"read"}}, map[string]string{csrfHeader: "1"})
	if rec.Code != 404 {
		t.Fatalf("unknown project: %d", rec.Code)
	}
	rec, out = do(t, u, "GET", "/ui/api/projects/p2/keys", nil, nil)
	if rec.Code != 200 || len(out["items"].([]any)) != 1 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "POST", "/ui/api/projects/p2/keys/"+key["id"].(string)+"/revoke", nil, map[string]string{csrfHeader: "1"})
	if rec.Code != 200 || len(store.revoked) != 1 {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = do(t, u, "POST", "/ui/api/projects/p2/keys/k_missing/revoke", nil, map[string]string{csrfHeader: "1"})
	if rec.Code != 404 {
		t.Fatalf("revoke unknown: %d", rec.Code)
	}
	var actions []string
	for _, a := range store.audit {
		actions = append(actions, a.Action)
	}
	if strings.Join(actions, ",") != "key.create,key.revoke" {
		t.Fatalf("audit: %v", actions)
	}
	// Non-admins cannot manage keys.
	u.cfg.UIAdmins = []string{"admin@example.com"}
	u.cfg.UIAuthMode = "azure"
	rec, _ = do(t, u, "GET", "/ui/api/projects/p2/keys", nil, nil)
	if rec.Code != 401 {
		t.Fatalf("anonymous in azure mode: %d", rec.Code)
	}
}
