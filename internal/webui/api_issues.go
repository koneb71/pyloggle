package webui

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/auth"
	"github.com/koneb71/pyloggle/internal/issue"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
	"github.com/koneb71/pyloggle/internal/timeparse"
)

func (u *UI) issueListQuery(r *http.Request) clickhouse.IssueListQuery {
	qs := r.URL.Query()
	q := clickhouse.IssueListQuery{
		ProjectID: auth.ProjectIDFromContext(r.Context()),
		Service:   qs.Get("service"),
		Env:       qs.Get("env"),
		Status:    strings.ToLower(strings.TrimSpace(qs.Get("status"))),
		Sort:      strings.ToLower(strings.TrimSpace(qs.Get("sort"))),
		Q:         qs.Get("q"),
		Release:   qs.Get("release"),
		Level:     qs.Get("level"),
		Assignee:  qs.Get("assignee"),
		Limit:     qInt(r, "limit", 50, 500),
		Offset:    qInt(r, "offset", 0, 100000),
	}
	if q.Assignee == "me" {
		q.Assignee = currentUser(r)
	}
	if qs.Get("from") != "" || qs.Get("to") != "" {
		q.From, q.To = qRange(r, 24*time.Hour)
	}
	return q
}

func (u *UI) apiIssues(w http.ResponseWriter, r *http.Request) {
	q := u.issueListQuery(r)
	items, err := u.store.QueryIssues(r.Context(), q)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.IssueSummary{}
	}
	counts, err := u.store.CountIssuesByStatus(r.Context(), q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if r.URL.Query().Get("sparklines") != "" && len(items) > 0 {
		ids := make([]string, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.IssueID)
		}
		if sp, err := u.store.IssueSparklines(r.Context(), q.ProjectID, ids, 24); err == nil {
			for i := range items {
				items[i].Sparkline = sp[items[i].IssueID]
			}
		}
	}
	writeJSON(w, http.StatusOK, api.IssueListResponse{Items: items, Counts: counts})
}

func (u *UI) apiIssue(w http.ResponseWriter, r *http.Request) {
	projectID := auth.ProjectIDFromContext(r.Context())
	id := r.PathValue("id")
	s, ok, err := u.store.GetIssueSummary(r.Context(), projectID, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "no such issue")
		return
	}
	from, to := qRange(r, 24*time.Hour)
	detail := api.IssueDetail{Issue: s, Breakdowns: map[string][]api.FacetCount{}}
	for _, field := range []string{"env", "release", "http_route", "hostname", "user", "handled"} {
		if fc, err := u.store.IssueBreakdown(r.Context(), projectID, id, field, from, to, 6); err == nil && len(fc) > 0 {
			detail.Breakdowns[field] = fc
		}
	}
	if sp, err := u.store.IssueSparklines(r.Context(), projectID, []string{id}, 24); err == nil {
		detail.Issue.Sparkline = sp[id]
	}
	bucket := int(clickhouse.NiceBucket(from, to, 60) / time.Second)
	if trend, err := u.store.IssueTrend(r.Context(), projectID, id, from, to, bucket); err == nil {
		detail.Trend = trend
	}
	if latest, ok, err := u.store.LatestOccurrence(r.Context(), projectID, id); err == nil && ok {
		detail.Latest = &latest
	}
	writeJSON(w, http.StatusOK, detail)
}

func (u *UI) apiIssueTrend(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	bucket := qInt(r, "bucket_seconds", 0, 7*86400)
	if bucket == 0 {
		bucket = int(clickhouse.NiceBucket(from, to, qInt(r, "buckets", 60, 500)) / time.Second)
	}
	pts, err := u.store.IssueTrend(r.Context(), auth.ProjectIDFromContext(r.Context()), r.PathValue("id"), from, to, bucket)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bucket_seconds": bucket, "points": pts})
}

func (u *UI) apiIssueBreakdown(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 30*24*time.Hour)
	field := strings.TrimSpace(r.URL.Query().Get("field"))
	if field == "" {
		field = "env"
	}
	fc, err := u.store.IssueBreakdown(r.Context(), auth.ProjectIDFromContext(r.Context()), r.PathValue("id"), field, from, to, qInt(r, "limit", 10, 100))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "query_failed", err.Error())
		return
	}
	if fc == nil {
		fc = []api.FacetCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"field": field, "items": fc})
}

func (u *UI) apiIssueOccurrences(w http.ResponseWriter, r *http.Request) {
	var from, to time.Time
	if r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != "" {
		from, to = qRange(r, 30*24*time.Hour)
	}
	before := qTime(r, "before")
	items, err := u.store.QueryOccurrences(r.Context(), auth.ProjectIDFromContext(r.Context()), r.PathValue("id"), from, to, before, qInt(r, "limit", 50, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.ErrorOccurrence{}
	}
	resp := map[string]any{"items": items}
	if n := len(items); n > 0 && n == qInt(r, "limit", 50, 500) {
		resp["next_before"] = items[n-1].TS
	}
	writeJSON(w, http.StatusOK, resp)
}

func (u *UI) apiIssueLatest(w http.ResponseWriter, r *http.Request) {
	occ, ok, err := u.store.LatestOccurrence(r.Context(), auth.ProjectIDFromContext(r.Context()), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "no occurrences")
		return
	}
	writeJSON(w, http.StatusOK, occ)
}

func (u *UI) apiOccurrence(w http.ResponseWriter, r *http.Request) {
	occ, ok, err := u.store.GetOccurrence(r.Context(), auth.ProjectIDFromContext(r.Context()), r.PathValue("event_id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "no such event")
		return
	}
	writeJSON(w, http.StatusOK, occ)
}

var validStatuses = map[string]bool{issue.StatusOpen: true, issue.StatusResolved: true, issue.StatusIgnored: true}

// stateBody is the JSON accepted by the state endpoints.
type stateBody struct {
	IssueIDs          []string `json:"issue_ids"` // bulk only
	Status            string   `json:"status"`
	Comment           string   `json:"comment"`
	SnoozeUntil       string   `json:"snooze_until"`        // RFC 3339 (ignored only)
	SnoozeForSeconds  int64    `json:"snooze_for_seconds"`  // alternative to snooze_until
	SnoozeCount       uint64   `json:"snooze_count"`        // N more occurrences (ignored only)
	ResolvedInRelease string   `json:"resolved_in_release"` // release or "next" (resolved only)
}

// changeFor turns a request body into a state change for one issue; it reads the issue's
// summary for "next" releases and count snoozes (which are stored as absolute targets).
func (u *UI) changeFor(r *http.Request, projectID, issueID string, body stateBody) (api.IssueStateChange, string, error) {
	status := strings.ToLower(strings.TrimSpace(body.Status))
	if !validStatuses[status] {
		return api.IssueStateChange{}, "bad_status", errors.New("status must be open, resolved or ignored")
	}
	ch := api.IssueStateChange{ProjectID: projectID, IssueID: issueID, Status: status, UpdatedBy: currentUser(r), Comment: body.Comment}
	summary, found, err := u.store.GetIssueSummary(r.Context(), projectID, issueID)
	if err != nil {
		return ch, "query_failed", err
	}
	if found {
		ch.Service, ch.Env = summary.Service, summary.Env
	}
	switch status {
	case issue.StatusIgnored:
		if body.SnoozeUntil != "" {
			t, ok := timeparse.Parse(body.SnoozeUntil)
			if !ok || !t.After(time.Now()) {
				return ch, "bad_snooze", errors.New("snooze_until must be a future timestamp")
			}
			t = t.UTC()
			ch.SnoozeUntil = &t
		} else if body.SnoozeForSeconds > 0 {
			t := time.Now().UTC().Add(time.Duration(body.SnoozeForSeconds) * time.Second)
			ch.SnoozeUntil = &t
		}
		if body.SnoozeCount > 0 {
			target := summary.OccurrenceCount + body.SnoozeCount
			ch.SnoozeCount = &target
		}
		if ch.SnoozeUntil != nil || ch.SnoozeCount != nil {
			ch.Kind = "snooze"
		}
	case issue.StatusResolved:
		rel := strings.TrimSpace(body.ResolvedInRelease)
		if rel == "next" {
			rel = summary.LastRelease
			if rel == "" {
				return ch, "no_release", errors.New("the issue has no release to resolve in; events must carry a release")
			}
		}
		if rel != "" {
			ch.ResolvedInRelease = &rel
			ch.Release = rel
			ch.Kind = "resolve_in_release"
		}
	}
	return ch, "", nil
}

// applyChange writes the change and refreshes the in-process state cache so ingest sees it.
func (u *UI) applyChange(r *http.Request, ch api.IssueStateChange) error {
	if err := u.store.SetIssueState(r.Context(), ch); err != nil {
		return err
	}
	if u.states != nil {
		if st, ok, err := u.store.GetIssueState(r.Context(), ch.ProjectID, ch.IssueID); err == nil && ok {
			u.states.Set(ch.ProjectID, ch.IssueID, st)
		}
	}
	return nil
}

// applyState is the pre-0.3 helper (status + comment); kept for the legacy admin endpoint path.
func (u *UI) applyState(r *http.Request, projectID, issueID, status, comment string) error {
	ch, code, err := u.changeFor(r, projectID, issueID, stateBody{Status: status, Comment: comment})
	if err != nil {
		return fmt.Errorf("%s: %w", code, err)
	}
	return u.applyChange(r, ch)
}

func (u *UI) apiIssueState(w http.ResponseWriter, r *http.Request) {
	var body stateBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	ch, code, err := u.changeFor(r, auth.ProjectIDFromContext(r.Context()), r.PathValue("id"), body)
	if err != nil {
		status := http.StatusBadRequest
		if code == "query_failed" {
			status = http.StatusInternalServerError
		}
		writeErr(w, status, code, err.Error())
		return
	}
	if err := u.applyChange(r, ch); err != nil {
		writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": ch.Status})
}

func (u *UI) apiIssuesBulkState(w http.ResponseWriter, r *http.Request) {
	var body stateBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	status := strings.ToLower(strings.TrimSpace(body.Status))
	if !validStatuses[status] || len(body.IssueIDs) == 0 || len(body.IssueIDs) > 200 {
		writeErr(w, http.StatusBadRequest, "bad_request", "status must be open/resolved/ignored and 1..200 issue_ids are required")
		return
	}
	projectID := auth.ProjectIDFromContext(r.Context())
	for _, id := range body.IssueIDs {
		ch, code, err := u.changeFor(r, projectID, id, body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, code, id+": "+err.Error())
			return
		}
		if err := u.applyChange(r, ch); err != nil {
			writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated": len(body.IssueIDs)})
}

func (u *UI) apiIssueAssign(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Assignee string `json:"assignee"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	who := strings.TrimSpace(body.Assignee)
	if who == "me" {
		who = currentUser(r)
	}
	if len(who) > 200 {
		writeErr(w, http.StatusBadRequest, "bad_assignee", "assignee is too long")
		return
	}
	projectID := auth.ProjectIDFromContext(r.Context())
	ch := api.IssueStateChange{ProjectID: projectID, IssueID: r.PathValue("id"), UpdatedBy: currentUser(r), Assignee: &who, Kind: "assign"}
	if s, ok, err := u.store.GetIssueSummary(r.Context(), projectID, ch.IssueID); err == nil && ok {
		ch.Service, ch.Env = s.Service, s.Env
	}
	if err := u.applyChange(r, ch); err != nil {
		writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "assignee": who})
}

func (u *UI) apiIssueComment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	text := strings.TrimSpace(body.Body)
	if text == "" || len(text) > 20000 {
		writeErr(w, http.StatusBadRequest, "bad_comment", "comment body must be 1..20000 characters")
		return
	}
	if err := u.store.AddIssueComment(r.Context(), auth.ProjectIDFromContext(r.Context()), r.PathValue("id"), currentUser(r), text); err != nil {
		writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (u *UI) apiIssueActivity(w http.ResponseWriter, r *http.Request) {
	items, err := u.store.ListIssueActivity(r.Context(), auth.ProjectIDFromContext(r.Context()), r.PathValue("id"), qInt(r, "limit", 100, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.IssueActivity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (u *UI) apiIssueAssignees(w http.ResponseWriter, r *http.Request) {
	items, err := u.store.ListAssignees(r.Context(), auth.ProjectIDFromContext(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []string{}
	}
	me := currentUser(r)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "me": me})
}
