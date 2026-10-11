package alerting

import (
	"context"
	"log"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
	"pyloggle/internal/storage/clickhouse"
)

type IssueSpike struct {
	IssueID   string `json:"issue_id"`
	Title     string `json:"title"`
	CurCount  uint64 `json:"cur_count"`
	PrevCount uint64 `json:"prev_count"`
	Delta     int64  `json:"delta"`
}

// IssueRef points at an issue from an alert (new_issue / regression).
type IssueRef struct {
	IssueID string `json:"issue_id"`
	Title   string `json:"title"`
	Culprit string `json:"culprit,omitempty"`
	Count   uint64 `json:"count,omitempty"`
	URL     string `json:"url,omitempty"`
}

type AlertEvent struct {
	Rule        api.AlertRule `json:"rule"`
	FiredAt     time.Time     `json:"fired_at"`
	WindowStart time.Time     `json:"window_start"`
	WindowEnd   time.Time     `json:"window_end"`

	Kind string `json:"kind"` // error_rate | issue_spike | new_issue | regression

	ErrorCount uint64  `json:"error_count,omitempty"`
	ErrorRate  float64 `json:"error_rate,omitempty"` // errors/sec

	Spikes []IssueSpike `json:"spikes,omitempty"`
	Issues []IssueRef   `json:"issues,omitempty"`
}

type Runner struct {
	cfg       config.Config
	ch        *clickhouse.Client
	notifiers []Notifier
}

func NewRunner(cfg config.Config, ch *clickhouse.Client) *Runner {
	ns := make([]Notifier, 0, 2)
	if cfg.SMTPHost != "" {
		ns = append(ns, NewEmailNotifier(cfg))
	}
	if cfg.AlertTelegramBotToken != "" {
		ns = append(ns, NewTelegramNotifier(cfg))
	}
	ns = append(ns, NewWebhookNotifier(cfg))
	return &Runner{cfg: cfg, ch: ch, notifiers: ns}
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
	r.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	rules, err := r.ch.ListEnabledAlertRules(ctx)
	if err != nil {
		log.Printf("alerting: list rules: %v", err)
		return
	}
	if len(rules) == 0 {
		return
	}

	now := time.Now().UTC()
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

		// Cooldown check (persisted)
		if rule.CooldownSeconds > 0 {
			if last, ok, err := r.ch.GetLastAlertFiredAt(ctx, rule.ID); err == nil && ok {
				if now.Sub(last) < time.Duration(rule.CooldownSeconds)*time.Second {
					continue
				}
			}
		}

		kind := strings.ToLower(strings.TrimSpace(rule.ConditionType))
		switch kind {
		case "error_rate":
			r.evalErrorRate(ctx, rule, now)
		case "issue_spike", "issue_spikes":
			r.evalIssueSpike(ctx, rule, now)
		case "new_issue", "new_issues":
			r.evalNewIssues(ctx, rule, now)
		case "regression", "regressions":
			r.evalRegressions(ctx, rule, now)
		default:
			// ignore unknown rule types
		}
	}
}

func (r *Runner) evalErrorRate(ctx context.Context, rule api.AlertRule, now time.Time) {
	w := time.Duration(rule.TimeWindowSeconds) * time.Second
	if w <= 0 {
		w = 60 * time.Second
	}
	from := now.Add(-w)
	to := now

	n, err := r.ch.CountErrorOccurrences(ctx, rule, from, to)
	if err != nil {
		log.Printf("alerting: rule=%s error_rate query: %v", rule.ID, err)
		return
	}
	rate := float64(n) / w.Seconds()
	if rule.Threshold <= 0 {
		return
	}
	if rate < rule.Threshold {
		return
	}

	ev := AlertEvent{
		Rule:        rule,
		FiredAt:     now,
		WindowStart: from,
		WindowEnd:   to,
		Kind:        "error_rate",
		ErrorCount:  n,
		ErrorRate:   rate,
	}
	r.fire(ctx, ev)
}

func (r *Runner) evalIssueSpike(ctx context.Context, rule api.AlertRule, now time.Time) {
	w := time.Duration(rule.TimeWindowSeconds) * time.Second
	if w <= 0 {
		w = 60 * time.Second
	}
	curFrom := now.Add(-w)
	curTo := now
	prevFrom := now.Add(-2 * w)
	prevTo := now.Add(-w)

	rows, err := r.ch.IssueSpikeCounts(ctx, rule, prevFrom, prevTo, curFrom, curTo, 50)
	if err != nil {
		log.Printf("alerting: rule=%s issue_spike query: %v", rule.ID, err)
		return
	}
	if rule.Threshold <= 0 {
		return
	}

	spikes := make([]IssueSpike, 0, 5)
	for _, row := range rows {
		delta := int64(row.CurCount) - int64(row.PrevCount)
		if float64(delta) < rule.Threshold {
			continue
		}
		spikes = append(spikes, IssueSpike{
			IssueID:   row.IssueID,
			Title:     row.Title,
			CurCount:  row.CurCount,
			PrevCount: row.PrevCount,
			Delta:     delta,
		})
		if len(spikes) >= 5 {
			break
		}
	}
	if len(spikes) == 0 {
		return
	}

	ev := AlertEvent{
		Rule:        rule,
		FiredAt:     now,
		WindowStart: curFrom,
		WindowEnd:   curTo,
		Kind:        "issue_spike",
		Spikes:      spikes,
	}
	r.fire(ctx, ev)
}

func (r *Runner) issueURL(id string) string {
	if r.cfg.PublicURL == "" {
		return ""
	}
	return r.cfg.PublicURL + "/ui/issues/" + id
}

func (r *Runner) refs(rows []api.IssueSummary) []IssueRef {
	out := make([]IssueRef, 0, len(rows))
	for _, s := range rows {
		out = append(out, IssueRef{IssueID: s.IssueID, Title: s.Title, Culprit: s.Culprit, Count: s.OccurrenceCount, URL: r.issueURL(s.IssueID)})
	}
	return out
}

// evalNewIssues fires when at least `threshold` (default 1) issues were first seen in the window.
func (r *Runner) evalNewIssues(ctx context.Context, rule api.AlertRule, now time.Time) {
	w := time.Duration(rule.TimeWindowSeconds) * time.Second
	if w <= 0 {
		w = 60 * time.Second
	}
	from, to := now.Add(-w), now
	rows, err := r.ch.NewIssues(ctx, rule, from, to, 20)
	if err != nil {
		log.Printf("alerting: rule=%s new_issue query: %v", rule.ID, err)
		return
	}
	min := int(rule.Threshold)
	if min < 1 {
		min = 1
	}
	if len(rows) < min {
		return
	}
	r.fire(ctx, AlertEvent{Rule: rule, FiredAt: now, WindowStart: from, WindowEnd: to, Kind: "new_issue", Issues: r.refs(rows)})
}

// evalRegressions fires when resolved issues came back in the window.
func (r *Runner) evalRegressions(ctx context.Context, rule api.AlertRule, now time.Time) {
	w := time.Duration(rule.TimeWindowSeconds) * time.Second
	if w <= 0 {
		w = 60 * time.Second
	}
	from, to := now.Add(-w), now
	rows, err := r.ch.RegressedIssues(ctx, rule, from, to, 20)
	if err != nil {
		log.Printf("alerting: rule=%s regression query: %v", rule.ID, err)
		return
	}
	min := int(rule.Threshold)
	if min < 1 {
		min = 1
	}
	if len(rows) < min {
		return
	}
	r.fire(ctx, AlertEvent{Rule: rule, FiredAt: now, WindowStart: from, WindowEnd: to, Kind: "regression", Issues: r.refs(rows)})
}

func (r *Runner) fire(ctx context.Context, ev AlertEvent) {
	ok := false
	for _, n := range r.notifiers {
		if err := n.Notify(ctx, ev); err != nil {
			log.Printf("alerting: notify %s: %v", n.Name(), err)
			continue
		}
		ok = true
	}
	if !ok {
		return
	}
	_ = r.ch.InsertAlertFiring(ctx, clickhouse.AlertFiring{
		RuleID:        ev.Rule.ID,
		ProjectID:     ev.Rule.ProjectID,
		Service:       ev.Rule.Service,
		Env:           ev.Rule.Env,
		ConditionType: ev.Kind,
		FiredAt:       ev.FiredAt,
		WindowStart:   ev.WindowStart,
		WindowEnd:     ev.WindowEnd,
		Payload:       ev,
	})
}
