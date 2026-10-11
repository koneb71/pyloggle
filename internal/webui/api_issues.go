package webui

import (
	"net/http"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/auth"
	"pyloggle/internal/issue"
	"pyloggle/internal/storage/clickhouse"
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
		Limit:     qInt(r, "limit", 50, 500),
		Offset:    qInt(r, "offset", 0, 100000),
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

func (u *UI) applyState(r *http.Request, projectID, issueID, status, comment string) error {
	st := api.IssueStateChange{ProjectID: projectID, IssueID: issueID, Status: status, UpdatedBy: currentUser(r), Comment: comment}
	if s, ok, err := u.store.GetIssueSummary(r.Context(), projectID, issueID); err == nil && ok {
		st.Service, st.Env = s.Service, s.Env
	}
	if err := u.store.SetIssueState(r.Context(), st); err != nil {
		return err
	}
	if u.states != nil {
		u.states.Set(projectID, issueID, issue.State{Status: status})
	}
	return nil
}

func (u *UI) apiIssueState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	status := strings.ToLower(strings.TrimSpace(body.Status))
	if !validStatuses[status] {
		writeErr(w, http.StatusBadRequest, "bad_status", "status must be open, resolved or ignored")
		return
	}
	if err := u.applyState(r, auth.ProjectIDFromContext(r.Context()), r.PathValue("id"), status, body.Comment); err != nil {
		writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (u *UI) apiIssuesBulkState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IssueIDs []string `json:"issue_ids"`
		Status   string   `json:"status"`
		Comment  string   `json:"comment"`
	}
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
		if err := u.applyState(r, projectID, id, status, body.Comment); err != nil {
			writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated": len(body.IssueIDs)})
}
