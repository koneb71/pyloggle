package webui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/auth"
)

// exportChunk is the page size of the keyset loop; QueryLogs caps a page at 1000 rows.
const exportChunk = 1000

// exportColumns is the default CSV column order; attrs/tags are JSON objects.
var exportColumns = []string{"ts", "event_id", "level", "service", "env", "logger", "message", "request_id", "trace_id", "span_id",
	"http_method", "http_route", "http_status", "http_url", "duration_ms", "hostname", "release", "source", "issue_id", "user_id_hash", "exception_type", "attrs", "tags"}

// apiLogsExport streams the explorer selection as CSV or NDJSON, newest first, in keyset
// pages so memory stays flat whatever the row count. The first page runs before any header is
// written so a bad query still answers with JSON and a 400.
func (u *UI) apiLogsExport(w http.ResponseWriter, r *http.Request) {
	lq, err := u.logQueryFromRequest(r, time.Hour)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "ndjson" {
		writeErr(w, http.StatusBadRequest, "bad_format", "format must be csv or ndjson")
		return
	}
	max := u.cfg.ExportMaxRows
	if max <= 0 {
		max = 100000
	}
	limit := qInt(r, "limit", 10000, max)
	if limit == 0 {
		limit = max
	}
	cols := qStrings(r, "cols")
	if len(cols) == 0 {
		cols = exportColumns
	}
	lq.Limit = minInt(exportChunk, limit)
	ctx := r.Context()
	items, err := u.store.QueryLogs(ctx, lq)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}

	name := exportFilename(auth.ProjectIDFromContext(ctx), lq.To, format)
	h := w.Header()
	if format == "csv" {
		h.Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		h.Set("Content-Type", "application/x-ndjson")
	}
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)

	var cw *csv.Writer
	var enc *json.Encoder
	if format == "csv" {
		cw = csv.NewWriter(w)
		_ = cw.Write(cols)
	} else {
		enc = json.NewEncoder(w)
	}
	written := 0
	for {
		for _, ev := range items {
			if cw != nil {
				_ = cw.Write(csvRow(ev, cols))
			} else {
				_ = enc.Encode(ev)
			}
		}
		written += len(items)
		if cw != nil {
			cw.Flush()
		}
		_ = rc.Flush()
		if len(items) < lq.Limit || written >= limit || ctx.Err() != nil {
			return
		}
		last := items[len(items)-1]
		lq.BeforeTS, lq.BeforeID = last.TS, last.EventID
		lq.Limit = minInt(exportChunk, limit-written)
		items, err = u.store.QueryLogs(ctx, lq)
		if err != nil {
			return // headers are out; the truncated file is the best we can do
		}
	}
}

func exportFilename(projectID string, to time.Time, format string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, projectID)
	if safe == "" {
		safe = "project"
	}
	return fmt.Sprintf("pyloggle-logs-%s-%s.%s", safe, to.UTC().Format("20060102-150405"), format)
}

// csvRow renders one event. Text cells that spreadsheets would evaluate as formulas are
// prefixed with an apostrophe (OWASP CSV injection guidance).
func csvRow(ev api.LogEvent, cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = exportCell(ev, c)
	}
	return out
}

func exportCell(ev api.LogEvent, col string) string {
	switch col {
	case "ts":
		return ev.TS.UTC().Format(time.RFC3339Nano)
	case "event_id":
		return ev.EventID
	case "level":
		return csvSafe(ev.Level)
	case "service":
		return csvSafe(ev.Service)
	case "env":
		return csvSafe(ev.Env)
	case "logger":
		return csvSafe(ev.Logger)
	case "message":
		return csvSafe(ev.Message)
	case "request_id":
		return csvSafe(ev.RequestID)
	case "trace_id":
		return csvSafe(ev.TraceID)
	case "span_id":
		return csvSafe(ev.SpanID)
	case "http_method":
		return csvSafe(ev.HTTPMethod)
	case "http_route":
		return csvSafe(ev.HTTPRoute)
	case "http_status":
		if ev.HTTPStatus == 0 {
			return ""
		}
		return strconv.Itoa(int(ev.HTTPStatus))
	case "http_url":
		return csvSafe(ev.HTTPURL)
	case "duration_ms":
		if ev.DurationMS == 0 {
			return ""
		}
		return strconv.FormatFloat(float64(ev.DurationMS), 'f', -1, 32)
	case "hostname":
		return csvSafe(ev.Hostname)
	case "release":
		return csvSafe(ev.Release)
	case "source":
		return csvSafe(ev.Source)
	case "issue_id":
		return ev.IssueID
	case "user_id_hash":
		return csvSafe(ev.UserIDHash)
	case "exception_type":
		return csvSafe(ev.ExceptionType)
	case "attrs":
		return csvSafe(jsonMap(ev.Attrs))
	case "tags":
		return csvSafe(jsonMap(ev.Tags))
	}
	if k, ok := strings.CutPrefix(col, "attrs."); ok {
		return csvSafe(ev.Attrs[k])
	}
	if k, ok := strings.CutPrefix(col, "tags."); ok {
		return csvSafe(ev.Tags[k])
	}
	if k, ok := strings.CutPrefix(col, "@"); ok {
		return csvSafe(ev.Attrs[k])
	}
	return ""
}

func jsonMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
