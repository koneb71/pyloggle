package ingest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/issue"
	"github.com/koneb71/pyloggle/internal/metrics"
	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/tenant"
)

// Sink receives accepted rows (the ClickHouse batching client, or a memory sink in tests).
type Sink interface {
	EnqueueLog(ctx context.Context, ev model.Event) error
	EnqueueErrorOccurrence(ctx context.Context, occ issue.ErrorOccurrence) error
	EnqueueSpans(ctx context.Context, spans []model.Span) error
	EnqueueMetricPoints(ctx context.Context, points []model.MetricPoint) error
	EnqueueRequestCounts(ctx context.Context, counts []model.RequestCount) error
}

// IssueStateStore is the slice of storage the pipeline needs for regression and snooze handling.
type IssueStateStore interface {
	GetIssueState(ctx context.Context, projectID, issueID string) (issue.State, bool, error)
	SetIssueState(ctx context.Context, st api.IssueStateChange) error
}

// ReleaseStore is optional: when the state store also knows release first-seen times, opaque
// release names (build ids, dates) can be ordered for resolve-in-release.
type ReleaseStore interface {
	ReleaseFirstSeen(ctx context.Context, projectID, release string) (time.Time, bool, error)
}

// KeyResolver maps API keys to projects.
type KeyResolver interface {
	ResolveByAPIKey(ctx context.Context, apiKey string) (tenant.Project, bool, error)
}

// Tenant is the resolved sender of a request.
type Tenant struct {
	ProjectID    string
	RPS          float64
	Burst        float64
	MaxStorageGB uint32
}

// Outcome says what happened to one event.
type Outcome int

const (
	Accepted Outcome = iota
	DroppedQuota
	DroppedRateLimit
	DroppedSampled
	DroppedQueueFull
)

func (o Outcome) String() string {
	switch o {
	case Accepted:
		return "accepted"
	case DroppedQuota:
		return "quota"
	case DroppedRateLimit:
		return "rate_limit"
	case DroppedSampled:
		return "sampled"
	case DroppedQueueFull:
		return "queue_full"
	}
	return "unknown"
}

// ErrUnauthorized is returned by Authenticate for unknown keys.
var ErrUnauthorized = errors.New("unauthorized")

// ErrInsufficientScope is returned by Authenticate for a key without the ingest scope.
var ErrInsufficientScope = errors.New("insufficient_scope: this key cannot ingest")

// Pipeline is the shared ingest path: authentication, normalization, protection
// (quota, rate limit, sampling), issue grouping and regression detection.
type Pipeline struct {
	cfg    config.Config
	sink   Sink
	states *issue.StateCache
	store  IssueStateStore
	res    KeyResolver
	lim    *limiter
	qt     *usageGuard
	dd     *traceDedupe

	relMu    sync.Mutex
	releases map[string]releaseSeen
	now      func() time.Time
}

func NewPipeline(cfg config.Config, sink Sink, store IssueStateStore, res KeyResolver) *Pipeline {
	p := &Pipeline{
		cfg:   cfg,
		sink:  sink,
		store: store,
		res:   res,
		lim:   newLimiter(cfg),
		now:   time.Now,
	}
	var usage UsageStore
	if us, ok := store.(UsageStore); ok {
		usage = us
	}
	window := cfg.RetentionLogsDays
	if cfg.RetentionErrorsDays > window {
		window = cfg.RetentionErrorsDays
	}
	p.qt = newUsageGuard(usage, time.Duration(window)*24*time.Hour)
	p.dd = newTraceDedupe(2*time.Minute, 20000)
	var loader issue.StateLoader
	if store != nil {
		loader = store.GetIssueState
	}
	p.states = issue.NewStateCache(time.Minute, loader)
	p.releases = map[string]releaseSeen{}
	return p
}

// States exposes the cache so UI state changes made in this process are seen by ingest at once.
func (p *Pipeline) States() *issue.StateCache { return p.states }

// Authenticate resolves the tenant from X-API-Key or Authorization: Bearer. Requests without a
// key use the default project when one is configured (unchanged v1 behavior).
func (p *Pipeline) Authenticate(ctx context.Context, r *http.Request) (Tenant, error) {
	t := Tenant{ProjectID: p.cfg.DefaultProjID, RPS: p.cfg.RateLimitRPS, Burst: p.cfg.RateLimitBurst}
	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if apiKey == "" {
		if auth := r.Header.Get("Authorization"); len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
			apiKey = strings.TrimSpace(auth[7:])
		}
	}
	if apiKey == "" || secretEqual(apiKey, p.cfg.SharedSecret) {
		// No key, or the 0.1 shared secret: both mean the default project, if there is one.
		if t.ProjectID == "" {
			return t, ErrUnauthorized
		}
		return t, nil
	}
	if p.res == nil {
		return t, ErrUnauthorized
	}
	proj, ok, err := p.res.ResolveByAPIKey(ctx, apiKey)
	if err != nil {
		return t, err
	}
	if !ok {
		return t, ErrUnauthorized
	}
	if !proj.Has(tenant.ScopeIngest) {
		return t, ErrInsufficientScope
	}
	t.ProjectID = proj.ID
	t.MaxStorageGB = proj.MaxStorageGB
	if proj.MaxEventsPerSec > 0 {
		t.RPS = float64(proj.MaxEventsPerSec)
		if t.Burst <= 0 {
			t.Burst = t.RPS * 2
		}
		if t.Burst < t.RPS {
			t.Burst = t.RPS
		}
	}
	return t, nil
}

// ProcessSpans stores a batch of spans from one OTLP request: project defaults, the storage
// quota and one rate-limit token per batch (spans are never sampled: a partial trace is
// worse than none). Returns how many were accepted and, when none were, why.
func (p *Pipeline) ProcessSpans(ctx context.Context, t Tenant, spans []model.Span, rawLen int) (int, Outcome) {
	if len(spans) == 0 {
		return 0, Accepted
	}
	now := p.now().UTC()
	if !p.qt.allow(t.ProjectID, false, rawLen, t.MaxStorageGB, now) {
		return 0, DroppedQuota
	}
	for i := range spans {
		sp := &spans[i]
		sp.ProjectID = t.ProjectID
		if sp.Service == "" {
			sp.Service = p.cfg.DefaultSvc
		}
		if sp.Env == "" {
			sp.Env = p.cfg.DefaultEnv
		}
		if sp.StartTS.IsZero() {
			sp.StartTS = now
		}
		if sp.Kind == "" {
			sp.Kind = "unspecified"
		}
		if sp.Status == "" {
			sp.Status = "unset"
		}
	}
	if t.RPS > 0 && !p.lim.take(t.ProjectID+"|"+spans[0].Service, t.RPS, t.Burst) {
		return 0, DroppedRateLimit
	}
	if err := p.sink.EnqueueSpans(ctx, spans); err != nil {
		return 0, DroppedQueueFull
	}
	metrics.SpansAccepted.Add(float64(len(spans)), t.ProjectID)
	return len(spans), Accepted
}

// ProcessMetrics stores a batch of OTLP metric points: project defaults, the storage quota and
// one rate-limit token per batch.
func (p *Pipeline) ProcessMetrics(ctx context.Context, t Tenant, points []model.MetricPoint, rawLen int) (int, Outcome) {
	if len(points) == 0 {
		return 0, Accepted
	}
	now := p.now().UTC()
	if !p.qt.allow(t.ProjectID, false, rawLen, t.MaxStorageGB, now) {
		return 0, DroppedQuota
	}
	for i := range points {
		pt := &points[i]
		pt.ProjectID = t.ProjectID
		if pt.Service == "" {
			pt.Service = p.cfg.DefaultSvc
		}
		if pt.Env == "" {
			pt.Env = p.cfg.DefaultEnv
		}
		if pt.TS.IsZero() {
			pt.TS = now
		}
		if pt.Type == "" {
			pt.Type = "gauge"
		}
		if pt.Temporality == "" {
			pt.Temporality = "unspecified"
		}
	}
	if t.RPS > 0 && !p.lim.take(t.ProjectID+"|"+points[0].Service, t.RPS, t.Burst) {
		return 0, DroppedRateLimit
	}
	if err := p.sink.EnqueueMetricPoints(ctx, points); err != nil {
		return 0, DroppedQueueFull
	}
	metrics.PointsAccepted.Add(float64(len(points)), t.ProjectID)
	return len(points), Accepted
}

// ProcessRequestCounts stores the request counts an app sent (POST /v1/ingest/requests): the
// storage quota and one rate-limit token per batch, like spans. Counts are never sampled. Returns
// how many were accepted and, when none were, why; a count that fails Normalize is rejected.
func (p *Pipeline) ProcessRequestCounts(ctx context.Context, t Tenant, counts []model.RequestCount, rawLen int) (accepted int, rejected []error, outcome Outcome) {
	if len(counts) == 0 {
		return 0, nil, Accepted
	}
	now := p.now().UTC()
	if !p.qt.allow(t.ProjectID, false, rawLen, t.MaxStorageGB, now) {
		return 0, nil, DroppedQuota
	}
	kept := counts[:0]
	for i := range counts {
		c := counts[i]
		if err := c.Normalize(now); err != nil {
			rejected = append(rejected, err)
			continue
		}
		c.ProjectID = t.ProjectID
		if c.Service == "" {
			c.Service = p.cfg.DefaultSvc
		}
		if c.Env == "" {
			c.Env = p.cfg.DefaultEnv
		}
		kept = append(kept, c)
	}
	if len(kept) == 0 {
		return 0, rejected, Accepted
	}
	if t.RPS > 0 && !p.lim.take(t.ProjectID+"|"+kept[0].Service, t.RPS, t.Burst) {
		return 0, rejected, DroppedRateLimit
	}
	if err := p.sink.EnqueueRequestCounts(ctx, kept); err != nil {
		return 0, rejected, DroppedQueueFull
	}
	return len(kept), rejected, Accepted
}

// Process normalizes, protects and stores one event. rawLen is the encoded size for quotas.
func (p *Pipeline) Process(ctx context.Context, t Tenant, ev model.Event, rawLen int) Outcome {
	o := p.process(ctx, t, ev, rawLen)
	metrics.IngestEvents.Inc(t.ProjectID, o.String())
	return o
}

func (p *Pipeline) process(ctx context.Context, t Tenant, ev model.Event, rawLen int) Outcome {
	now := p.now().UTC()
	ev.Normalize()
	if ev.Source == "" {
		ev.Source = "sdk"
	}
	if ev.EventID == "" {
		ev.EventID = newEventID()
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = model.At(now)
	}
	if ev.Service == "" {
		ev.Service = p.cfg.DefaultSvc
	}
	if ev.Env == "" {
		ev.Env = p.cfg.DefaultEnv
	}
	ev.ProjectID = t.ProjectID
	if ev.HTTP != nil {
		if ev.HTTP.RemoteAddr != "" {
			ev.Attrs = ev.Attrs.Set("http.remote_addr", ev.HTTP.RemoteAddr)
		}
		if ev.HTTP.ServerAddr != "" {
			ev.Attrs = ev.Attrs.Set("http.server_addr", ev.HTTP.ServerAddr)
		}
	}
	// The 0.2 SDKs carry request latency in attrs.latency_ms; analytics and request stats read the
	// duration_ms column, so promote it when the column is unset.
	if ev.DurationMS == 0 {
		if f, err := strconv.ParseFloat(ev.Attrs.Get("latency_ms"), 64); err == nil && f > 0 {
			ev.DurationMS = f
		}
	}
	isErr := ev.IsError()

	if !p.qt.allow(t.ProjectID, isErr, rawLen, t.MaxStorageGB, now) {
		return DroppedQuota
	}

	duplicate := false
	if isErr && !ev.SkipIssue {
		occ := issue.FromEvent(ev)
		if ev.Exception != nil && ev.TraceID != "" {
			// The same crash can reach us twice: the SDK's report and an OpenTelemetry
			// exception log or span event for the same trace. The first one is the
			// occurrence; the second stays a log line pointing at it.
			if prev, dup := p.dd.first(t.ProjectID, ev.TraceID, ev.Exception.Type, ev.EventID, occ.IssueID, now); dup {
				ev.SkipIssue = true
				ev.Attrs = ev.Attrs.Set("pyloggle.issue_id", prev.IssueID)
				ev.Attrs = ev.Attrs.Set("pyloggle.duplicate_of", prev.EventID)
				duplicate = true
			}
		}
	}
	// Requests are counted before sampling and the rate limit, so the counts stay exact when
	// the line itself is not kept. A second report of the same crash is not a second request.
	if c, ok := model.CountOf(ev); ok && !duplicate {
		_ = p.sink.EnqueueRequestCounts(ctx, []model.RequestCount{c})
	}
	if ev.CountOnly {
		return Accepted
	}
	if o := p.lim.outcome(ev, t.RPS, t.Burst); o != Accepted {
		return o
	}
	if isErr && !ev.SkipIssue {
		occ := issue.FromEvent(ev)
		ev.Attrs = ev.Attrs.Set("pyloggle.issue_id", occ.IssueID)
		p.applyIssueState(ctx, occ)
		if err := p.sink.EnqueueErrorOccurrence(ctx, occ); err != nil {
			return DroppedQueueFull
		}
	}
	if err := p.sink.EnqueueLog(ctx, ev); err != nil && isErr {
		return DroppedQueueFull
	}
	return Accepted
}

// applyIssueState reopens issues whose lifecycle state says they should come back: resolved
// issues on a new occurrence (a newer release when they were resolved in a release), and
// snoozed issues when the snooze expires or its occurrence budget is spent. Plain ignores stay
// ignored.
func (p *Pipeline) applyIssueState(ctx context.Context, occ issue.ErrorOccurrence) {
	if p.store == nil {
		return
	}
	st, known, err := p.states.Get(ctx, occ.ProjectID, occ.IssueID)
	if err != nil || !known {
		return
	}
	now := p.now().UTC()
	var change api.IssueStateChange
	switch st.Status {
	case issue.StatusResolved:
		if !p.isRegression(ctx, occ, st) {
			return
		}
		comment := "regressed: new occurrence after resolution"
		if st.ResolvedInRelease != "" {
			comment = fmt.Sprintf("regressed: resolved in %s, seen again in %s", st.ResolvedInRelease, occ.Release)
		}
		change = api.IssueStateChange{Status: issue.StatusOpen, Regressed: true, RegressedFrom: issue.StatusResolved, Comment: comment, Kind: "regression", Release: occ.Release}
	case issue.StatusIgnored:
		switch {
		case !st.SnoozeUntil.IsZero() && now.After(st.SnoozeUntil):
			change = api.IssueStateChange{Status: issue.StatusOpen, Regressed: true, RegressedFrom: issue.StatusIgnored, Comment: "snooze expired", Kind: "unsnooze"}
		case st.SnoozeCount > 0 && p.states.Bump(occ.ProjectID, occ.IssueID) >= st.SnoozeCount:
			change = api.IssueStateChange{Status: issue.StatusOpen, Regressed: true, RegressedFrom: issue.StatusIgnored, Comment: fmt.Sprintf("occurrence budget of %d reached", st.SnoozeCount), Kind: "unsnooze"}
		default:
			return
		}
	default:
		return
	}
	change.ProjectID, change.Service, change.Env, change.IssueID, change.UpdatedBy = occ.ProjectID, occ.Service, occ.Env, occ.IssueID, "system"
	if err := p.store.SetIssueState(ctx, change); err == nil {
		p.states.Set(occ.ProjectID, occ.IssueID, issue.State{Status: issue.StatusOpen, Regressed: true, RegressedFrom: change.RegressedFrom, Assignee: st.Assignee})
	}
}

// isRegression decides whether an occurrence of a resolved issue reopens it.
func (p *Pipeline) isRegression(ctx context.Context, occ issue.ErrorOccurrence, st issue.State) bool {
	if st.ResolvedInRelease == "" {
		return true
	}
	if occ.Release == "" {
		return false // the old deployment is probably still running
	}
	return issue.ReleaseNewer(occ.Release, st.ResolvedInRelease, p.releaseFirstSeen(ctx, occ.ProjectID))
}

type releaseSeen struct {
	t       time.Time
	ok      bool
	expires time.Time
}

// releaseFirstSeen returns a lookup backed by the ReleaseStore with a five-minute cache.
func (p *Pipeline) releaseFirstSeen(ctx context.Context, projectID string) func(string) (time.Time, bool) {
	rs, ok := p.store.(ReleaseStore)
	if !ok {
		return nil
	}
	return func(release string) (time.Time, bool) {
		key := projectID + "|" + release
		now := p.now()
		p.relMu.Lock()
		if e, ok := p.releases[key]; ok && now.Before(e.expires) {
			p.relMu.Unlock()
			return e.t, e.ok
		}
		p.relMu.Unlock()
		t, found, err := rs.ReleaseFirstSeen(ctx, projectID, release)
		if err != nil {
			return time.Time{}, false
		}
		if found {
			// Negative results are not cached: a release is unknown only until its first rows
			// are flushed, and a stale "unknown" would misjudge the next comparison.
			p.relMu.Lock()
			p.releases[key] = releaseSeen{t: t, ok: true, expires: now.Add(5 * time.Minute)}
			p.relMu.Unlock()
		}
		return t, found
	}
}

func newEventID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}
