package webui

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/auth"
	"github.com/koneb71/pyloggle/internal/logquery"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
	"github.com/koneb71/pyloggle/internal/timeparse"
)

// logQueryFromRequest builds the storage query from the explorer parameters:
// q (query language), from/to, plus simple filters that the SPA may pass as separate params.
func (u *UI) logQueryFromRequest(r *http.Request, defRange time.Duration) (clickhouse.LogQuery, error) {
	qs := r.URL.Query()
	from, to := qRange(r, defRange)
	lq := clickhouse.LogQuery{
		ProjectID: auth.ProjectIDFromContext(r.Context()),
		From:      from,
		To:        to,
		Service:   qs.Get("service"),
		Env:       qs.Get("env"),
		Logger:    qs.Get("logger"),
		TraceID:   qs.Get("trace_id"),
		RequestID: qs.Get("request_id"),
		IssueID:   qs.Get("issue_id"),
		Release:   qs.Get("release"),
		Hostname:  qs.Get("hostname"),
		Levels:    qStrings(r, "level"),
	}
	if text := strings.TrimSpace(qs.Get("text")); text != "" {
		lq.Text = text
	}
	if raw := strings.TrimSpace(qs.Get("q")); raw != "" {
		parsed, err := logquery.Parse(raw)
		if err != nil {
			return lq, err
		}
		where, args, err := parsed.SQL()
		if err != nil {
			return lq, err
		}
		lq.Where, lq.Args = where, args
	}
	return lq, nil
}

// cursor encodes the keyset position as "<unix millis>_<event_id>".
func encodeCursor(ev api.LogEvent) string {
	return strconv.FormatInt(ev.TS.UnixMilli(), 10) + "_" + ev.EventID
}

func decodeCursor(s string) (time.Time, string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, "", false
	}
	i := strings.IndexByte(s, '_')
	if i <= 0 {
		return time.Time{}, "", false
	}
	ms, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return time.Time{}, "", false
	}
	return time.UnixMilli(ms).UTC(), s[i+1:], true
}

func (u *UI) apiLogs(w http.ResponseWriter, r *http.Request) {
	lq, err := u.logQueryFromRequest(r, time.Hour)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	lq.Limit = qInt(r, "limit", 200, 1000)
	if ts, id, ok := decodeCursor(r.URL.Query().Get("cursor")); ok {
		lq.BeforeTS, lq.BeforeID = ts, id
	}
	if after := strings.TrimSpace(r.URL.Query().Get("after")); after != "" {
		if t, ok := timeparse.Parse(after); ok {
			lq.After = t
			lq.Ascending = true
		}
	}
	if strings.EqualFold(r.URL.Query().Get("order"), "asc") {
		lq.Ascending = true
	}
	items, err := u.store.QueryLogs(r.Context(), lq)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.LogEvent{}
	}
	resp := map[string]any{"items": items, "from": lq.From, "to": lq.To}
	if len(items) == lq.Limit && !lq.Ascending {
		resp["next_cursor"] = encodeCursor(items[len(items)-1])
	}
	if len(items) > 0 {
		newest := items[0]
		if lq.Ascending {
			newest = items[len(items)-1]
		}
		resp["newest_ts"] = newest.TS
	}
	writeJSON(w, http.StatusOK, resp)
}

func (u *UI) apiLogsHistogram(w http.ResponseWriter, r *http.Request) {
	lq, err := u.logQueryFromRequest(r, time.Hour)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	groupBy := strings.TrimSpace(r.URL.Query().Get("group_by"))
	if groupBy == "" {
		groupBy = "level"
	}
	var bucket time.Duration
	if secs := qInt(r, "bucket_seconds", 0, 7*86400); secs > 0 {
		bucket = time.Duration(secs) * time.Second
	} else {
		bucket = clickhouse.NiceBucket(lq.From, lq.To, qInt(r, "buckets", 80, 500))
	}
	h, err := u.store.LogHistogram(r.Context(), lq, groupBy, bucket)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// apiLogsAggregate is the analytics view: metric over a field, split by a group-by, bucketed.
func (u *UI) apiLogsAggregate(w http.ResponseWriter, r *http.Request) {
	lq, err := u.logQueryFromRequest(r, time.Hour)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	spec, ok := aggregateSpec(w, r, "messages", lq.From, lq.To)
	if !ok {
		return
	}
	agg, err := u.store.LogAggregate(r.Context(), lq, spec)
	writeAggregate(w, agg, err)
}

// aggregateSpec reads a chart's settings: metric (checked against the source's), of, group_by,
// limit, sort and the bucket (bucket_seconds, or buckets across the range).
func aggregateSpec(w http.ResponseWriter, r *http.Request, source string, from, to time.Time) (clickhouse.AggregateSpec, bool) {
	qs := r.URL.Query()
	spec := clickhouse.AggregateSpec{
		GroupBy: strings.TrimSpace(qs.Get("group_by")),
		Metric:  strings.ToLower(strings.TrimSpace(qs.Get("metric"))),
		Of:      strings.TrimSpace(qs.Get("of")),
		Limit:   qInt(r, "limit", 10, 50),
		Sort:    strings.TrimSpace(qs.Get("sort")),
	}
	if spec.Metric == "" {
		spec.Metric = "count"
	}
	metrics := clickhouse.ChartMetrics[source]
	known := false
	for _, m := range metrics {
		known = known || m == spec.Metric
	}
	if !known {
		writeErr(w, http.StatusBadRequest, "bad_metric", "metric must be one of "+strings.Join(metrics, ", "))
		return spec, false
	}
	if needsField(source, spec.Metric) && spec.Of == "" {
		writeErr(w, http.StatusBadRequest, "bad_metric", "metric "+spec.Metric+" needs a field (of=duration_ms, of=http.status, of=attrs.<key>)")
		return spec, false
	}
	if secs := qInt(r, "bucket_seconds", 0, 7*86400); secs > 0 {
		spec.Bucket = time.Duration(secs) * time.Second
	} else {
		spec.Bucket = clickhouse.NiceBucket(from, to, qInt(r, "buckets", 60, 500))
	}
	return spec, true
}

// needsField reports a metric that measures a field: everything but the counts, and for
// requests nothing (their response time is built in).
func needsField(source, metric string) bool {
	if source == "requests" {
		return false
	}
	switch metric {
	case "count", "people", "problems":
		return false
	}
	return true
}

func writeAggregate(w http.ResponseWriter, agg api.LogAggregate, err error) {
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not numeric") || strings.Contains(msg, "needs a field") || strings.Contains(msg, "cannot be") ||
			strings.Contains(msg, "measure their") || strings.Contains(msg, "are counted over") {
			writeErr(w, http.StatusBadRequest, "bad_metric", msg)
			return
		}
		writeErr(w, http.StatusInternalServerError, "query_failed", msg)
		return
	}
	writeJSON(w, http.StatusOK, agg)
}

func (u *UI) apiLogsFacets(w http.ResponseWriter, r *http.Request) {
	lq, err := u.logQueryFromRequest(r, time.Hour)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	fields := qStrings(r, "fields")
	f, err := u.store.LogFacets(r.Context(), lq, fields, qInt(r, "attr_keys", 30, 200), strings.TrimSpace(r.URL.Query().Get("key")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if f.Fields == nil {
		f.Fields = map[string][]api.FacetCount{}
	}
	writeJSON(w, http.StatusOK, f)
}

// apiLogsClear deletes every message of the current project, with the counts and request
// statistics built from them. The project is the one the request is for (the session's, or
// the access key's), never one named in the URL, and the body must repeat its id.
func (u *UI) apiLogsClear(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	projectID := auth.ProjectIDFromContext(r.Context())
	var body struct {
		Confirm string `json:"confirm"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if projectID == "" || strings.TrimSpace(body.Confirm) != projectID {
		writeErr(w, http.StatusBadRequest, "confirm_mismatch", "confirm must be the project id ("+projectID+")")
		return
	}
	// Not tied to the browser tab: a closed page must not stop the deletes halfway.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	defer cancel()
	if err := u.store.ClearLogs(ctx, projectID); err != nil {
		writeErr(w, http.StatusInternalServerError, "clear_failed", err.Error())
		return
	}
	u.audit(r, "logs.clear", projectID, "", "")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_ = u.store.RecomputeUsage(ctx, projectID)
	}()
	writeJSON(w, http.StatusOK, map[string]any{"status": "cleared", "id": projectID})
}

// apiLog returns one message with its request body.
func (u *UI) apiLog(w http.ResponseWriter, r *http.Request) {
	ev, ok, err := u.store.GetLog(r.Context(), auth.ProjectIDFromContext(r.Context()), strings.TrimSpace(r.PathValue("event_id")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "no such event")
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (u *UI) apiLogsContext(w http.ResponseWriter, r *http.Request) {
	eventID := strings.TrimSpace(r.URL.Query().Get("event_id"))
	if eventID == "" {
		writeErr(w, http.StatusBadRequest, "missing_event_id", "event_id is required")
		return
	}
	lc, ok, err := u.store.LogContext(r.Context(), auth.ProjectIDFromContext(r.Context()), eventID, qInt(r, "before", 50, 500), qInt(r, "after", 50, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "no such event")
		return
	}
	if lc.Before == nil {
		lc.Before = []api.LogEvent{}
	}
	if lc.After == nil {
		lc.After = []api.LogEvent{}
	}
	writeJSON(w, http.StatusOK, lc)
}

func (u *UI) apiLogsPatterns(w http.ResponseWriter, r *http.Request) {
	lq, err := u.logQueryFromRequest(r, time.Hour)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	items, err := u.store.LogPatterns(r.Context(), lq, qInt(r, "limit", 50, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.LogPattern{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (u *UI) apiLogsFields(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"fields":          logquery.Fields(),
		"facets":          clickhouse.DefaultFacetFields,
		"histogram_group": []string{"level", "service", "env", "logger", "http_status", "hostname", "release", "source"},
		"syntax": []string{
			"service:api env:prod            exact field match (terms are ANDed)",
			"level:(error OR warn)           one of several values",
			"-logger:werkzeug                exclude",
			"\"timed out\"  boom*             message text (quoted phrase, wildcard)",
			"http.status:>=500 duration:>250 numeric comparisons",
			"route:/orders*                  prefix / wildcard",
			"@user_id:42  tags.team:core     attributes and tags; attrs.key:* = key present",
			"service:api OR service:web      OR between terms",
		},
	})
}

// Saved views reuse saved_searches; Query holds the explorer state as JSON.
func (u *UI) apiSavedViews(w http.ResponseWriter, r *http.Request) {
	pinned := r.URL.Query().Get("pinned") == "1" || r.URL.Query().Get("pinned") == "true"
	items, err := u.store.ListSavedSearches(r.Context(), auth.ProjectIDFromContext(r.Context()), currentUser(r), true, pinned, qInt(r, "limit", 100, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.SavedSearch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (u *UI) apiSavedViewUpsert(w http.ResponseWriter, r *http.Request) {
	var body api.SavedSearch
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	body.ProjectID = auth.ProjectIDFromContext(r.Context())
	body.UserID = currentUser(r)
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "missing_name", "name is required")
		return
	}
	if body.ID != "" {
		// Saving over a view keeps its owner, and only the owner or an admin may do it.
		cur, ok := u.ownedView(w, r, body.ID)
		if !ok {
			return
		}
		body.UserID, body.CreatedAt = cur.UserID, cur.CreatedAt
	}
	saved, err := u.store.UpsertSavedSearch(r.Context(), body)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"view": saved})
}

func (u *UI) apiSavedViewDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := u.ownedView(w, r, id); !ok {
		return
	}
	if err := u.store.SoftDeleteSavedSearch(r.Context(), auth.ProjectIDFromContext(r.Context()), currentUser(r), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete_failed", fmt.Sprintf("delete %s: %v", id, err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiSavedViewPin pins a saved chart to Dashboards, or unpins it ({"pinned": bool}). A shared
// chart pinned shows on everyone's Dashboards, so only its owner or an admin may.
func (u *UI) apiSavedViewPin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	cur, ok := u.ownedView(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	cur.IsPinned = body.Pinned
	saved, err := u.store.UpsertSavedSearch(r.Context(), cur)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"view": saved})
}

// ownedView loads a saved view the caller may change: their own, or any as an admin. Another
// person's private view is not found.
func (u *UI) ownedView(w http.ResponseWriter, r *http.Request, id string) (api.SavedSearch, bool) {
	cur, ok, err := u.store.GetSavedSearch(r.Context(), auth.ProjectIDFromContext(r.Context()), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return cur, false
	}
	me := currentUser(r)
	if !ok || (!cur.IsPublic && cur.UserID != me && !u.isUIAdmin(r)) {
		writeErr(w, http.StatusNotFound, "not_found", "no such saved view")
		return cur, false
	}
	if cur.UserID != me && !u.isUIAdmin(r) {
		writeErr(w, http.StatusForbidden, "not_owner", "only "+cur.UserID+" or an admin can change this view")
		return cur, false
	}
	return cur, true
}
