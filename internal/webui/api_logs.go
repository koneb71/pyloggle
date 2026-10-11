package webui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/auth"
	"pyloggle/internal/logquery"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/timeparse"
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
	known := false
	for _, m := range clickhouse.AggregateMetrics {
		known = known || m == spec.Metric
	}
	if !known {
		writeErr(w, http.StatusBadRequest, "bad_metric", "metric must be one of "+strings.Join(clickhouse.AggregateMetrics, ", "))
		return
	}
	if spec.Metric != "count" && spec.Of == "" {
		writeErr(w, http.StatusBadRequest, "bad_metric", "metric "+spec.Metric+" needs a field (of=duration_ms, of=http.status, of=attrs.<key>)")
		return
	}
	if secs := qInt(r, "bucket_seconds", 0, 7*86400); secs > 0 {
		spec.Bucket = time.Duration(secs) * time.Second
	} else {
		spec.Bucket = clickhouse.NiceBucket(lq.From, lq.To, qInt(r, "buckets", 60, 500))
	}
	agg, err := u.store.LogAggregate(r.Context(), lq, spec)
	if err != nil {
		if strings.Contains(err.Error(), "not numeric") || strings.Contains(err.Error(), "needs a field") {
			writeErr(w, http.StatusBadRequest, "bad_metric", err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
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
	items, err := u.store.ListSavedSearches(r.Context(), auth.ProjectIDFromContext(r.Context()), currentUser(r), true, false, qInt(r, "limit", 100, 500))
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
	if body.UserID == "" {
		body.UserID = currentUser(r)
	}
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "missing_name", "name is required")
		return
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
	if err := u.store.SoftDeleteSavedSearch(r.Context(), auth.ProjectIDFromContext(r.Context()), currentUser(r), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete_failed", fmt.Sprintf("delete %s: %v", id, err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
