package ingest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
	"pyloggle/internal/issue"
	"pyloggle/internal/model"
	"pyloggle/internal/tenant"
)

// Sink receives accepted rows (the ClickHouse batching client, or a memory sink in tests).
type Sink interface {
	EnqueueLog(ctx context.Context, ev model.Event) error
	EnqueueErrorOccurrence(ctx context.Context, occ issue.ErrorOccurrence) error
}

// IssueStateStore is the slice of storage the pipeline needs for regression detection.
type IssueStateStore interface {
	GetIssueStatus(ctx context.Context, projectID, issueID string) (status string, regressed bool, found bool, err error)
	SetIssueState(ctx context.Context, st api.IssueStateChange) error
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

// Pipeline is the shared ingest path: authentication, normalization, protection
// (quota, rate limit, sampling), issue grouping and regression detection.
type Pipeline struct {
	cfg    config.Config
	sink   Sink
	states *issue.StateCache
	store  IssueStateStore
	res    KeyResolver
	lim    *limiter
	qt     *quotaTracker
	now    func() time.Time
}

func NewPipeline(cfg config.Config, sink Sink, store IssueStateStore, res KeyResolver) *Pipeline {
	p := &Pipeline{
		cfg:   cfg,
		sink:  sink,
		store: store,
		res:   res,
		lim:   newLimiter(cfg),
		qt:    newQuotaTracker(),
		now:   time.Now,
	}
	var loader issue.StateLoader
	if store != nil {
		loader = store.GetIssueStatus
	}
	p.states = issue.NewStateCache(time.Minute, loader)
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
		if t.ProjectID == "" && apiKey == "" {
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

// Process normalizes, protects and stores one event. rawLen is the encoded size for quotas.
func (p *Pipeline) Process(ctx context.Context, t Tenant, ev model.Event, rawLen int) Outcome {
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
	isErr := ev.IsError()

	if !p.qt.allow(t.ProjectID, isErr, rawLen, t.MaxStorageGB, now) {
		return DroppedQuota
	}
	if o := p.lim.outcome(ev, t.RPS, t.Burst); o != Accepted {
		return o
	}

	if isErr && !ev.SkipIssue {
		occ := issue.FromEvent(ev)
		ev.Attrs = ev.Attrs.Set("pyloggle.issue_id", occ.IssueID)
		p.detectRegression(ctx, occ)
		if err := p.sink.EnqueueErrorOccurrence(ctx, occ); err != nil {
			return DroppedQueueFull
		}
	}
	if err := p.sink.EnqueueLog(ctx, ev); err != nil && isErr {
		return DroppedQueueFull
	}
	return Accepted
}

// detectRegression reopens resolved issues that occur again. Ignored issues stay ignored.
func (p *Pipeline) detectRegression(ctx context.Context, occ issue.ErrorOccurrence) {
	if p.store == nil {
		return
	}
	st, known, err := p.states.Get(ctx, occ.ProjectID, occ.IssueID)
	if err != nil || !known {
		return
	}
	if st.Status != issue.StatusResolved {
		return
	}
	change := api.IssueStateChange{
		ProjectID: occ.ProjectID, Service: occ.Service, Env: occ.Env, IssueID: occ.IssueID,
		Status: issue.StatusOpen, Regressed: true, RegressedFrom: issue.StatusResolved,
		UpdatedBy: "system", Comment: "regressed: new occurrence after resolution",
	}
	if err := p.store.SetIssueState(ctx, change); err == nil {
		p.states.Set(occ.ProjectID, occ.IssueID, issue.State{Status: issue.StatusOpen, Regressed: true})
	}
}

func newEventID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}
