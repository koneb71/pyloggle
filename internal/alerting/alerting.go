// Package alerting evaluates alert rules on a timer and delivers firings through notifiers.
package alerting

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
	"pyloggle/internal/logquery"
	"pyloggle/internal/metrics"
	"pyloggle/internal/storage/clickhouse"
)

type IssueSpike struct {
	IssueID   string `json:"issue_id"`
	Title     string `json:"title"`
	CurCount  uint64 `json:"cur_count"`
	PrevCount uint64 `json:"prev_count"`
	Delta     int64  `json:"delta"`
	Users     uint64 `json:"users,omitempty"` // how many people, never who
	URL       string `json:"url,omitempty"`
}

// IssueRef points at an issue from an alert (new_issue / regression).
type IssueRef struct {
	IssueID string `json:"issue_id"`
	Title   string `json:"title"`
	Culprit string `json:"culprit,omitempty"`
	Count   uint64 `json:"count,omitempty"`
	Users   uint64 `json:"users,omitempty"`
	URL     string `json:"url,omitempty"`
}

// RuleRef is the part of a rule that receivers may see: never the notification targets, so a
// webhook consumer does not learn every e-mail address and URL the rule fans out to.
type RuleRef struct {
	ID                string  `json:"id"`
	ProjectID         string  `json:"project_id"`
	Name              string  `json:"name"`
	Service           string  `json:"service,omitempty"`
	Env               string  `json:"env,omitempty"`
	HTTPRoute         string  `json:"http_route,omitempty"`
	HTTPMethod        string  `json:"http_method,omitempty"`
	ConditionType     string  `json:"condition_type"`
	Query             string  `json:"query,omitempty"`
	Comparison        string  `json:"comparison,omitempty"`
	Threshold         float64 `json:"threshold"`
	TimeWindowSeconds uint32  `json:"time_window_seconds"`
}

// AlertEvent is one firing. Rule carries the full rule for the notifiers (targets); the JSON
// form (webhooks, the firings table) is Public().
type AlertEvent struct {
	Rule        api.AlertRule
	FiredAt     time.Time
	WindowStart time.Time
	WindowEnd   time.Time

	Kind string // error_rate | issue_spike | new_issue | regression | log_query

	ErrorCount uint64
	ErrorRate  float64 // errors/sec
	Count      uint64  // log_query: matching lines in the window
	URL        string  // deep link into the UI (empty without PYLOGGLE_PUBLIC_URL)

	Spikes []IssueSpike
	Issues []IssueRef

	// Filled by enrich() just before notifying; best effort, so either may be empty.
	TopIssues []IssueContext // the first few issues with the latest time each happened
	Samples   []LogSample    // log_query: a few of the matching lines

	Recap *Recap // daily_recap only

	// What to record as sent once at least one notifier delivered (see once.go).
	mark     []string
	markOpen bool
}

// PublicAlert is the wire shape of a firing.
type PublicAlert struct {
	Rule        RuleRef      `json:"rule"`
	FiredAt     time.Time    `json:"fired_at"`
	WindowStart time.Time    `json:"window_start"`
	WindowEnd   time.Time    `json:"window_end"`
	Kind        string       `json:"kind"`
	ErrorCount  uint64       `json:"error_count,omitempty"`
	ErrorRate   float64      `json:"error_rate,omitempty"`
	Count       uint64       `json:"count,omitempty"`
	URL         string       `json:"url,omitempty"`
	Spikes      []IssueSpike `json:"spikes,omitempty"`
	Issues      []IssueRef   `json:"issues,omitempty"`

	Context   AlertContext   `json:"context"`
	TopIssues []IssueContext `json:"top_issues,omitempty"`
	Samples   []LogSample    `json:"samples,omitempty"`
	Recap     *Recap         `json:"recap,omitempty"`
}

// AlertContext is the alert in words, as the notifications say it.
type AlertContext struct {
	Title   string `json:"title"`   // "Too many errors"
	Summary string `json:"summary"` // "42 errors in 5 minutes, 8.4 a minute. The limit is 6 a minute."
	Scope   string `json:"scope"`   // "api · prod" or "all apps · all environments"
	Window  string `json:"window"`  // "Oct 11, 11:13–11:18 UTC (5 minutes)"
}

func (ev AlertEvent) Public() PublicAlert {
	r := ev.Rule
	return PublicAlert{
		Rule: RuleRef{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Service: r.Service, Env: r.Env, HTTPRoute: r.HTTPRoute, HTTPMethod: r.HTTPMethod,
			ConditionType: r.ConditionType, Query: r.Query, Comparison: r.Comparison, Threshold: r.Threshold, TimeWindowSeconds: r.TimeWindowSeconds},
		FiredAt: ev.FiredAt, WindowStart: ev.WindowStart, WindowEnd: ev.WindowEnd, Kind: ev.Kind,
		ErrorCount: ev.ErrorCount, ErrorRate: ev.ErrorRate, Count: ev.Count, URL: ev.URL, Spikes: ev.Spikes, Issues: ev.Issues,
		Context:   AlertContext{Title: kindTitle(ev), Summary: headline(ev), Scope: scopeWords(r), Window: windowWords(ev)},
		TopIssues: ev.TopIssues, Samples: ev.Samples, Recap: ev.Recap,
	}
}

// MarshalJSON makes every serialization of an event the public shape.
func (ev AlertEvent) MarshalJSON() ([]byte, error) { return json.Marshal(ev.Public()) }

// Store is what the runner needs from storage (satisfied by *clickhouse.Client; faked in tests).
type Store interface {
	ListEnabledAlertRules(ctx context.Context) ([]api.AlertRule, error)
	GetLastAlertFiredAt(ctx context.Context, ruleID string) (time.Time, bool, error)
	InsertAlertFiring(ctx context.Context, f clickhouse.AlertFiring) error
	CountErrorOccurrences(ctx context.Context, rule api.AlertRule, from, to time.Time) (uint64, error)
	IssueSpikeCounts(ctx context.Context, rule api.AlertRule, prevFrom, prevTo, curFrom, curTo time.Time, limit int) ([]clickhouse.IssueSpikeRow, error)
	NewIssues(ctx context.Context, rule api.AlertRule, from, to time.Time, limit int) ([]api.IssueSummary, error)
	RegressionEvents(ctx context.Context, rule api.AlertRule, from, to time.Time, limit int) ([]clickhouse.RegressionEvent, error)
	CountLogs(ctx context.Context, q clickhouse.LogQuery) (uint64, error)

	// What each rule already sent (once.go).
	NotifiedSubjects(ctx context.Context, projectID, ruleID string, subjects []string, since time.Time) (map[string]bool, error)
	MarkNotified(ctx context.Context, projectID, ruleID string, subjects []string, open bool) error
	IncidentOpen(ctx context.Context, projectID, ruleID string) (bool, error)

	// Recap of yesterday (recap.go).
	GetProjectByID(ctx context.Context, id string) (clickhouse.ProjectView, bool, error)
	Overview(ctx context.Context, projectID, service, env string, from, to time.Time) (api.Overview, error)
	ServiceHealth(ctx context.Context, projectID, env string, from, to time.Time, limit int) ([]api.ServiceHealth, error)
	TopIssues(ctx context.Context, projectID, service, env string, from, to time.Time, limit int) ([]api.IssueSummary, error)
	ListAlertRules(ctx context.Context, projectID string) ([]api.AlertRule, error)
	ListAlertFirings(ctx context.Context, projectID string, limit int) ([]api.AlertFiring, error)
	OpenIncidents(ctx context.Context, projectID string) ([]clickhouse.OpenIncident, error)

	// Used to add context to a firing (enrich.go).
	QueryLogs(ctx context.Context, q clickhouse.LogQuery) ([]api.LogEvent, error)
	QueryOccurrences(ctx context.Context, projectID, issueID string, from, to, before time.Time, limit int) ([]api.ErrorOccurrence, error)
	TracesWithSpans(ctx context.Context, projectID string, traceIDs []string, from, to time.Time) (map[string]bool, error)
}

type Runner struct {
	cfg          config.Config
	store        Store
	notifiers    []Notifier
	badRules     map[string]string // rule id -> last logged parse error (logged once)
	now          func() time.Time
	enrichBudget time.Duration // how long adding context to one firing may take
}

func NewRunner(cfg config.Config, ch *clickhouse.Client) *Runner {
	ns := make([]Notifier, 0, 4)
	if cfg.SMTPHost != "" {
		ns = append(ns, NewEmailNotifier(cfg))
	}
	if cfg.AlertTelegramBotToken != "" {
		ns = append(ns, NewTelegramNotifier(cfg))
	}
	ns = append(ns, NewSlackNotifier(cfg), NewWebhookNotifier(cfg))
	return NewRunnerWith(cfg, ch, ns)
}

// NewRunnerWith builds a runner around any store and notifier set (tests).
func NewRunnerWith(cfg config.Config, store Store, notifiers []Notifier) *Runner {
	return &Runner{cfg: cfg, store: store, notifiers: notifiers, badRules: map[string]string{}, now: func() time.Time { return time.Now().UTC() },
		enrichBudget: 5 * time.Second}
}

func (r *Runner) Run(ctx context.Context) {
	if !r.cfg.AlertingEnabled {
		return
	}
	interval := r.cfg.AlertingPollInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	t := time.NewTicker(interval)
	defer t.Stop()

	// Run one pass immediately.
	r.RunOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce evaluates every enabled rule once.
func (r *Runner) RunOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	rules, err := r.store.ListEnabledAlertRules(ctx)
	if err != nil {
		log.Printf("alerting: list rules: %v", err)
		return
	}
	now := r.now()
	metrics.AlertLastRun.Set(float64(now.Unix()))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if rule.TimeWindowSeconds == 0 {
			rule.TimeWindowSeconds = 60
		}
		if rule.CooldownSeconds == 0 {
			rule.CooldownSeconds = rule.TimeWindowSeconds
		}
		kind := strings.ToLower(strings.TrimSpace(rule.ConditionType))
		metrics.AlertEvaluations.Inc(kind)

		// Cooldown check (persisted). A recap goes out once per local day instead.
		if rule.CooldownSeconds > 0 && kind != "daily_recap" {
			if last, ok, err := r.store.GetLastAlertFiredAt(ctx, rule.ID); err == nil && ok {
				if now.Sub(last) < time.Duration(rule.CooldownSeconds)*time.Second {
					continue
				}
			}
		}

		switch kind {
		case "error_rate":
			r.evalErrorRate(ctx, rule, now)
		case "issue_spike", "issue_spikes":
			r.evalIssueSpike(ctx, rule, now)
		case "new_issue", "new_issues":
			r.evalNewIssues(ctx, rule, now)
		case "regression", "regressions":
			r.evalRegressions(ctx, rule, now)
		case "log_query":
			r.evalLogQuery(ctx, rule, now)
		case "daily_recap":
			r.evalDailyRecap(ctx, rule, now)
		default:
			// ignore unknown rule types
		}
	}
}

func window(rule api.AlertRule) time.Duration {
	w := time.Duration(rule.TimeWindowSeconds) * time.Second
	if w <= 0 {
		w = 60 * time.Second
	}
	return w
}

func (r *Runner) evalErrorRate(ctx context.Context, rule api.AlertRule, now time.Time) {
	w := window(rule)
	from, to := now.Add(-w), now
	n, err := r.store.CountErrorOccurrences(ctx, rule, from, to)
	if err != nil {
		log.Printf("alerting: rule=%s error_rate query: %v", rule.ID, err)
		return
	}
	rate := float64(n) / w.Seconds()
	if !r.incidentStarts(ctx, rule, rule.Threshold > 0 && rate >= rule.Threshold) {
		return
	}
	r.fire(ctx, AlertEvent{Rule: rule, FiredAt: now, WindowStart: from, WindowEnd: to, Kind: "error_rate", ErrorCount: n, ErrorRate: rate,
		URL: r.logsURL(scopeQuery(rule)+" level:error", from, to), mark: []string{incidentSubject}, markOpen: true})
}

func (r *Runner) evalIssueSpike(ctx context.Context, rule api.AlertRule, now time.Time) {
	w := window(rule)
	curFrom, curTo := now.Add(-w), now
	prevFrom, prevTo := now.Add(-2*w), now.Add(-w)
	rows, err := r.store.IssueSpikeCounts(ctx, rule, prevFrom, prevTo, curFrom, curTo, 50)
	if err != nil {
		log.Printf("alerting: rule=%s issue_spike query: %v", rule.ID, err)
		return
	}
	if rule.Threshold <= 0 {
		return
	}
	var jumped []clickhouse.IssueSpikeRow
	for _, row := range rows {
		if delta := int64(row.CurCount) - int64(row.PrevCount); float64(delta) >= rule.Threshold {
			jumped = append(jumped, row)
		}
	}
	// A problem alerts a jump rule at most once a day.
	jumped = unsent(ctx, r, rule, now.Add(-24*time.Hour), jumped, func(row clickhouse.IssueSpikeRow) string { return jumpSubject(row.IssueID) })
	spikes := make([]IssueSpike, 0, 5)
	var mark []string
	for _, row := range jumped {
		spikes = append(spikes, IssueSpike{IssueID: row.IssueID, Title: row.Title, CurCount: row.CurCount, PrevCount: row.PrevCount,
			Delta: int64(row.CurCount) - int64(row.PrevCount), Users: row.CurUsers, URL: r.issueURL(row.IssueID)})
		mark = append(mark, jumpSubject(row.IssueID))
		if len(spikes) >= 5 {
			break
		}
	}
	if len(spikes) == 0 {
		return
	}
	r.fire(ctx, AlertEvent{Rule: rule, FiredAt: now, WindowStart: curFrom, WindowEnd: curTo, Kind: "issue_spike", Spikes: spikes, URL: r.issuesURL("open", curFrom), mark: mark})
}

// evalNewIssues fires when at least `threshold` (default 1) issues were first seen in the window.
func (r *Runner) evalNewIssues(ctx context.Context, rule api.AlertRule, now time.Time) {
	w := window(rule)
	from, to := now.Add(-w), now
	rows, err := r.store.NewIssues(ctx, rule, from, to, 20)
	if err != nil {
		log.Printf("alerting: rule=%s new_issue query: %v", rule.ID, err)
		return
	}
	// Each new problem alerts a rule once.
	rows = unsent(ctx, r, rule, time.Time{}, rows, func(s api.IssueSummary) string { return issueSubject(s.IssueID) })
	if len(rows) < minCount(rule.Threshold) {
		return
	}
	mark := make([]string, 0, len(rows))
	for _, s := range rows {
		mark = append(mark, issueSubject(s.IssueID))
	}
	r.fire(ctx, AlertEvent{Rule: rule, FiredAt: now, WindowStart: from, WindowEnd: to, Kind: "new_issue", Issues: r.refs(rows), URL: r.issuesURL("open", from), mark: mark})
}

// evalRegressions fires when resolved issues came back in the window.
func (r *Runner) evalRegressions(ctx context.Context, rule api.AlertRule, now time.Time) {
	w := window(rule)
	from, to := now.Add(-w), now
	events, err := r.store.RegressionEvents(ctx, rule, from, to, 20)
	if err != nil {
		log.Printf("alerting: rule=%s regression query: %v", rule.ID, err)
		return
	}
	// Each comeback alerts a rule once; editing the problem afterwards is not a comeback.
	events = unsent(ctx, r, rule, time.Time{}, events, func(e clickhouse.RegressionEvent) string { return cameBackSubject(e.ID) })
	var rows []api.IssueSummary
	var mark []string
	listed := map[string]bool{}
	for _, e := range events {
		mark = append(mark, cameBackSubject(e.ID))
		if !listed[e.IssueID] {
			listed[e.IssueID] = true
			rows = append(rows, e.Issue)
		}
	}
	if len(rows) < minCount(rule.Threshold) {
		return
	}
	r.fire(ctx, AlertEvent{Rule: rule, FiredAt: now, WindowStart: from, WindowEnd: to, Kind: "regression", Issues: r.refs(rows), URL: r.issuesURL("regressed", from), mark: mark})
}

// evalLogQuery counts the lines matching the rule's query in the window (ending eval-delay ago so
// batches still in flight are not misread) and compares the count with the threshold.
func (r *Runner) evalLogQuery(ctx context.Context, rule api.AlertRule, now time.Time) {
	parsed, err := logquery.Parse(rule.Query)
	if err != nil {
		r.badRule(rule, err)
		return
	}
	where, args, err := parsed.SQL()
	if err != nil {
		r.badRule(rule, err)
		return
	}
	delete(r.badRules, rule.ID)
	w := window(rule)
	to := now.Add(-r.cfg.AlertingEvalDelay)
	from := to.Add(-w)
	n, err := r.store.CountLogs(ctx, clickhouse.LogQuery{ProjectID: rule.ProjectID, From: from, To: to, Where: where, Args: args,
		Service: rule.Service, Env: rule.Env, HTTPRoute: rule.HTTPRoute, HTTPMethod: rule.HTTPMethod, Limit: 1})
	if err != nil {
		log.Printf("alerting: rule=%s log_query: %v", rule.ID, err)
		return
	}
	if !r.incidentStarts(ctx, rule, Compare(rule.Comparison, float64(n), rule.Threshold)) {
		return
	}
	r.fire(ctx, AlertEvent{Rule: rule, FiredAt: now, WindowStart: from, WindowEnd: to, Kind: "log_query", Count: n, URL: r.logsURL(rule.Query, from, to),
		mark: []string{incidentSubject}, markOpen: true})
}

// Compare applies a rule comparison (gt by default) to a measured value.
func Compare(op string, value, threshold float64) bool {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "", "gt":
		return value > threshold
	case "gte":
		return value >= threshold
	case "lt":
		return value < threshold
	case "lte":
		return value <= threshold
	case "eq":
		return value == threshold
	}
	return false
}

func (r *Runner) badRule(rule api.AlertRule, err error) {
	if r.badRules[rule.ID] == err.Error() {
		return
	}
	r.badRules[rule.ID] = err.Error()
	log.Printf("alerting: rule=%s (%s) has an invalid query and is skipped: %v", rule.ID, rule.Name, err)
}

func minCount(threshold float64) int {
	if m := int(threshold); m > 1 {
		return m
	}
	return 1
}

func (r *Runner) publicURL() string {
	return strings.TrimRight(strings.TrimSpace(r.cfg.PublicURL), "/")
}

func (r *Runner) issueURL(id string) string {
	if r.publicURL() == "" {
		return ""
	}
	return r.publicURL() + "/ui/issues/" + id
}

func (r *Runner) issuesURL(status string, from time.Time) string {
	if r.publicURL() == "" {
		return ""
	}
	v := url.Values{}
	v.Set("status", status)
	v.Set("from", from.UTC().Format(time.RFC3339))
	return r.publicURL() + "/ui/issues?" + v.Encode()
}

func (r *Runner) logsURL(q string, from, to time.Time) string {
	if r.publicURL() == "" {
		return ""
	}
	v := url.Values{}
	v.Set("q", strings.TrimSpace(q))
	v.Set("from", from.UTC().Format(time.RFC3339))
	v.Set("to", to.UTC().Format(time.RFC3339))
	return r.publicURL() + "/ui/logs?" + v.Encode()
}

// scopeQuery renders the rule's scope filters in the explorer's query language.
func scopeQuery(rule api.AlertRule) string {
	var parts []string
	add := func(field, v string) {
		if v == "" {
			return
		}
		if strings.ContainsAny(v, " \"'()") {
			v = `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
		}
		parts = append(parts, field+":"+v)
	}
	add("service", rule.Service)
	add("env", rule.Env)
	add("http.route", rule.HTTPRoute)
	add("http.method", rule.HTTPMethod)
	return strings.Join(parts, " ")
}

func (r *Runner) refs(rows []api.IssueSummary) []IssueRef {
	out := make([]IssueRef, 0, len(rows))
	for _, s := range rows {
		out = append(out, IssueRef{IssueID: s.IssueID, Title: s.Title, Culprit: s.Culprit, Count: s.OccurrenceCount, Users: s.AffectedUsers, URL: r.issueURL(s.IssueID)})
	}
	return out
}

func (r *Runner) fire(ctx context.Context, ev AlertEvent) {
	r.enrich(ctx, &ev)
	ok := false
	for _, n := range r.notifiers {
		if err := n.Notify(ctx, ev); err != nil {
			metrics.AlertNotifyFailure.Inc(n.Name())
			log.Printf("alerting: notify %s: %v", n.Name(), err)
			continue
		}
		ok = true
	}
	if !ok {
		return
	}
	if len(ev.mark) > 0 {
		if err := r.store.MarkNotified(ctx, ev.Rule.ProjectID, ev.Rule.ID, ev.mark, ev.markOpen); err != nil {
			log.Printf("alerting: rule=%s: could not record what was sent (it may be sent again): %v", ev.Rule.ID, err)
		}
	}
	metrics.AlertFirings.Inc(ev.Kind)
	_ = r.store.InsertAlertFiring(ctx, clickhouse.AlertFiring{
		RuleID:        ev.Rule.ID,
		ProjectID:     ev.Rule.ProjectID,
		Service:       ev.Rule.Service,
		Env:           ev.Rule.Env,
		ConditionType: ev.Kind,
		FiredAt:       ev.FiredAt,
		WindowStart:   ev.WindowStart,
		WindowEnd:     ev.WindowEnd,
		Count:         ev.Count,
		URL:           ev.URL,
		Payload:       ev.Public(),
	})
}
