package clickhouse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/model"
)

// IssueListQuery filters the issues list. Status: "" | all | open | regressed | resolved | ignored.
// Sort: last_seen (default) | first_seen | count | users.
type IssueListQuery struct {
	ProjectID string
	Service   string
	Env       string
	Status    string
	Sort      string
	Q         string
	Release   string
	Level     string
	IssueIDs  []string
	Assignee  string // exact assignee, or "none" for unassigned
	From      time.Time
	To        time.Time
	Limit     int
	Offset    int
}

const issuesAggSelect = `
	SELECT
		project_id, service, env, issue_id,
		argMaxMerge(title) AS title,
		argMaxMerge(culprit) AS culprit,
		argMaxMerge(last_message) AS last_message,
		argMaxMerge(last_release) AS last_release,
		min(first_seen) AS first_seen,
		max(last_seen) AS last_seen,
		sum(occurrence_count) AS occurrence_count,
		uniqIfMerge(affected_users) AS affected_users,
		anyLast(exception_type) AS exception_type,
		anyLast(exception_module) AS exception_module,
		anyLast(level) AS level,
		anyLast(stack_fingerprint) AS stack_fingerprint
	FROM issues_agg
	WHERE project_id = ?`

const issuesStateSelect = `
	SELECT
		project_id, service, env, issue_id,
		argMax(status, updated_at) AS st_status,
		argMax(is_regressed, updated_at) AS is_regressed,
		argMax(assignee, updated_at) AS assignee,
		argMax(snooze_until, updated_at) AS snooze_until,
		argMax(snooze_count, updated_at) AS snooze_count,
		argMax(resolved_in_release, updated_at) AS resolved_in_release,
		max(updated_at) AS status_updated_at
	FROM issues_state
	WHERE project_id = ?
	GROUP BY project_id, service, env, issue_id`

type issueRow struct {
	ProjectID        string    `ch:"project_id"`
	Service          string    `ch:"service"`
	Env              string    `ch:"env"`
	IssueID          string    `ch:"issue_id"`
	Title            string    `ch:"title"`
	Culprit          string    `ch:"culprit"`
	LastMessage      string    `ch:"last_message"`
	LastRelease      string    `ch:"last_release"`
	FirstSeen        time.Time `ch:"first_seen"`
	LastSeen         time.Time `ch:"last_seen"`
	OccurrenceCount  uint64    `ch:"occurrence_count"`
	AffectedUsers    uint64    `ch:"affected_users"`
	ExceptionType    string    `ch:"exception_type"`
	ExceptionModule  string    `ch:"exception_module"`
	Level            string    `ch:"level"`
	StackFingerprint string    `ch:"stack_fingerprint"`
	Status           string    `ch:"status"`
	IsRegressed      uint8     `ch:"is_regressed"`
	Assignee         string    `ch:"assignee"`
	SnoozeUntil      time.Time `ch:"snooze_until"`
	SnoozeCount      uint64    `ch:"snooze_count"`
	ResolvedIn       string    `ch:"resolved_in_release"`
	StatusUpdatedAt  time.Time `ch:"status_updated_at"`
}

func (r issueRow) toAPI() api.IssueSummary {
	s := api.IssueSummary{
		IssueID:           r.IssueID,
		Service:           r.Service,
		Env:               r.Env,
		ProjectID:         r.ProjectID,
		Title:             r.Title,
		Culprit:           r.Culprit,
		LastMessage:       r.LastMessage,
		Level:             r.Level,
		Status:            r.Status,
		IsRegressed:       r.IsRegressed == 1,
		Assignee:          r.Assignee,
		SnoozeCount:       r.SnoozeCount,
		ResolvedInRelease: r.ResolvedIn,
		StatusUpdatedAt:   r.StatusUpdatedAt,
		FirstSeen:         r.FirstSeen,
		LastSeen:          r.LastSeen,
		OccurrenceCount:   r.OccurrenceCount,
		AffectedUsers:     r.AffectedUsers,
		ExceptionType:     r.ExceptionType,
		ExceptionModule:   r.ExceptionModule,
		LastRelease:       r.LastRelease,
		StackFingerprint:  r.StackFingerprint,
	}
	if r.SnoozeUntil.Unix() > 0 {
		t := r.SnoozeUntil.UTC()
		s.SnoozeUntil = &t
	}
	// Legacy top_* fields from the culprit ("app.views in checkout").
	if i := strings.LastIndex(r.Culprit, " in "); i > 0 {
		s.TopFilename = r.Culprit[:i]
		s.TopFunction = r.Culprit[i+4:]
	} else {
		s.TopFilename = r.Culprit
	}
	return s
}

// issuesJoinedQuery builds the joined issues query up to (not including) ORDER BY.
// It returns the SQL and args; outer filters reference the aliases of the joined row.
func issuesJoinedQuery(q IssueListQuery) (string, []any, error) {
	if strings.TrimSpace(q.ProjectID) == "" {
		return "", nil, errors.New("missing project_id")
	}
	inner := issuesAggSelect
	args := []any{q.ProjectID}
	if q.Service != "" {
		inner += " AND service = ?"
		args = append(args, q.Service)
	}
	if q.Env != "" {
		inner += " AND env = ?"
		args = append(args, q.Env)
	}
	if len(q.IssueIDs) > 0 {
		inner += " AND has(?, issue_id)"
		args = append(args, q.IssueIDs)
	}
	inner += " GROUP BY project_id, service, env, issue_id"
	args = append(args, q.ProjectID)

	sql := `
	SELECT
		i.project_id AS project_id, i.service AS service, i.env AS env, i.issue_id AS issue_id,
		i.title AS title, i.culprit AS culprit, i.last_message AS last_message, i.last_release AS last_release,
		i.first_seen AS first_seen, i.last_seen AS last_seen, i.occurrence_count AS occurrence_count,
		i.affected_users AS affected_users, i.exception_type AS exception_type, i.exception_module AS exception_module,
		i.level AS level, i.stack_fingerprint AS stack_fingerprint,
		if(st.st_status = '', 'open', st.st_status) AS status,
		st.is_regressed AS is_regressed,
		st.assignee AS assignee,
		st.snooze_until AS snooze_until,
		st.snooze_count AS snooze_count,
		st.resolved_in_release AS resolved_in_release,
		st.status_updated_at AS status_updated_at
	FROM (` + inner + `) AS i
	LEFT JOIN (` + issuesStateSelect + `) AS st
		ON st.project_id = i.project_id AND st.service = i.service AND st.env = i.env AND st.issue_id = i.issue_id
	WHERE 1 = 1`

	switch strings.ToLower(strings.TrimSpace(q.Status)) {
	case "", "all":
	case "open":
		sql += " AND status = 'open'"
	case "regressed":
		sql += " AND status = 'open' AND is_regressed = 1"
	case "resolved":
		sql += " AND status = 'resolved'"
	case "ignored":
		sql += " AND status = 'ignored'"
	default:
		return "", nil, fmt.Errorf("unknown status %q", q.Status)
	}
	if !q.From.IsZero() {
		sql += " AND last_seen >= ?"
		args = append(args, q.From.UTC())
	}
	if !q.To.IsZero() {
		sql += " AND last_seen < ?"
		args = append(args, q.To.UTC())
	}
	if q.Release != "" {
		sql += " AND last_release = ?"
		args = append(args, q.Release)
	}
	switch a := strings.TrimSpace(q.Assignee); a {
	case "":
	case "none":
		sql += " AND assignee = ''"
	default:
		sql += " AND assignee = ?"
		args = append(args, a)
	}
	if q.Level != "" {
		sql += " AND has(?, level)"
		args = append(args, model.LevelAliases(q.Level))
	}
	if t := strings.TrimSpace(q.Q); t != "" {
		sql += ` AND (positionCaseInsensitiveUTF8(title, ?) > 0 OR positionCaseInsensitiveUTF8(last_message, ?) > 0
			OR positionCaseInsensitiveUTF8(culprit, ?) > 0 OR positionCaseInsensitiveUTF8(exception_type, ?) > 0 OR issue_id = ?)`
		args = append(args, t, t, t, t, t)
	}
	return sql, args, nil
}

var issueSorts = map[string]string{
	"":           "last_seen",
	"last_seen":  "last_seen",
	"first_seen": "first_seen",
	"count":      "occurrence_count",
	"users":      "affected_users",
}

// QueryIssues lists issues with status/sort/search applied in SQL.
func (c *Client) QueryIssues(ctx context.Context, q IssueListQuery) ([]api.IssueSummary, error) {
	sql, args, err := issuesJoinedQuery(q)
	if err != nil {
		return nil, err
	}
	sortCol, ok := issueSorts[strings.ToLower(q.Sort)]
	if !ok {
		return nil, fmt.Errorf("unknown sort %q", q.Sort)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	sql += " ORDER BY " + sortCol + " DESC, issue_id LIMIT ? OFFSET ?"
	args = append(args, limit, max(q.Offset, 0))

	var rows []issueRow
	if err := c.conn.Select(ctx, &rows, sql, args...); err != nil {
		return nil, err
	}
	out := make([]api.IssueSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toAPI())
	}
	return out, nil
}

// CountIssuesByStatus returns counts for the status tabs under the same non-status filters.
func (c *Client) CountIssuesByStatus(ctx context.Context, q IssueListQuery) (map[string]uint64, error) {
	q.Status = ""
	sql, args, err := issuesJoinedQuery(q)
	if err != nil {
		return nil, err
	}
	outer := `SELECT
		countIf(status = 'open') AS c_open,
		countIf(status = 'open' AND is_regressed = 1) AS c_regressed,
		countIf(status = 'resolved') AS c_resolved,
		countIf(status = 'ignored') AS c_ignored,
		count() AS c_all
	FROM (` + sql + `)`
	var open, regressed, resolved, ignored, all uint64
	if err := c.conn.QueryRow(ctx, outer, args...).Scan(&open, &regressed, &resolved, &ignored, &all); err != nil {
		return nil, err
	}
	return map[string]uint64{"open": open, "regressed": regressed, "resolved": resolved, "ignored": ignored, "all": all}, nil
}

// GetIssueSummary fetches one issue.
func (c *Client) GetIssueSummary(ctx context.Context, projectID, issueID string) (api.IssueSummary, bool, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" {
		return api.IssueSummary{}, false, errors.New("missing project_id/issue_id")
	}
	rows, err := c.QueryIssues(ctx, IssueListQuery{ProjectID: projectID, IssueIDs: []string{issueID}, Limit: 1})
	if err != nil || len(rows) == 0 {
		return api.IssueSummary{}, false, err
	}
	return rows[0], true, nil
}

// IssueSparklines returns hourly counts for the last `hours` hours, oldest first, per issue.
func (c *Client) IssueSparklines(ctx context.Context, projectID string, issueIDs []string, hours int) (map[string][]uint64, error) {
	if hours <= 0 {
		hours = 24
	}
	out := make(map[string][]uint64, len(issueIDs))
	for _, id := range issueIDs {
		out[id] = make([]uint64, hours)
	}
	if len(issueIDs) == 0 {
		return out, nil
	}
	start := time.Now().UTC().Truncate(time.Hour).Add(-time.Duration(hours-1) * time.Hour)
	rows, err := c.conn.Query(ctx, `
		SELECT issue_id, bucket, sum(cnt) AS cnt
		FROM issue_counts_hourly
		WHERE project_id = ? AND has(?, issue_id) AND bucket >= ?
		GROUP BY issue_id, bucket`, projectID, issueIDs, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var bucket time.Time
		var cnt uint64
		if err := rows.Scan(&id, &bucket, &cnt); err != nil {
			return nil, err
		}
		idx := int(bucket.Sub(start) / time.Hour)
		if idx >= 0 && idx < hours {
			out[id][idx] += cnt
		}
	}
	return out, rows.Err()
}

// IssueTrend buckets the occurrences of one issue over time.
func (c *Client) IssueTrend(ctx context.Context, projectID, issueID string, from, to time.Time, bucketSeconds int) ([]api.TimeSeriesPoint, error) {
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	from, to = defaultRange(from, to, 24*time.Hour)
	rows, err := c.conn.Query(ctx, `
		SELECT toStartOfInterval(ts, INTERVAL ? SECOND) AS bucket, count() AS cnt
		FROM error_occurrences
		WHERE project_id = ? AND issue_id = ? AND ts >= ? AND ts < ?
		GROUP BY bucket ORDER BY bucket`, bucketSeconds, projectID, issueID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[time.Time]uint64{}
	for rows.Next() {
		var b time.Time
		var n uint64
		if err := rows.Scan(&b, &n); err != nil {
			return nil, err
		}
		got[b.UTC()] = n
	}
	return fillSeries(from, to, time.Duration(bucketSeconds)*time.Second, got), rows.Err()
}

var breakdownFields = map[string]string{
	"env":             "env",
	"service":         "service",
	"release":         "release",
	"http_route":      "http_route",
	"http_method":     "http_method",
	"http_status":     "toString(http_status)",
	"hostname":        "hostname",
	"user":            "user_id_hash",
	"runtime_version": "runtime_version",
	"sdk_version":     "sdk_version",
	"level":           "level",
	"mechanism":       "mechanism_type",
	"handled":         "if(mechanism_handled = 1, 'handled', 'unhandled')",
}

// IssueBreakdown counts occurrences of one issue by a field (or "tag:<key>").
func (c *Client) IssueBreakdown(ctx context.Context, projectID, issueID, field string, from, to time.Time, limit int) ([]api.FacetCount, error) {
	expr, args := "", []any{}
	if strings.HasPrefix(field, "tag:") {
		expr = "tags[?]"
		args = append(args, strings.TrimPrefix(field, "tag:"))
	} else {
		e, ok := breakdownFields[field]
		if !ok {
			return nil, fmt.Errorf("unknown breakdown field %q", field)
		}
		expr = e
	}
	if limit <= 0 {
		limit = 10
	}
	from, to = defaultRange(from, to, 30*24*time.Hour)
	args = append(args, projectID, issueID, from, to, limit)
	rows, err := c.conn.Query(ctx, `
		SELECT `+expr+` AS value, count() AS cnt, uniqIf(user_id_hash, user_id_hash != '') AS users
		FROM error_occurrences
		WHERE project_id = ? AND issue_id = ? AND ts >= ? AND ts < ?
		GROUP BY value ORDER BY cnt DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.FacetCount
	for rows.Next() {
		var f api.FacetCount
		if err := rows.Scan(&f.Value, &f.Count, &f.Users); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type occurrenceRow struct {
	TS               time.Time         `ch:"ts"`
	EventID          string            `ch:"event_id"`
	ProjectID        string            `ch:"project_id"`
	Service          string            `ch:"service"`
	Env              string            `ch:"env"`
	IssueID          string            `ch:"issue_id"`
	Level            string            `ch:"level"`
	Logger           string            `ch:"logger"`
	Message          string            `ch:"message"`
	ExceptionType    string            `ch:"exception_type"`
	ExceptionModule  string            `ch:"exception_module"`
	ExceptionMessage string            `ch:"exception_message"`
	StackFingerprint string            `ch:"stack_fingerprint"`
	FingerprintVer   uint8             `ch:"fingerprint_version"`
	Title            string            `ch:"title"`
	Culprit          string            `ch:"culprit"`
	StackFrames      []map[string]any  `ch:"stack_frames"`
	FramesJSON       string            `ch:"frames_json"`
	ChainJSON        string            `ch:"chain_json"`
	Notes            []string          `ch:"notes"`
	BreadcrumbsJSON  string            `ch:"breadcrumbs_json"`
	TraceID          string            `ch:"trace_id"`
	SpanID           string            `ch:"span_id"`
	RequestID        string            `ch:"request_id"`
	HTTPMethod       string            `ch:"http_method"`
	HTTPRoute        string            `ch:"http_route"`
	HTTPStatus       uint16            `ch:"http_status"`
	HTTPURL          string            `ch:"http_url"`
	HTTPQuery        string            `ch:"http_query"`
	HTTPHeaders      map[string]string `ch:"http_headers"`
	HTTPBody         string            `ch:"http_body"`
	HTTPUserAgent    string            `ch:"http_user_agent"`
	HTTPRemoteAddr   string            `ch:"http_remote_addr"`
	HTTPServerAddr   string            `ch:"http_server_addr"`
	HTTPClientIP     string            `ch:"http_client_ip"`
	UserIDHash       string            `ch:"user_id_hash"`
	UserID           string            `ch:"user_id"`
	UserEmail        string            `ch:"user_email"`
	UserUsername     string            `ch:"user_username"`
	UserIP           string            `ch:"user_ip"`
	Release          string            `ch:"release"`
	Hostname         string            `ch:"hostname"`
	RuntimeName      string            `ch:"runtime_name"`
	RuntimeVersion   string            `ch:"runtime_version"`
	RuntimePlatform  string            `ch:"runtime_platform"`
	RuntimePID       uint32            `ch:"runtime_pid"`
	RuntimeThread    string            `ch:"runtime_thread"`
	SDKName          string            `ch:"sdk_name"`
	SDKVersion       string            `ch:"sdk_version"`
	Source           string            `ch:"source"`
	MechanismType    string            `ch:"mechanism_type"`
	MechanismHandled uint8             `ch:"mechanism_handled"`
	Tags             map[string]string `ch:"tags"`
	Attrs            map[string]string `ch:"attrs"`
	ExtraJSON        string            `ch:"extra_json"`
}

const occurrenceColumns = `ts, event_id, project_id, service, env, issue_id, level, logger, message,
	exception_type, exception_module, exception_message, stack_fingerprint, fingerprint_version, title, culprit,
	stack_frames, frames_json, chain_json, notes, breadcrumbs_json, trace_id, span_id, request_id,
	http_method, http_route, http_status, http_url, http_query, http_headers, http_body,
	http_user_agent, http_remote_addr, http_server_addr, http_client_ip,
	user_id_hash, user_id, user_email, user_username, user_ip,
	release, hostname, runtime_name, runtime_version, runtime_platform, runtime_pid, runtime_thread,
	sdk_name, sdk_version, source, mechanism_type, mechanism_handled, tags, attrs, extra_json`

func (r occurrenceRow) toAPI() api.ErrorOccurrence {
	o := api.ErrorOccurrence{
		TS:               r.TS,
		EventID:          r.EventID,
		IssueID:          r.IssueID,
		Service:          r.Service,
		Env:              r.Env,
		ProjectID:        r.ProjectID,
		Level:            r.Level,
		Logger:           r.Logger,
		Message:          r.Message,
		Title:            r.Title,
		Culprit:          r.Culprit,
		ExceptionType:    r.ExceptionType,
		ExceptionModule:  r.ExceptionModule,
		ExceptionMessage: r.ExceptionMessage,
		StackFingerprint: r.StackFingerprint,
		FingerprintVer:   r.FingerprintVer,
		StackFrames:      r.StackFrames,
		Notes:            r.Notes,
		TraceID:          r.TraceID,
		SpanID:           r.SpanID,
		RequestID:        r.RequestID,
		HTTPMethod:       r.HTTPMethod,
		HTTPRoute:        r.HTTPRoute,
		HTTPStatus:       r.HTTPStatus,
		UserIDHash:       r.UserIDHash,
		Release:          r.Release,
		Hostname:         r.Hostname,
		Source:           r.Source,
		Tags:             r.Tags,
		Attrs:            r.Attrs,
	}
	if r.FramesJSON != "" {
		_ = json.Unmarshal([]byte(r.FramesJSON), &o.Frames)
	}
	if len(o.Frames) == 0 && len(r.StackFrames) > 0 {
		o.Frames = framesFromLegacy(r.StackFrames)
	}
	if r.ChainJSON != "" {
		_ = json.Unmarshal([]byte(r.ChainJSON), &o.Chain)
	}
	if r.BreadcrumbsJSON != "" {
		_ = json.Unmarshal([]byte(r.BreadcrumbsJSON), &o.Breadcrumbs)
	}
	if r.HTTPMethod != "" || r.HTTPRoute != "" || r.HTTPURL != "" || r.HTTPStatus != 0 {
		o.HTTP = &model.HTTPInfo{
			Method: r.HTTPMethod, Route: r.HTTPRoute, Status: r.HTTPStatus, URL: r.HTTPURL, Query: r.HTTPQuery,
			Headers: r.HTTPHeaders, Body: model.JSONText(r.HTTPBody), UserAgent: r.HTTPUserAgent,
			RemoteAddr: r.HTTPRemoteAddr, ServerAddr: r.HTTPServerAddr, ClientIP: r.HTTPClientIP,
		}
	}
	if r.UserIDHash != "" || r.UserID != "" || r.UserEmail != "" {
		o.User = &model.UserInfo{IDHash: r.UserIDHash, ID: r.UserID, Email: r.UserEmail, Username: r.UserUsername, IP: r.UserIP}
	}
	if r.RuntimeName != "" || r.RuntimeVersion != "" {
		o.Runtime = &model.RuntimeInfo{Name: r.RuntimeName, Version: r.RuntimeVersion, Platform: r.RuntimePlatform, PID: int(r.RuntimePID), Thread: r.RuntimeThread}
	}
	if r.SDKName != "" {
		o.SDK = &model.SDKInfo{Name: r.SDKName, Version: r.SDKVersion}
	}
	if r.MechanismType != "" || r.MechanismHandled == 0 {
		h := r.MechanismHandled == 1
		o.Mechanism = &model.Mechanism{Type: r.MechanismType, Handled: &h}
	}
	if r.ExtraJSON != "" && json.Valid([]byte(r.ExtraJSON)) {
		o.Extra = json.RawMessage(r.ExtraJSON)
	}
	return o
}

func framesFromLegacy(rows []map[string]any) []model.Frame {
	out := make([]model.Frame, 0, len(rows))
	for _, m := range rows {
		f := model.Frame{}
		if v, ok := m["filename"].(string); ok {
			f.Filename = v
		}
		if v, ok := m["function"].(string); ok {
			f.Function = v
		}
		if v, ok := m["context"].(string); ok {
			f.Context = v
		}
		switch v := m["lineno"].(type) {
		case uint32:
			f.Lineno = v
		case int64:
			f.Lineno = uint32(v)
		case float64:
			f.Lineno = uint32(v)
		}
		inApp := false
		switch v := m["is_in_app"].(type) {
		case uint8:
			inApp = v == 1
		case int64:
			inApp = v == 1
		case float64:
			inApp = v == 1
		}
		f.InApp = &inApp
		out = append(out, f)
	}
	return out
}

// GetOccurrence loads one occurrence with prev/next ids within its issue.
func (c *Client) GetOccurrence(ctx context.Context, projectID, eventID string) (api.ErrorOccurrence, bool, error) {
	if projectID == "" || eventID == "" {
		return api.ErrorOccurrence{}, false, errors.New("missing project_id/event_id")
	}
	var rows []occurrenceRow
	if err := c.conn.Select(ctx, &rows, "SELECT "+occurrenceColumns+" FROM error_occurrences WHERE project_id = ? AND event_id = ? LIMIT 1", projectID, eventID); err != nil {
		return api.ErrorOccurrence{}, false, err
	}
	if len(rows) == 0 {
		return api.ErrorOccurrence{}, false, nil
	}
	r := rows[0]
	o := r.toAPI()
	base := "SELECT event_id FROM error_occurrences WHERE project_id = ? AND service = ? AND env = ? AND issue_id = ? AND (ts, event_id) "
	_ = c.conn.QueryRow(ctx, base+"< ("+ts3Expr+", ?) ORDER BY ts DESC, event_id DESC LIMIT 1", projectID, r.Service, r.Env, r.IssueID, ts3(r.TS), r.EventID).Scan(&o.PrevEventID)
	_ = c.conn.QueryRow(ctx, base+"> ("+ts3Expr+", ?) ORDER BY ts ASC, event_id ASC LIMIT 1", projectID, r.Service, r.Env, r.IssueID, ts3(r.TS), r.EventID).Scan(&o.NextEventID)
	return o, true, nil
}

// LatestOccurrence returns the newest occurrence of an issue (full detail).
func (c *Client) LatestOccurrence(ctx context.Context, projectID, issueID string) (api.ErrorOccurrence, bool, error) {
	var rows []occurrenceRow
	if err := c.conn.Select(ctx, &rows, "SELECT "+occurrenceColumns+" FROM error_occurrences WHERE project_id = ? AND issue_id = ? ORDER BY ts DESC LIMIT 1", projectID, issueID); err != nil {
		return api.ErrorOccurrence{}, false, err
	}
	if len(rows) == 0 {
		return api.ErrorOccurrence{}, false, nil
	}
	return c.GetOccurrence(ctx, projectID, rows[0].EventID)
}

// QueryOccurrences lists occurrences of an issue newest-first with a keyset cursor (before).
func (c *Client) QueryOccurrences(ctx context.Context, projectID, issueID string, from, to, before time.Time, limit int) ([]api.ErrorOccurrence, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" {
		return nil, errors.New("missing project_id/issue_id")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	q := "SELECT " + occurrenceColumns + " FROM error_occurrences WHERE project_id = ? AND issue_id = ?"
	args := []any{projectID, issueID}
	if !from.IsZero() {
		q += " AND ts >= ?"
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		q += " AND ts < ?"
		args = append(args, to.UTC())
	}
	if !before.IsZero() {
		q += " AND ts < ?"
		args = append(args, before.UTC())
	}
	q += " ORDER BY ts DESC LIMIT ?"
	args = append(args, limit)
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

// UpsertIssueState is the legacy signature used by the admin API.
func (c *Client) UpsertIssueState(ctx context.Context, projectID, service, env, issueID, status, updatedBy, comment string) error {
	return c.SetIssueState(ctx, api.IssueStateChange{ProjectID: projectID, Service: service, Env: env, IssueID: issueID, Status: status, UpdatedBy: updatedBy, Comment: comment})
}

// ListIssues is the legacy signature (status filter not applied).
func (c *Client) ListIssues(ctx context.Context, projectID, service, env string, from, to time.Time, limit int) ([]api.IssueSummary, error) {
	return c.QueryIssues(ctx, IssueListQuery{ProjectID: projectID, Service: service, Env: env, From: from, To: to, Limit: limit})
}

// ListIssueOccurrences is the legacy signature.
func (c *Client) ListIssueOccurrences(ctx context.Context, projectID, issueID string, from, to time.Time, limit int) ([]api.ErrorOccurrence, error) {
	return c.QueryOccurrences(ctx, projectID, issueID, from, to, time.Time{}, limit)
}

// The driver binds time.Time at second precision; comparisons that must be exact bind a
// millisecond string through toDateTime64 instead.
const ts3Expr = "toDateTime64(?, 3, 'UTC')"

func ts3(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05.000") }

func defaultRange(from, to time.Time, def time.Duration) (time.Time, time.Time) {
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-def)
	}
	return from.UTC(), to.UTC()
}

// fillSeries returns one point per bucket between from and to (zero-filled).
func fillSeries(from, to time.Time, step time.Duration, got map[time.Time]uint64) []api.TimeSeriesPoint {
	if step <= 0 {
		step = time.Hour
	}
	start := from.Truncate(step)
	n := int(to.Sub(start)/step) + 1
	if n > 5000 {
		n = 5000
	}
	out := make([]api.TimeSeriesPoint, 0, n)
	for i := 0; i < n; i++ {
		b := start.Add(time.Duration(i) * step)
		if !b.Before(to) {
			break
		}
		out = append(out, api.TimeSeriesPoint{TS: b, Count: got[b]})
	}
	return out
}
