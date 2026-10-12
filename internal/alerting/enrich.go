package alerting

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/logquery"
	"pyloggle/internal/metrics"
	"pyloggle/internal/model"
	"pyloggle/internal/storage/clickhouse"
)

// IssueContext is one problem behind a firing, told through the latest time it happened in
// the window. It carries counts, never who: no user ids, e-mails, request URLs or assignees.
type IssueContext struct {
	IssueID   string    `json:"issue_id"`
	Title     string    `json:"title"`
	Reason    string    `json:"reason,omitempty"` // jumped | new | came_back | most_frequent
	Count     uint64    `json:"count"`
	PrevCount uint64    `json:"prev_count,omitempty"`
	Users     uint64    `json:"users,omitempty"`
	Service   string    `json:"service,omitempty"`
	Env       string    `json:"env,omitempty"`
	Release   string    `json:"release,omitempty"`
	Where     string    `json:"where,omitempty"`   // "app/gateway.py, line 41, in call"
	Code      string    `json:"code,omitempty"`    // the line that failed
	Request   string    `json:"request,omitempty"` // "POST /pay → 500": the route, never the URL
	EventID   string    `json:"event_id,omitempty"`
	At        time.Time `json:"at,omitzero"`
	URL       string    `json:"url,omitempty"`       // this time, in Pyloggle
	LogsURL   string    `json:"logs_url,omitempty"`  // messages from the same request
	TraceURL  string    `json:"trace_url,omitempty"` // the request timeline, only when it was stored

	// Steps are the last breadcrumbs before the error. They are free text written by the app
	// (and may hold URLs), so they go to the people the rule notifies, not into the payload.
	Steps []string `json:"-"`

	traceID string
}

// LogSample is one line that matched a log_query rule.
type LogSample struct {
	TS      time.Time `json:"ts"`
	Level   string    `json:"level"`
	Service string    `json:"service,omitempty"`
	Env     string    `json:"env,omitempty"`
	Message string    `json:"message"`
}

const (
	detailedIssues = 3 // issues that get their latest occurrence looked up
	sampleLines    = 3
	maxStepLen     = 160
	maxCodeLen     = 160
	maxSampleLen   = 300
)

// enrich adds context to a firing: who/what/where for the first few issues, or sample lines
// for a log monitor. It never blocks delivery for long and never fails it: the work gets at
// most enrichBudget (less when the evaluation pass is close to its own deadline), a panic is
// recovered, and whatever could not be looked up is simply left out.
func (r *Runner) enrich(ctx context.Context, ev *AlertEvent) {
	budget := r.enrichBudget
	if dl, ok := ctx.Deadline(); ok {
		if half := time.Until(dl) / 2; half < budget {
			budget = half
		}
	}
	if budget <= 0 {
		metrics.AlertEnrichFailure.Inc("timeout")
		return
	}
	ectx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			metrics.AlertEnrichFailure.Inc("panic")
			log.Printf("alerting: rule=%s: adding context panicked, sending without it: %v", ev.Rule.ID, p)
		}
	}()

	var failed error
	note := func(err error) {
		if err != nil && failed == nil {
			failed = err
		}
	}
	var top []IssueContext
	var samples []LogSample
	switch ev.Kind {
	case "error_rate":
		top = r.topIssuesForErrorRate(ectx, *ev, note)
	case "issue_spike":
		for _, s := range ev.Spikes {
			top = append(top, IssueContext{IssueID: s.IssueID, Title: s.Title, Reason: "jumped", Count: s.CurCount, PrevCount: s.PrevCount, Users: s.Users})
		}
	case "new_issue", "regression":
		reason := "new"
		if ev.Kind == "regression" {
			reason = "came_back"
		}
		for _, is := range ev.Issues {
			top = append(top, IssueContext{IssueID: is.IssueID, Title: is.Title, Reason: reason, Count: is.Count, Users: is.Users})
		}
	case "log_query":
		samples = r.sampleLines(ectx, *ev, note)
	}
	if len(top) > detailedIssues {
		top = top[:detailedIssues]
	}
	for i := range top {
		r.describeIssue(ectx, *ev, &top[i], note)
	}
	r.linkTimelines(ectx, *ev, top, note)

	ev.TopIssues, ev.Samples = top, samples
	if failed != nil {
		reason := "query"
		if errors.Is(failed, context.DeadlineExceeded) || errors.Is(ectx.Err(), context.DeadlineExceeded) {
			reason = "timeout"
		}
		metrics.AlertEnrichFailure.Inc(reason)
		log.Printf("alerting: rule=%s: some context could not be added (%s): %v", ev.Rule.ID, reason, failed)
	}
}

// topIssuesForErrorRate names the issues behind an error-rate firing, with the window before
// for comparison.
func (r *Runner) topIssuesForErrorRate(ctx context.Context, ev AlertEvent, note func(error)) []IssueContext {
	w := ev.WindowEnd.Sub(ev.WindowStart)
	rows, err := r.store.IssueSpikeCounts(ctx, ev.Rule, ev.WindowStart.Add(-w), ev.WindowStart, ev.WindowStart, ev.WindowEnd, detailedIssues)
	note(err)
	out := make([]IssueContext, 0, len(rows))
	for _, row := range rows {
		out = append(out, IssueContext{IssueID: row.IssueID, Title: row.Title, Reason: "most_frequent", Count: row.CurCount, PrevCount: row.PrevCount, Users: row.CurUsers})
	}
	return out
}

// describeIssue fills one issue from the latest time it happened in the window.
func (r *Runner) describeIssue(ctx context.Context, ev AlertEvent, ic *IssueContext, note func(error)) {
	ic.URL = r.issueURL(ic.IssueID)
	if ctx.Err() != nil {
		return
	}
	occs, err := r.store.QueryOccurrences(ctx, ev.Rule.ProjectID, ic.IssueID, ev.WindowStart, ev.WindowEnd, time.Time{}, 1)
	note(err)
	if len(occs) == 0 {
		return
	}
	o := occs[0]
	ic.EventID, ic.At = o.EventID, o.TS.UTC()
	ic.Service, ic.Env, ic.Release = o.Service, o.Env, o.Release
	if ic.Title == "" {
		ic.Title = o.Title
	}
	if f, ok := crashFrame(o.Frames); ok {
		ic.Where = frameWords(f)
		ic.Code = clip(strings.TrimSpace(f.Context), maxCodeLen)
	}
	ic.Request = requestWords(o)
	ic.Steps = stepWords(o.Breadcrumbs, 3)
	if ic.URL != "" && o.EventID != "" {
		ic.URL += "?event=" + url.QueryEscape(o.EventID)
	}
	switch {
	case o.RequestID != "":
		ic.LogsURL = r.logsURL("request:"+quoteValue(o.RequestID), o.TS.Add(-15*time.Minute), o.TS.Add(5*time.Minute))
	case o.TraceID != "":
		ic.LogsURL = r.logsURL("trace:"+quoteValue(o.TraceID), o.TS.Add(-15*time.Minute), o.TS.Add(5*time.Minute))
	}
	ic.traceID = o.TraceID
}

// linkTimelines adds request-timeline links, in one query, for the traces that were stored.
func (r *Runner) linkTimelines(ctx context.Context, ev AlertEvent, top []IssueContext, note func(error)) {
	if r.publicURL() == "" || ctx.Err() != nil {
		return
	}
	var ids []string
	for _, ic := range top {
		if ic.traceID != "" {
			ids = append(ids, ic.traceID)
		}
	}
	if len(ids) == 0 {
		return
	}
	found, err := r.store.TracesWithSpans(ctx, ev.Rule.ProjectID, ids, ev.WindowStart.Add(-time.Hour), ev.WindowEnd.Add(time.Hour))
	note(err)
	for i := range top {
		if t := top[i].traceID; t != "" && found[t] {
			v := url.Values{}
			v.Set("at", top[i].At.Format(time.RFC3339))
			top[i].TraceURL = r.publicURL() + "/ui/traces/" + url.PathEscape(t) + "?" + v.Encode()
		}
	}
}

// sampleLines returns the newest lines that matched a log_query rule in the window.
func (r *Runner) sampleLines(ctx context.Context, ev AlertEvent, note func(error)) []LogSample {
	if ev.Count == 0 {
		return nil
	}
	parsed, err := logquery.Parse(ev.Rule.Query)
	if err != nil {
		return nil
	}
	where, args, err := parsed.SQL()
	if err != nil {
		return nil
	}
	rule := ev.Rule
	rows, err := r.store.QueryLogs(ctx, clickhouse.LogQuery{ProjectID: rule.ProjectID, From: ev.WindowStart, To: ev.WindowEnd, Where: where, Args: args,
		Service: rule.Service, Env: rule.Env, HTTPRoute: rule.HTTPRoute, HTTPMethod: rule.HTTPMethod, Limit: sampleLines})
	note(err)
	out := make([]LogSample, 0, len(rows))
	for _, l := range rows {
		out = append(out, LogSample{TS: l.TS.UTC(), Level: l.Level, Service: l.Service, Env: l.Env, Message: clip(oneLine(l.Message), maxSampleLen)})
		if len(out) == sampleLines {
			break
		}
	}
	return out
}

// crashFrame is where the error was raised in the app's own code: the last in-app frame
// (Python tracebacks run oldest call first), else the last frame.
func crashFrame(frames []model.Frame) (model.Frame, bool) {
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].InApp != nil && *frames[i].InApp {
			return frames[i], true
		}
	}
	if len(frames) > 0 {
		return frames[len(frames)-1], true
	}
	return model.Frame{}, false
}

func frameWords(f model.Frame) string {
	file := f.Filename
	if file == "" {
		file = f.AbsPath
	}
	if file == "" {
		file = f.Module
	}
	var parts []string
	if file != "" {
		parts = append(parts, file)
	}
	if f.Lineno > 0 {
		parts = append(parts, fmt.Sprintf("line %d", f.Lineno))
	}
	if f.Function != "" {
		parts = append(parts, "in "+f.Function)
	}
	return strings.Join(parts, ", ")
}

// requestWords is "POST /pay → 500" from the route template (never the URL, which can carry
// ids and tokens).
func requestWords(o api.ErrorOccurrence) string {
	method, route, status := o.HTTPMethod, o.HTTPRoute, o.HTTPStatus
	if o.HTTP != nil {
		if method == "" {
			method = o.HTTP.Method
		}
		if route == "" {
			route = o.HTTP.Route
		}
		if status == 0 {
			status = o.HTTP.Status
		}
	}
	s := strings.TrimSpace(method + " " + route)
	if s == "" {
		return ""
	}
	if status > 0 {
		s += fmt.Sprintf(" → %d", status)
	}
	return s
}

var urlQuery = regexp.MustCompile(`(https?://[^\s?#]+)[?#][^\s]*`)

// stepWords renders the last n breadcrumbs as "12:01:03 http POST https://psp/charge (402)",
// dropping query strings from URLs.
func stepWords(crumbs []model.Breadcrumb, n int) []string {
	if len(crumbs) > n {
		crumbs = crumbs[len(crumbs)-n:]
	}
	out := make([]string, 0, len(crumbs))
	for _, c := range crumbs {
		msg := c.Message
		if msg == "" && c.Data != nil {
			if m, u := c.Data["method"], c.Data["url"]; u != "" {
				msg = strings.TrimSpace(m + " " + u)
				if st := c.Data["status_code"]; st != "" {
					msg += " (" + st + ")"
				}
			}
		}
		msg = urlQuery.ReplaceAllString(oneLine(msg), "$1")
		if msg == "" {
			continue
		}
		parts := []string{}
		if t := c.TS.Time; !t.IsZero() {
			parts = append(parts, t.UTC().Format("15:04:05"))
		}
		if label := stepLabel(c); label != "" {
			parts = append(parts, label)
		}
		out = append(out, clip(strings.Join(append(parts, msg), " · "), maxStepLen))
	}
	return out
}

// stepLabel names what kind of step a breadcrumb was: "HTTP", "Database", or the logger.
func stepLabel(c model.Breadcrumb) string {
	cat := strings.ToLower(c.Category)
	switch {
	case c.Type == "http" || strings.HasPrefix(cat, "http"):
		return "HTTP"
	case c.Type == "query" || cat == "db" || cat == "sql" || cat == "query" || strings.HasPrefix(cat, "db."):
		return "Database"
	case c.Category != "":
		return c.Category
	}
	return c.Type
}

func quoteValue(v string) string {
	if strings.ContainsAny(v, " \"'():") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// clip shortens s to at most n characters (runes), ending with an ellipsis when cut.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
