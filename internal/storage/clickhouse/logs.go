package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/model"
)

// LogQuery is the explorer query. Where/Args carry the conditions rendered by the query
// language (ANDed to the base filters); the plain fields cover the simple REST filters.
type LogQuery struct {
	ProjectID string
	From      time.Time
	To        time.Time

	Where string
	Args  []any

	Service      string
	Env          string
	Logger       string
	TraceID      string
	RequestID    string
	IssueID      string
	Release      string
	Hostname     string
	HTTPRoute    string
	HTTPMethod   string
	Text         string
	Levels       []string
	HasException bool

	After     time.Time // live tail: ts > After
	BeforeTS  time.Time // keyset cursor: (ts, event_id) < (BeforeTS, BeforeID)
	BeforeID  string
	Limit     int
	Ascending bool
}

const logColumns = `ts, event_id, service, env, level, logger, message, trace_id, span_id, request_id, project_id,
	issue_id, release, hostname, source, http_method, http_route, http_status, http_url, duration_ms,
	user_id_hash, exception_type, attrs, tags`

type logRow struct {
	TS            time.Time         `ch:"ts"`
	EventID       string            `ch:"event_id"`
	Service       string            `ch:"service"`
	Env           string            `ch:"env"`
	Level         string            `ch:"level"`
	Logger        string            `ch:"logger"`
	Message       string            `ch:"message"`
	TraceID       string            `ch:"trace_id"`
	SpanID        string            `ch:"span_id"`
	RequestID     string            `ch:"request_id"`
	ProjectID     string            `ch:"project_id"`
	IssueID       string            `ch:"issue_id"`
	Release       string            `ch:"release"`
	Hostname      string            `ch:"hostname"`
	Source        string            `ch:"source"`
	HTTPMethod    string            `ch:"http_method"`
	HTTPRoute     string            `ch:"http_route"`
	HTTPStatus    uint16            `ch:"http_status"`
	HTTPURL       string            `ch:"http_url"`
	DurationMS    float32           `ch:"duration_ms"`
	HTTPBody      string            `ch:"http_body"` // only selected by GetLog
	UserIDHash    string            `ch:"user_id_hash"`
	ExceptionType string            `ch:"exception_type"`
	Attrs         map[string]string `ch:"attrs"`
	Tags          map[string]string `ch:"tags"`
}

func (r logRow) toAPI() api.LogEvent {
	return api.LogEvent{
		TS: r.TS, EventID: r.EventID, Service: r.Service, Env: r.Env, Level: r.Level, Logger: r.Logger,
		Message: r.Message, TraceID: r.TraceID, SpanID: r.SpanID, RequestID: r.RequestID, ProjectID: r.ProjectID,
		IssueID: r.IssueID, Release: r.Release, Hostname: r.Hostname, Source: r.Source,
		HTTPMethod: r.HTTPMethod, HTTPRoute: r.HTTPRoute, HTTPStatus: r.HTTPStatus, HTTPURL: r.HTTPURL,
		DurationMS: r.DurationMS, HTTPBody: r.HTTPBody, UserIDHash: r.UserIDHash, ExceptionType: r.ExceptionType,
		Attrs: r.Attrs, Tags: r.Tags,
	}
}

// where renders the WHERE clause (without the keyword) and its args.
// CountLogs counts the rows matching the query (alert monitors, rule previews).
func (c *Client) CountLogs(ctx context.Context, q LogQuery) (uint64, error) {
	where, args, err := q.where()
	if err != nil {
		return 0, err
	}
	var n uint64
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM logs_raw WHERE "+where, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (q LogQuery) where() (string, []any, error) {
	if strings.TrimSpace(q.ProjectID) == "" {
		return "", nil, errors.New("missing project_id")
	}
	from, to := defaultRange(q.From, q.To, time.Hour)
	var sb strings.Builder
	sb.WriteString("project_id = ? AND ts >= ? AND ts < ?")
	args := []any{q.ProjectID, from, to}
	add := func(cond string, a ...any) {
		sb.WriteString(" AND " + cond)
		args = append(args, a...)
	}
	if q.Service != "" {
		add("service = ?", q.Service)
	}
	if q.Env != "" {
		add("env = ?", q.Env)
	}
	if q.Logger != "" {
		add("logger = ?", q.Logger)
	}
	if q.TraceID != "" {
		add("trace_id = ?", q.TraceID)
	}
	if q.RequestID != "" {
		add("request_id = ?", q.RequestID)
	}
	if q.IssueID != "" {
		add("issue_id = ?", q.IssueID)
	}
	if q.Release != "" {
		add("release = ?", q.Release)
	}
	if q.Hostname != "" {
		add("hostname = ?", q.Hostname)
	}
	if q.HTTPRoute != "" {
		add("http_route = ?", q.HTTPRoute)
	}
	if q.HTTPMethod != "" {
		add("http_method = ?", q.HTTPMethod)
	}
	if len(q.Levels) > 0 {
		var all []string
		for _, l := range q.Levels {
			all = append(all, model.LevelAliases(l)...)
		}
		add("has(?, level)", all)
	}
	if q.HasException {
		sb.WriteString(" AND exception_type != ''")
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		add("positionCaseInsensitiveUTF8(message, ?) > 0", t)
	}
	if q.Where != "" {
		add("("+q.Where+")", q.Args...)
	}
	if !q.After.IsZero() {
		add("ts > "+ts3Expr, ts3(q.After))
	}
	if !q.BeforeTS.IsZero() {
		if q.BeforeID != "" {
			add("(ts, event_id) < ("+ts3Expr+", ?)", ts3(q.BeforeTS), q.BeforeID)
		} else {
			add("ts < "+ts3Expr, ts3(q.BeforeTS))
		}
	}
	return sb.String(), args, nil
}

// QueryLogs returns log rows, newest first unless Ascending.
func (c *Client) QueryLogs(ctx context.Context, q LogQuery) ([]api.LogEvent, error) {
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	order := "ORDER BY ts DESC, event_id DESC"
	if q.Ascending {
		order = "ORDER BY ts ASC, event_id ASC"
	}
	sql := "SELECT " + logColumns + " FROM logs_raw WHERE " + where + " " + order + " LIMIT ?"
	args = append(args, limit)
	var rows []logRow
	if err := c.conn.Select(ctx, &rows, sql, args...); err != nil {
		return nil, err
	}
	out := make([]api.LogEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toAPI())
	}
	return out, nil
}

// ListLogs is the legacy signature used by /v1/logs and the old UI.
func (c *Client) ListLogs(ctx context.Context, projectID, service, env, level, text, traceID, requestID string, from, to time.Time, limit int) ([]api.LogEvent, error) {
	q := LogQuery{ProjectID: projectID, Service: service, Env: env, Text: text, TraceID: traceID, RequestID: requestID, From: from, To: to, Limit: limit}
	if level != "" {
		q.Levels = []string{level}
	}
	return c.QueryLogs(ctx, q)
}

var histogramGroups = map[string]string{
	"":            "''",
	"level":       "level",
	"service":     "service",
	"env":         "env",
	"logger":      "logger",
	"http_status": "toString(http_status)",
	"hostname":    "hostname",
	"release":     "release",
	"source":      "source",
}

// NiceBucket picks a bucket size so the range yields at most maxBuckets bars.
func NiceBucket(from, to time.Time, maxBuckets int) time.Duration {
	if maxBuckets <= 0 {
		maxBuckets = 80
	}
	span := to.Sub(from)
	if span <= 0 {
		return time.Minute
	}
	candidates := []time.Duration{time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute,
		5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, 3 * time.Hour,
		6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}
	for _, c := range candidates {
		if span/c <= time.Duration(maxBuckets) {
			return c
		}
	}
	return candidates[len(candidates)-1]
}

// LogHistogram buckets the matching rows over time, split by groupBy (top 8 series + "other").
func (c *Client) LogHistogram(ctx context.Context, q LogQuery, groupBy string, bucket time.Duration) (api.Histogram, error) {
	expr, ok := histogramGroups[groupBy]
	if !ok {
		return api.Histogram{}, fmt.Errorf("unknown group_by %q", groupBy)
	}
	from, to := defaultRange(q.From, q.To, time.Hour)
	q.From, q.To = from, to
	if bucket <= 0 {
		bucket = NiceBucket(from, to, 80)
	}
	where, args, err := q.where()
	if err != nil {
		return api.Histogram{}, err
	}
	secs := int(bucket / time.Second)
	sql := "SELECT toStartOfInterval(ts, INTERVAL ? SECOND) AS b, " + expr + " AS grp, count() AS cnt FROM logs_raw WHERE " + where + " GROUP BY b, grp ORDER BY b"
	args = append([]any{secs}, args...)
	rows, err := c.conn.Query(ctx, sql, args...)
	if err != nil {
		return api.Histogram{}, err
	}
	defer rows.Close()
	type cell struct {
		b   time.Time
		grp string
		n   uint64
	}
	var cells []cell
	totals := map[string]uint64{}
	for rows.Next() {
		var cl cell
		if err := rows.Scan(&cl.b, &cl.grp, &cl.n); err != nil {
			return api.Histogram{}, err
		}
		cl.b = cl.b.UTC()
		cells = append(cells, cl)
		totals[cl.grp] += cl.n
	}
	if err := rows.Err(); err != nil {
		return api.Histogram{}, err
	}
	// Keep the 8 largest series; fold the rest into "other".
	series := make([]string, 0, len(totals))
	for g := range totals {
		series = append(series, g)
	}
	sort.Slice(series, func(i, j int) bool {
		if totals[series[i]] != totals[series[j]] {
			return totals[series[i]] > totals[series[j]]
		}
		return series[i] < series[j]
	})
	keep := map[string]bool{}
	for i, g := range series {
		if i < 8 {
			keep[g] = true
		}
	}
	if len(series) > 8 {
		series = append(series[:8], "other")
	}
	if groupBy == "" {
		series = []string{"total"}
	}
	byBucket := map[time.Time]map[string]uint64{}
	for _, cl := range cells {
		g := cl.grp
		if groupBy == "" {
			g = "total"
		} else if !keep[g] {
			g = "other"
		}
		m := byBucket[cl.b]
		if m == nil {
			m = map[string]uint64{}
			byBucket[cl.b] = m
		}
		m[g] += cl.n
	}
	h := api.Histogram{BucketSeconds: secs, GroupBy: groupBy, Series: series}
	start := from.Truncate(bucket)
	for b := start; b.Before(to) && len(h.Buckets) < 5000; b = b.Add(bucket) {
		counts := byBucket[b]
		if counts == nil {
			counts = map[string]uint64{}
		}
		var total uint64
		for _, n := range counts {
			total += n
		}
		h.Buckets = append(h.Buckets, api.HistogramBucket{TS: b, Total: total, Counts: counts})
	}
	return h, nil
}

var facetFields = map[string]string{
	"level":       "level",
	"service":     "service",
	"env":         "env",
	"logger":      "logger",
	"hostname":    "hostname",
	"release":     "release",
	"source":      "source",
	"http_method": "http_method",
	"http_status": "toString(http_status)",
	"http_route":  "http_route",
}

// DefaultFacetFields is the explorer sidebar set.
var DefaultFacetFields = []string{"level", "service", "env", "logger", "hostname", "release", "http_method", "http_status", "http_route"}

// LogFacets counts values per field under the query; attrKey selects values of one attrs/tags key.
func (c *Client) LogFacets(ctx context.Context, q LogQuery, fields []string, attrKeys int, attrKey string) (api.LogFacets, error) {
	where, args, err := q.where()
	if err != nil {
		return api.LogFacets{}, err
	}
	out := api.LogFacets{Fields: map[string][]api.FacetCount{}}
	if len(fields) == 0 {
		fields = DefaultFacetFields
	}
	// exprArgs bind placeholders inside the SELECT expression, so they come before the WHERE args.
	facet := func(expr string, exprArgs []any, extraWhere string, extraArgs ...any) ([]api.FacetCount, error) {
		sql := "SELECT " + expr + " AS value, count() AS cnt FROM logs_raw WHERE " + where + extraWhere + " GROUP BY value ORDER BY cnt DESC LIMIT 25"
		a := append(append(append([]any{}, exprArgs...), args...), extraArgs...)
		rows, err := c.conn.Query(ctx, sql, a...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var fc []api.FacetCount
		for rows.Next() {
			var f api.FacetCount
			if err := rows.Scan(&f.Value, &f.Count); err != nil {
				return nil, err
			}
			if (f.Value == "" && expr != "level") || (f.Value == "0" && expr == "toString(http_status)") {
				continue // empty / non-HTTP rows are not a facet value
			}
			fc = append(fc, f)
		}
		return fc, rows.Err()
	}
	for _, f := range fields {
		expr, ok := facetFields[f]
		if !ok {
			continue
		}
		fc, err := facet(expr, nil, "")
		if err != nil {
			return out, err
		}
		out.Fields[f] = fc
	}
	if attrKeys > 0 {
		for _, col := range []string{"attrs", "tags"} {
			sql := "SELECT k AS value, count() AS cnt FROM logs_raw ARRAY JOIN mapKeys(" + col + ") AS k WHERE " + where + " GROUP BY k ORDER BY cnt DESC LIMIT ?"
			rows, err := c.conn.Query(ctx, sql, append(append([]any{}, args...), attrKeys)...)
			if err != nil {
				return out, err
			}
			var fc []api.FacetCount
			for rows.Next() {
				var f api.FacetCount
				if err := rows.Scan(&f.Value, &f.Count); err != nil {
					rows.Close()
					return out, err
				}
				fc = append(fc, f)
			}
			rows.Close()
			if col == "attrs" {
				out.AttrKeys = fc
			} else {
				out.TagKeys = fc
			}
		}
	}
	if attrKey != "" {
		col, key := "attrs", attrKey
		if strings.HasPrefix(attrKey, "tags.") {
			col, key = "tags", strings.TrimPrefix(attrKey, "tags.")
		} else if strings.HasPrefix(attrKey, "attrs.") {
			key = strings.TrimPrefix(attrKey, "attrs.")
		}
		fc, err := facet(col+"[?]", []any{key}, " AND mapContains("+col+", ?)", key)
		if err != nil {
			return out, err
		}
		out.Values = fc
	}
	return out, nil
}

// LogContext returns the lines around one event (same project/service/env), chronological.
// GetLog returns one message with its request body, which the lists leave out to stay light.
func (c *Client) GetLog(ctx context.Context, projectID, eventID string) (api.LogEvent, bool, error) {
	var rows []logRow
	if err := c.conn.Select(ctx, &rows, "SELECT "+logColumns+", http_body FROM logs_raw WHERE project_id = ? AND event_id = ? LIMIT 1", projectID, eventID); err != nil {
		return api.LogEvent{}, false, err
	}
	if len(rows) == 0 {
		return api.LogEvent{}, false, nil
	}
	return rows[0].toAPI(), true, nil
}

func (c *Client) LogContext(ctx context.Context, projectID, eventID string, before, after int) (api.LogContext, bool, error) {
	if before <= 0 {
		before = 50
	}
	if after <= 0 {
		after = 50
	}
	if before > 500 {
		before = 500
	}
	if after > 500 {
		after = 500
	}
	var anchor []logRow
	if err := c.conn.Select(ctx, &anchor, "SELECT "+logColumns+" FROM logs_raw WHERE project_id = ? AND event_id = ? LIMIT 1", projectID, eventID); err != nil {
		return api.LogContext{}, false, err
	}
	if len(anchor) == 0 {
		return api.LogContext{}, false, nil
	}
	a := anchor[0]
	out := api.LogContext{Anchor: a.toAPI()}
	var rows []logRow
	if err := c.conn.Select(ctx, &rows, "SELECT "+logColumns+` FROM logs_raw WHERE project_id = ? AND service = ? AND env = ?
		AND ts >= ? AND (ts, event_id) < (`+ts3Expr+`, ?) ORDER BY ts DESC, event_id DESC LIMIT ?`,
		projectID, a.Service, a.Env, a.TS.Add(-2*time.Hour), ts3(a.TS), a.EventID, before); err != nil {
		return out, true, err
	}
	for i := len(rows) - 1; i >= 0; i-- {
		out.Before = append(out.Before, rows[i].toAPI())
	}
	rows = nil
	if err := c.conn.Select(ctx, &rows, "SELECT "+logColumns+` FROM logs_raw WHERE project_id = ? AND service = ? AND env = ?
		AND ts < ? AND (ts, event_id) > (`+ts3Expr+`, ?) ORDER BY ts ASC, event_id ASC LIMIT ?`,
		projectID, a.Service, a.Env, a.TS.Add(2*time.Hour), ts3(a.TS), a.EventID, after); err != nil {
		return out, true, err
	}
	for _, r := range rows {
		out.After = append(out.After, r.toAPI())
	}
	return out, true, nil
}

// normalizedMessageSQL mirrors issue.NormalizeMessage in ClickHouse (same substitutions, same
// order). The single quote is written as \x27 and the optional group as {0,1} so the driver's
// placeholder scanner never sees a stray quote or '?'.
const normalizedMessageSQL = `replaceRegexpAll(replaceRegexpAll(replaceRegexpAll(replaceRegexpAll(message,
	'[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}', '<uuid>'),
	'"[^"]*"|\\x27[^\\x27]*\\x27', '<s>'),
	'\\b[0-9a-fA-F]{8,}\\b', '<hex>'),
	'\\d+(\\.\\d+){0,1}', '<n>')`

// LogPatterns clusters the matching rows by message template.
func (c *Client) LogPatterns(ctx context.Context, q LogQuery, limit int) ([]api.LogPattern, error) {
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	sql := `SELECT ` + normalizedMessageSQL + ` AS pattern, count() AS cnt, any(message) AS sample, anyLast(level) AS lvl, groupUniqArray(5)(service) AS services
		FROM logs_raw WHERE ` + where + ` GROUP BY pattern ORDER BY cnt DESC LIMIT ?`
	rows, err := c.conn.Query(ctx, sql, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.LogPattern
	for rows.Next() {
		var p api.LogPattern
		if err := rows.Scan(&p.Pattern, &p.Count, &p.Sample, &p.Level, &p.Services); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
