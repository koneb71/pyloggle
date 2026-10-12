package alerting

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/metrics"
)

// A recap rule (condition_type daily_recap) sends a summary of yesterday once a day, after its
// local send hour. "Yesterday" is the previous calendar day in the rule's time zone.

// Recap is yesterday in numbers and short lists; it is also the webhook payload's "recap".
// It names problems and apps, never people.
type Recap struct {
	Date     string         `json:"date"` // yesterday, local: "2026-10-11"
	Timezone string         `json:"timezone"`
	Project  string         `json:"project"`
	Totals   RecapTotals    `json:"totals"`
	Previous RecapTotals    `json:"previous"` // the day before, for comparison
	New      []RecapIssue   `json:"new_issues,omitempty"`
	CameBack []RecapIssue   `json:"came_back,omitempty"`
	Top      []RecapIssue   `json:"top_issues,omitempty"`
	Apps     []RecapApp     `json:"apps,omitempty"` // only apps that need a look
	Alerts   []RecapAlert   `json:"alerts,omitempty"`
	Ongoing  []RecapOngoing `json:"ongoing,omitempty"`
}

// RecapTotals are one day's numbers.
type RecapTotals struct {
	Errors         uint64 `json:"errors"`
	People         uint64 `json:"people"`
	Messages       uint64 `json:"messages"`
	Apps           uint64 `json:"apps"`
	Requests       uint64 `json:"requests"`
	FailedRequests uint64 `json:"failed_requests"`
	NewProblems    int    `json:"new_problems"`
	CameBack       int    `json:"came_back"`
}

// RecapIssue is one problem in a recap list.
type RecapIssue struct {
	IssueID string `json:"issue_id"`
	Title   string `json:"title"`
	Count   uint64 `json:"count"`
	People  uint64 `json:"people,omitempty"`
	URL     string `json:"url,omitempty"`
}

// RecapApp is an app that was failing, degraded or silent.
type RecapApp struct {
	Service   string    `json:"service"`
	Status    string    `json:"status"` // failing | degraded | silent
	FailedPct float64   `json:"failed_pct"`
	Errors    uint64    `json:"errors"`
	LastSeen  time.Time `json:"last_seen,omitzero"`
}

// RecapAlert is how many alerts one rule sent yesterday.
type RecapAlert struct {
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

// RecapOngoing is a count rule whose incident has not gone back to normal.
type RecapOngoing struct {
	Rule  string    `json:"rule"`
	Kind  string    `json:"kind"`
	Since time.Time `json:"since"`
}

const (
	recapListLen   = 5   // problems shown per list
	recapCountsLen = 200 // problems whose counts for the day are looked up
)

// recapDue returns yesterday's bounds when a recap rule should send now, in its time zone.
func recapDue(rule api.AlertRule, now time.Time) (day string, from, to time.Time, due bool, err error) {
	loc, err := recapLocation(rule.Timezone)
	if err != nil {
		return "", time.Time{}, time.Time{}, false, err
	}
	local := now.In(loc)
	if local.Hour() < int(rule.SendHour) {
		return "", time.Time{}, time.Time{}, false, nil
	}
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return local.Format("2006-01-02"), today.AddDate(0, 0, -1), today, true, nil
}

func recapLocation(tz string) (*time.Location, error) {
	if strings.TrimSpace(tz) == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(strings.TrimSpace(tz))
}

// evalDailyRecap sends yesterday's recap once per local day, after the send hour. A server that
// was down at that hour sends it as soon as it is back, the same day.
func (r *Runner) evalDailyRecap(ctx context.Context, rule api.AlertRule, now time.Time) {
	day, from, to, due, err := recapDue(rule, now)
	if err != nil {
		r.badRule(rule, err)
		return
	}
	if !due {
		return
	}
	subject := recapSubject(day)
	if len(unsent(ctx, r, rule, time.Time{}, []string{subject}, func(s string) string { return s })) == 0 {
		return
	}
	rc := r.buildRecap(ctx, rule, from, to)
	r.fire(ctx, AlertEvent{Rule: rule, FiredAt: now, WindowStart: from.UTC(), WindowEnd: to.UTC(), Kind: "daily_recap", Count: rc.Totals.Errors,
		Recap: &rc, URL: r.overviewURL(from, to), mark: []string{subject}})
}

// PreviewRecap renders what a recap rule would send now, for yesterday ("Test this alert").
func PreviewRecap(ctx context.Context, cfg config.Config, store Store, rule api.AlertRule, now time.Time) (string, error) {
	loc, err := recapLocation(rule.Timezone)
	if err != nil {
		return "", err
	}
	r := NewRunnerWith(cfg, store, nil)
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	from := today.AddDate(0, 0, -1)
	rc := r.buildRecap(ctx, rule, from, today)
	ev := AlertEvent{Rule: rule, FiredAt: now, WindowStart: from.UTC(), WindowEnd: today.UTC(), Kind: "daily_recap", Recap: &rc, URL: r.overviewURL(from, today)}
	return renderPlain(buildMessage(ev)), nil
}

// buildRecap gathers yesterday's numbers. Each lookup is best effort within one budget: what
// cannot be read in time is left out and counted in pyloggle_alert_enrich_failures_total.
func (r *Runner) buildRecap(ctx context.Context, rule api.AlertRule, from, to time.Time) Recap {
	budget := 2 * r.enrichBudget
	if dl, ok := ctx.Deadline(); ok {
		if half := time.Until(dl) / 2; half < budget {
			budget = half
		}
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	tz := strings.TrimSpace(rule.Timezone)
	if tz == "" {
		tz = "UTC"
	}
	rc := Recap{Date: from.Format("2006-01-02"), Timezone: tz, Project: rule.ProjectID}
	var failed error
	note := func(err error) {
		if err != nil && failed == nil {
			failed = err
		}
	}
	defer func() {
		if p := recover(); p != nil {
			metrics.AlertEnrichFailure.Inc("panic")
			log.Printf("alerting: rule=%s: building the recap panicked: %v", rule.ID, p)
		}
		if failed != nil {
			metrics.AlertEnrichFailure.Inc("query")
			log.Printf("alerting: rule=%s: parts of the recap could not be read: %v", rule.ID, failed)
		}
	}()

	if p, ok, err := r.store.GetProjectByID(ctx, rule.ProjectID); err == nil && ok && p.Name != "" {
		rc.Project = p.Name
	} else {
		note(err)
	}
	rc.Totals = r.recapTotals(ctx, rule, from, to, &rc.Apps, note)
	rc.Previous = r.recapTotals(ctx, rule, from.AddDate(0, 0, -1), from, nil, note)

	// Every list counts yesterday only: the busiest problems of the day give the counts, and a
	// problem missing from them (far down the list) keeps its all-time figures.
	day := map[string]api.IssueSummary{}
	if rows, err := r.store.TopIssues(ctx, rule.ProjectID, rule.Service, rule.Env, from, to, recapCountsLen); err == nil {
		for i, s := range rows {
			day[s.IssueID] = s
			if i < recapListLen {
				rc.Top = append(rc.Top, RecapIssue{IssueID: s.IssueID, Title: s.Title, Count: s.OccurrenceCount, People: s.AffectedUsers, URL: r.issueURL(s.IssueID)})
			}
		}
	} else {
		note(err)
	}
	item := func(s api.IssueSummary) RecapIssue {
		if d, ok := day[s.IssueID]; ok {
			s.OccurrenceCount, s.AffectedUsers = d.OccurrenceCount, d.AffectedUsers
		}
		return RecapIssue{IssueID: s.IssueID, Title: s.Title, Count: s.OccurrenceCount, People: s.AffectedUsers, URL: r.issueURL(s.IssueID)}
	}
	if rows, err := r.store.NewIssues(ctx, rule, from, to, 50); err == nil {
		rc.Totals.NewProblems = len(rows)
		for _, s := range firstN(rows, recapListLen) {
			rc.New = append(rc.New, item(s))
		}
	} else {
		note(err)
	}
	if evs, err := r.store.RegressionEvents(ctx, rule, from, to, 50); err == nil {
		seen := map[string]bool{}
		for _, e := range evs {
			if seen[e.IssueID] {
				continue
			}
			seen[e.IssueID] = true
			rc.Totals.CameBack++
			if len(rc.CameBack) < recapListLen {
				issue := e.Issue
				issue.IssueID = e.IssueID
				rc.CameBack = append(rc.CameBack, item(issue))
			}
		}
	} else {
		note(err)
	}
	if rules, err := r.store.ListAlertRules(ctx, rule.ProjectID); err == nil {
		names := map[string]api.AlertRule{}
		for _, ar := range rules {
			names[ar.ID] = ar
		}
		if firings, err := r.store.ListAlertFirings(ctx, rule.ProjectID, 500); err == nil {
			counts := map[string]int{}
			for _, f := range firings {
				if f.RuleID == rule.ID || f.FiredAt.Before(from) || !f.FiredAt.Before(to) {
					continue
				}
				counts[ruleName(names, f.RuleID)]++
			}
			for name, n := range counts {
				rc.Alerts = append(rc.Alerts, RecapAlert{Rule: name, Count: n})
			}
			sort.Slice(rc.Alerts, func(i, j int) bool {
				if rc.Alerts[i].Count != rc.Alerts[j].Count {
					return rc.Alerts[i].Count > rc.Alerts[j].Count
				}
				return rc.Alerts[i].Rule < rc.Alerts[j].Rule
			})
		} else {
			note(err)
		}
		if open, err := r.store.OpenIncidents(ctx, rule.ProjectID); err == nil {
			for _, o := range open {
				ar, ok := names[o.RuleID]
				if !ok || !ar.Enabled {
					continue
				}
				rc.Ongoing = append(rc.Ongoing, RecapOngoing{Rule: ruleName(names, o.RuleID), Kind: ar.ConditionType, Since: o.Since.UTC()})
			}
		} else {
			note(err)
		}
	} else {
		note(err)
	}
	return rc
}

// recapTotals sums one day. apps, when given, collects the apps that need a look.
func (r *Runner) recapTotals(ctx context.Context, rule api.AlertRule, from, to time.Time, apps *[]RecapApp, note func(error)) RecapTotals {
	var t RecapTotals
	if ov, err := r.store.Overview(ctx, rule.ProjectID, rule.Service, rule.Env, from, to); err == nil {
		t.Errors, t.People, t.Messages, t.Apps = ov.Errors, ov.AffectedUsers, ov.Events, ov.Services
	} else {
		note(err)
	}
	if health, err := r.store.ServiceHealth(ctx, rule.ProjectID, rule.Env, from, to, 50); err == nil {
		for _, a := range health {
			if rule.Service != "" && a.Service != rule.Service {
				continue
			}
			t.Requests += a.Requests
			t.FailedRequests += a.Failed
			if apps != nil && a.Status != "ok" && a.Status != "" && len(*apps) < recapListLen {
				*apps = append(*apps, RecapApp{Service: a.Service, Status: a.Status, FailedPct: a.FailedPct, Errors: a.Errors, LastSeen: a.LastSeen.UTC()})
			}
		}
	} else {
		note(err)
	}
	return t
}

func ruleName(rules map[string]api.AlertRule, id string) string {
	if r, ok := rules[id]; ok && strings.TrimSpace(r.Name) != "" {
		return strings.TrimSpace(r.Name)
	}
	return "A deleted rule"
}

func firstN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (r *Runner) overviewURL(from, to time.Time) string {
	if r.publicURL() == "" {
		return ""
	}
	v := url.Values{}
	v.Set("from", from.UTC().Format(time.RFC3339))
	v.Set("to", to.UTC().Format(time.RFC3339))
	return r.publicURL() + "/ui/overview?" + v.Encode()
}

// --- words ---

// recapHeadline: "Oct 11: 142 errors, up 20% from the day before. 3 new problems, 1 came back.
// 2.1% of requests failed." or, on a quiet day, "Oct 11 was quiet: no errors. 12,300 messages
// from 4 apps."
func recapHeadline(rc Recap) string {
	day := rc.Date
	if t, err := time.Parse("2006-01-02", rc.Date); err == nil {
		day = t.Format("Jan 2")
	}
	t := rc.Totals
	if t.Errors == 0 && t.NewProblems == 0 && t.CameBack == 0 {
		s := day + " was quiet: no errors."
		if t.Messages > 0 {
			s += fmt.Sprintf(" %s from %s.", plural(t.Messages, "message", "messages"), plural(t.Apps, "app", "apps"))
		}
		return s
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%s: %s, %s.", day, plural(t.Errors, "error", "errors"), changeWords(t.Errors, rc.Previous.Errors)))
	var probs []string
	if t.NewProblems > 0 {
		probs = append(probs, plural(uint64(t.NewProblems), "new problem", "new problems"))
	}
	if t.CameBack > 0 {
		probs = append(probs, fmt.Sprintf("%d came back", t.CameBack))
	}
	if len(probs) > 0 {
		s := strings.Join(probs, ", ") + "."
		parts = append(parts, strings.ToUpper(s[:1])+s[1:])
	}
	if t.Requests > 0 {
		parts = append(parts, fmt.Sprintf("%s%% of requests failed.", num(100*float64(t.FailedRequests)/float64(t.Requests))))
	}
	return strings.Join(parts, " ")
}

// changeWords compares a count with the day before: "up 20% from the day before".
func changeWords(cur, prev uint64) string {
	switch {
	case prev == 0 && cur == 0:
		return "none the day before either"
	case prev == 0:
		return "up from none the day before"
	case cur == prev:
		return "the same as the day before"
	case cur >= 2*prev:
		return "up from " + countWords(prev) + " the day before" // "up 2233%" is hard to picture
	}
	pct := 100 * (float64(cur) - float64(prev)) / float64(prev)
	if pct > 0 {
		return fmt.Sprintf("up %s%% from the day before", num(roundPct(pct)))
	}
	return fmt.Sprintf("down %s%% from the day before", num(roundPct(-pct)))
}

func roundPct(p float64) float64 {
	if p >= 10 {
		return float64(int(p + 0.5))
	}
	return float64(int(p*10+0.5)) / 10
}

// recapSections lays the recap lists out as titled lines.
func recapSections(rc Recap) []MessageSection {
	var out []MessageSection
	issues := func(title string, list []RecapIssue, total int) {
		if len(list) == 0 {
			return
		}
		s := MessageSection{Title: title}
		for _, is := range list {
			txt := ": " + timesWords(is.Count)
			if is.People > 0 {
				txt += " · " + plural(is.People, "person", "people")
			}
			s.Lines = append(s.Lines, MessageLine{Lead: clip(oneLine(is.Title), 160), Text: txt, URL: is.URL})
		}
		if total > len(list) {
			s.Lines = append(s.Lines, MessageLine{Text: fmt.Sprintf("…and %d more", total-len(list))})
		}
		out = append(out, s)
	}
	issues("New problems", rc.New, rc.Totals.NewProblems)
	issues("Came back", rc.CameBack, rc.Totals.CameBack)
	issues("Happened most", rc.Top, len(rc.Top))
	if len(rc.Apps) > 0 {
		s := MessageSection{Title: "Apps that need a look"}
		for _, a := range rc.Apps {
			s.Lines = append(s.Lines, MessageLine{Text: appWords(a)})
		}
		out = append(out, s)
	}
	if len(rc.Ongoing) > 0 {
		s := MessageSection{Title: "Still going on"}
		for _, o := range rc.Ongoing {
			s.Lines = append(s.Lines, MessageLine{Text: fmt.Sprintf("%s: %s, since %s", o.Rule, ruleKindWords(o.Kind), o.Since.UTC().Format("Jan 2 15:04 UTC"))})
		}
		out = append(out, s)
	}
	if len(rc.Alerts) > 0 {
		var parts []string
		for _, a := range rc.Alerts {
			parts = append(parts, fmt.Sprintf("%s: %d", a.Rule, a.Count))
		}
		out = append(out, MessageSection{Title: "Alerts sent", Lines: []MessageLine{{Text: strings.Join(parts, " · ")}}})
	}
	return out
}

func appWords(a RecapApp) string {
	switch a.Status {
	case "silent":
		if !a.LastSeen.IsZero() {
			return fmt.Sprintf("%s went silent: last heard from %s", a.Service, a.LastSeen.UTC().Format("Jan 2 15:04 UTC"))
		}
		return a.Service + " went silent"
	case "failing":
		return fmt.Sprintf("%s is failing: %s%% of requests failed", a.Service, num(a.FailedPct))
	}
	s := fmt.Sprintf("%s had trouble", a.Service)
	if a.FailedPct > 0 {
		s += fmt.Sprintf(": %s%% of requests failed", num(a.FailedPct))
	}
	if a.Errors > 0 {
		s += ", " + plural(a.Errors, "error", "errors")
	}
	return s
}

func ruleKindWords(kind string) string {
	switch kind {
	case "error_rate":
		return "too many errors"
	case "log_query":
		return "messages matching its search"
	}
	return strings.ReplaceAll(kind, "_", " ")
}
