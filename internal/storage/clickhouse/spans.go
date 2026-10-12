package clickhouse

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/model"
)

// SpanQuery selects spans for the traces list and stats. Where/Args come from
// logquery.Query.SQLFor(logquery.Spans).
type SpanQuery struct {
	ProjectID string
	From      time.Time
	To        time.Time
	Where     string
	Args      []any
	BeforeTS  time.Time // keyset cursor on the entry span's start
	BeforeID  string    // trace id at that instant
	Limit     int
}

const ts6Expr = "toDateTime64(?, 6, 'UTC')"

func ts6(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05.000000") }

// entrySpan selects the span that represents a trace in the list: the root, or a server span
// when the root lives upstream and was never exported (gateways propagating traceparent).
const entrySpan = "(parent_span_id = '' OR kind = 'server')"

// InsertSpans enqueues spans; a full queue drops them (counted in the batcher stats).
func (c *Client) InsertSpans(ctx context.Context, spans []model.Span) error {
	var firstErr error
	for _, sp := range spans {
		if err := c.spans.enqueue(ctx, sp, 0); err != nil && firstErr == nil && err != errQueueFull {
			firstErr = err
		}
	}
	return firstErr
}

// EnqueueSpans makes *Client usable as an ingest sink.
func (c *Client) EnqueueSpans(ctx context.Context, spans []model.Span) error {
	return c.InsertSpans(ctx, spans)
}

func (c *Client) flushSpans(ctx context.Context, spans []model.Span) error {
	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO spans (
			start_ts, duration_us, project_id, service, env, release, hostname, scope,
			trace_id, span_id, parent_span_id, name, kind, status, status_message,
			http_method, http_route, http_status, attrs, events_json
		)
	`)
	if err != nil {
		return err
	}
	for i := range spans {
		sp := &spans[i]
		events := sp.EventsJSON()
		if err := batch.Append(
			sp.StartTS.UTC(), sp.DurationUS, sp.ProjectID, sp.Service, sp.Env, sp.Release, sp.Hostname, sp.Scope,
			sp.TraceID, sp.SpanID, sp.ParentSpanID, sp.Name, sp.Kind, sp.Status, sp.StatusMsg,
			sp.HTTPMethod, sp.HTTPRoute, sp.HTTPStatus, nonNilMap(sp.Attrs), events,
		); err != nil {
			_ = batch.Abort()
			return err
		}
	}
	return batch.Send()
}

const spanColumns = `trace_id, span_id, parent_span_id, service, env, release, hostname, scope, name, kind, status, status_message,
	start_ts, duration_us, http_method, http_route, http_status, attrs, events_json`

type spanRow struct {
	TraceID      string            `ch:"trace_id"`
	SpanID       string            `ch:"span_id"`
	ParentSpanID string            `ch:"parent_span_id"`
	Service      string            `ch:"service"`
	Env          string            `ch:"env"`
	Release      string            `ch:"release"`
	Hostname     string            `ch:"hostname"`
	Scope        string            `ch:"scope"`
	Name         string            `ch:"name"`
	Kind         string            `ch:"kind"`
	Status       string            `ch:"status"`
	StatusMsg    string            `ch:"status_message"`
	StartTS      time.Time         `ch:"start_ts"`
	DurationUS   uint64            `ch:"duration_us"`
	HTTPMethod   string            `ch:"http_method"`
	HTTPRoute    string            `ch:"http_route"`
	HTTPStatus   uint16            `ch:"http_status"`
	Attrs        map[string]string `ch:"attrs"`
	EventsJSON   string            `ch:"events_json"`
}

func (r spanRow) toAPI() api.Span {
	sp := api.Span{TraceID: r.TraceID, SpanID: r.SpanID, ParentSpanID: r.ParentSpanID, Service: r.Service, Env: r.Env, Release: r.Release,
		Hostname: r.Hostname, Scope: r.Scope, Name: r.Name, Kind: r.Kind, Status: r.Status, StatusMsg: r.StatusMsg, StartTS: r.StartTS.UTC(),
		DurationUS: r.DurationUS, HTTPMethod: r.HTTPMethod, HTTPRoute: r.HTTPRoute, HTTPStatus: r.HTTPStatus, Attrs: nonNilMap(r.Attrs)}
	if r.EventsJSON != "" {
		_ = json.Unmarshal([]byte(r.EventsJSON), &sp.Events)
	}
	return sp
}

// TraceSpans returns every span of a trace, oldest first. at (optional) is any instant inside
// the trace: it bounds the scan to the surrounding day partitions.
func (c *Client) TraceSpans(ctx context.Context, projectID, traceID string, at time.Time) ([]api.Span, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(traceID) == "" {
		return nil, errors.New("missing project_id/trace_id")
	}
	q := "SELECT " + spanColumns + " FROM spans WHERE project_id = ? AND trace_id = ?"
	args := []any{projectID, traceID}
	if !at.IsZero() {
		q += " AND start_ts >= ? AND start_ts < ?"
		args = append(args, at.Add(-12*time.Hour).UTC(), at.Add(12*time.Hour).UTC())
	}
	q += " ORDER BY start_ts ASC, span_id ASC LIMIT 5000"
	var rows []spanRow
	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	out := make([]api.Span, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toAPI())
	}
	return out, nil
}

// TraceErrors returns the error occurrences that carry the trace id, oldest first.
func (c *Client) TraceErrors(ctx context.Context, projectID, traceID string, at time.Time) ([]api.ErrorOccurrence, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(traceID) == "" {
		return nil, errors.New("missing project_id/trace_id")
	}
	q := "SELECT " + occurrenceColumns + " FROM error_occurrences WHERE project_id = ? AND trace_id = ?"
	args := []any{projectID, traceID}
	if !at.IsZero() {
		q += " AND ts >= ? AND ts < ?"
		args = append(args, at.Add(-12*time.Hour).UTC(), at.Add(12*time.Hour).UTC())
	}
	q += " ORDER BY ts ASC LIMIT 100"
	var rows []occurrenceRow
	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	out := make([]api.ErrorOccurrence, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toAPI())
	}
	return out, nil
}

// traceListQueries renders the list statement. A query filters on any span of the trace
// (traces containing a matching span); the entry span still represents the trace.
func traceListQuery(q SpanQuery) (string, []any) {
	// Aliases must not shadow column names: ClickHouse would substitute them into the WHERE.
	sql := "SELECT trace_id, argMin(span_id, start_ts) AS root_span_id, min(start_ts) AS ts, argMin(service, start_ts) AS root_service, argMin(name, start_ts) AS root_name," +
		" argMin(kind, start_ts) AS root_kind, argMin(status, start_ts) AS root_status, argMin(duration_us, start_ts) AS root_duration_us," +
		" argMin(http_method, start_ts) AS root_http_method, argMin(http_route, start_ts) AS root_http_route, argMin(http_status, start_ts) AS root_http_status" +
		" FROM spans WHERE project_id = ? AND start_ts >= ? AND start_ts < ? AND " + entrySpan
	args := []any{q.ProjectID, q.From, q.To}
	if q.Where != "" {
		sql += " AND trace_id IN (SELECT trace_id FROM spans WHERE project_id = ? AND start_ts >= ? AND start_ts < ? AND (" + q.Where + "))"
		args = append(append(args, q.ProjectID, q.From, q.To), q.Args...)
	}
	sql += " GROUP BY trace_id"
	if !q.BeforeTS.IsZero() {
		sql += " HAVING (ts, trace_id) < (" + ts6Expr + ", ?)"
		args = append(args, ts6(q.BeforeTS), q.BeforeID)
	}
	sql += " ORDER BY ts DESC LIMIT ?"
	args = append(args, q.Limit)
	return sql, args
}

// TraceList lists traces newest first with trace-wide span counts, error counts and services.
func (c *Client) TraceList(ctx context.Context, q SpanQuery) ([]api.TraceSummary, error) {
	if strings.TrimSpace(q.ProjectID) == "" {
		return nil, errors.New("missing project_id")
	}
	q.From, q.To = defaultRange(q.From, q.To, time.Hour)
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 500 {
		q.Limit = 500
	}
	sql, args := traceListQuery(q)
	rows, err := c.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out := []api.TraceSummary{}
	var ids []string
	for rows.Next() {
		var t api.TraceSummary
		if err := rows.Scan(&t.TraceID, &t.RootSpanID, &t.StartTS, &t.Service, &t.Name, &t.Kind, &t.Status, &t.DurationUS, &t.HTTPMethod, &t.HTTPRoute, &t.HTTPStatus); err != nil {
			rows.Close()
			return nil, err
		}
		t.StartTS = t.StartTS.UTC()
		t.Services = []string{}
		out = append(out, t)
		ids = append(ids, t.TraceID)
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	// Trace-wide counts in one pass over the surrounding window. IN rather than has(): the
	// bloom-filter index on trace_id makes ClickHouse reject has(array, column).
	crows, err := c.conn.Query(ctx, `SELECT trace_id, count(), countIf(status = 'error'), groupUniqArray(service)
		FROM spans WHERE project_id = ? AND trace_id IN (?) AND start_ts >= ? AND start_ts < ? GROUP BY trace_id`,
		q.ProjectID, ids, q.From.Add(-time.Hour), q.To.Add(time.Hour))
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	byID := map[string]int{}
	for i, t := range out {
		byID[t.TraceID] = i
	}
	for crows.Next() {
		var id string
		var n, errs uint64
		var services []string
		if err := crows.Scan(&id, &n, &errs, &services); err != nil {
			return nil, err
		}
		if i, ok := byID[id]; ok {
			sort.Strings(services)
			out[i].SpanCount, out[i].Errors, out[i].Services = n, errs, services
		}
	}
	return out, crows.Err()
}

// TraceStats summarizes the matching spans: latency and error rate per span name, and the
// distribution of entry-span durations in power-of-two buckets (stable, unlike histogram()).
func (c *Client) TraceStats(ctx context.Context, q SpanQuery) (api.TraceStats, error) {
	if strings.TrimSpace(q.ProjectID) == "" {
		return api.TraceStats{}, errors.New("missing project_id")
	}
	q.From, q.To = defaultRange(q.From, q.To, time.Hour)
	where := " FROM spans WHERE project_id = ? AND start_ts >= ? AND start_ts < ?"
	args := []any{q.ProjectID, q.From, q.To}
	if q.Where != "" {
		where += " AND (" + q.Where + ")"
		args = append(args, q.Args...)
	}
	out := api.TraceStats{ByName: []api.SpanNameStats{}, Latency: []api.LatencyBucket{}}
	rows, err := c.conn.Query(ctx, "SELECT name, count() AS n, quantilesTDigest(0.5, 0.95)(duration_us), countIf(status = 'error')"+where+" GROUP BY name ORDER BY n DESC LIMIT 15", args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var s api.SpanNameStats
		var qs []float32
		var errs uint64
		if err := rows.Scan(&s.Name, &s.Count, &qs, &errs); err != nil {
			rows.Close()
			return out, err
		}
		if len(qs) == 2 {
			s.P50US, s.P95US = float64(qs[0]), float64(qs[1])
		}
		if s.Count > 0 {
			s.ErrorPct = float64(errs) / float64(s.Count) * 100
		}
		out.ByName = append(out.ByName, s)
	}
	rows.Close()
	rows, err = c.conn.Query(ctx, "SELECT roundToExp2(duration_us) AS lo, count()"+where+" AND "+entrySpan+" GROUP BY lo ORDER BY lo", args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var b api.LatencyBucket
		if err := rows.Scan(&b.FromUS, &b.Count); err != nil {
			return out, err
		}
		b.ToUS = b.FromUS * 2
		if b.FromUS == 0 {
			b.ToUS = 1
		}
		out.Latency = append(out.Latency, b)
	}
	return out, rows.Err()
}

// TracesWithSpans reports which of the trace ids have at least one stored span between from
// and to, so links to a request timeline are only offered when the timeline exists.
func (c *Client) TracesWithSpans(ctx context.Context, projectID string, traceIDs []string, from, to time.Time) (map[string]bool, error) {
	out := map[string]bool{}
	ids := make([]string, 0, len(traceIDs))
	for _, id := range traceIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if strings.TrimSpace(projectID) == "" || len(ids) == 0 {
		return out, nil
	}
	q, args := tracesWithSpansQuery(projectID, ids, from, to)
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// tracesWithSpansQuery uses IN, not has(): the bloom-filter index on trace_id makes ClickHouse
// reject has(array, column).
func tracesWithSpansQuery(projectID string, ids []string, from, to time.Time) (string, []any) {
	return `SELECT DISTINCT trace_id FROM spans WHERE project_id = ? AND trace_id IN (?) AND start_ts >= ? AND start_ts < ?`,
		[]any{projectID, ids, from.UTC(), to.UTC()}
}
