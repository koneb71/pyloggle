package webui

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/auth"
	"pyloggle/internal/logquery"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/timeparse"
)

// spanQueryFromRequest parses q (span schema), from/to and the keyset cursor.
func (u *UI) spanQueryFromRequest(r *http.Request) (clickhouse.SpanQuery, error) {
	from, to := qRange(r, time.Hour)
	sq := clickhouse.SpanQuery{ProjectID: auth.ProjectIDFromContext(r.Context()), From: from, To: to, Limit: qInt(r, "limit", 50, 500)}
	if raw := strings.TrimSpace(r.URL.Query().Get("q")); raw != "" {
		parsed, err := logquery.Parse(raw)
		if err != nil {
			return sq, err
		}
		where, args, err := parsed.SQLFor(logquery.Spans)
		if err != nil {
			return sq, err
		}
		sq.Where, sq.Args = where, args
	}
	if ts, id, ok := decodeTraceCursor(r.URL.Query().Get("cursor")); ok {
		sq.BeforeTS, sq.BeforeID = ts, id
	}
	return sq, nil
}

// The traces cursor is "<unix micros>_<trace_id>".
func encodeTraceCursor(t api.TraceSummary) string {
	return strconv.FormatInt(t.StartTS.UnixMicro(), 10) + "_" + t.TraceID
}

func decodeTraceCursor(s string) (time.Time, string, bool) {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, '_')
	if i <= 0 {
		return time.Time{}, "", false
	}
	us, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return time.Time{}, "", false
	}
	return time.UnixMicro(us).UTC(), s[i+1:], true
}

func (u *UI) apiTraces(w http.ResponseWriter, r *http.Request) {
	sq, err := u.spanQueryFromRequest(r)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	items, err := u.store.TraceList(r.Context(), sq)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.TraceSummary{}
	}
	resp := map[string]any{"items": items, "from": sq.From, "to": sq.To}
	if len(items) == sq.Limit {
		resp["next_cursor"] = encodeTraceCursor(items[len(items)-1])
	}
	if strings.Contains(r.URL.Query().Get("include"), "stats") {
		st, err := u.store.TraceStats(r.Context(), sq)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}
		resp["stats"] = st
	}
	writeJSON(w, http.StatusOK, resp)
}

// apiTrace returns one trace: spans, error occurrences and the log lines carrying the id.
func (u *UI) apiTrace(w http.ResponseWriter, r *http.Request) {
	traceID := strings.TrimSpace(r.PathValue("trace_id"))
	if traceID == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "missing trace_id")
		return
	}
	projectID := auth.ProjectIDFromContext(r.Context())
	at, _ := timeparse.Parse(r.URL.Query().Get("at"))
	spans, err := u.store.TraceSpans(r.Context(), projectID, traceID, at)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if len(spans) == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "no spans stored for this trace (spans keep "+strconv.Itoa(u.cfg.RetentionSpansDays)+" days)")
		return
	}
	d := api.TraceDetail{TraceID: traceID, Spans: spans, Errors: []api.ErrorOccurrence{}, Logs: []api.LogEvent{}}
	start, end := spans[0].StartTS, spans[0].StartTS
	seen := map[string]bool{}
	for _, sp := range spans {
		if sp.StartTS.Before(start) {
			start = sp.StartTS
		}
		if e := sp.StartTS.Add(time.Duration(sp.DurationUS) * time.Microsecond); e.After(end) {
			end = e
		}
		if sp.Service != "" && !seen[sp.Service] {
			seen[sp.Service] = true
			d.Services = append(d.Services, sp.Service)
		}
	}
	sort.Strings(d.Services)
	d.StartTS = start
	d.DurationUS = uint64(end.Sub(start) / time.Microsecond)
	if errs, err := u.store.TraceErrors(r.Context(), projectID, traceID, start); err == nil && errs != nil {
		d.Errors = errs
	}
	logs, err := u.store.QueryLogs(r.Context(), clickhouse.LogQuery{ProjectID: projectID, TraceID: traceID, From: start.Add(-5 * time.Minute), To: end.Add(5 * time.Minute), Ascending: true, Limit: 500})
	if err == nil && logs != nil {
		d.Logs = logs
	}
	writeJSON(w, http.StatusOK, d)
}

// apiTraceFields lists the span fields the search bar can suggest.
func (u *UI) apiTraceFields(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"fields": logquery.Spans.Fields(),
		"syntax": []string{
			"service:api name:\"GET /orders\" status:error",
			"duration:>250 kind:client db.system:postgres",
			"bare words match the span name; -field:value excludes; OR between terms",
		},
	})
}
