package clickhouse

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"pyloggle/internal/api"
	"pyloggle/internal/issue"
	"pyloggle/internal/model"
)

// Client is a thin wrapper around ClickHouse operations we need.
type Client struct {
	conn ch.Conn

	logCh chan model.Event
	errCh chan issue.ErrorOccurrence

	batchSize    int
	batchTimeout time.Duration

	wg   sync.WaitGroup
	stop chan struct{}

	droppedLogs   atomic.Uint64
	droppedErrors atomic.Uint64
	lastDropLog   atomic.Int64
}

// Stats reports batching health for /healthz?verbose=1.
type Stats struct {
	DroppedLogs   uint64 `json:"dropped_logs"`
	DroppedErrors uint64 `json:"dropped_errors"`
	QueuedLogs    int    `json:"queued_logs"`
	QueuedErrors  int    `json:"queued_errors"`
}

func (c *Client) Stats() Stats {
	return Stats{
		DroppedLogs:   c.droppedLogs.Load(),
		DroppedErrors: c.droppedErrors.Load(),
		QueuedLogs:    len(c.logCh),
		QueuedErrors:  len(c.errCh),
	}
}

// flushWithRetry retries a batch flush with backoff; rows are only dropped (and counted) after
// the last attempt fails. Drop messages are rate-limited to one per 10s.
func (c *Client) flushWithRetry(kind string, n int, f func() error) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = f(); err == nil {
			return
		}
		if attempt < 2 {
			time.Sleep(time.Duration(500<<attempt) * time.Millisecond)
		}
	}
	if kind == "logs" {
		c.droppedLogs.Add(uint64(n))
	} else {
		c.droppedErrors.Add(uint64(n))
	}
	now := time.Now().Unix()
	if last := c.lastDropLog.Load(); now-last >= 10 && c.lastDropLog.CompareAndSwap(last, now) {
		log.Printf("clickhouse: dropped %d %s rows after 3 failed flush attempts: %v", n, kind, err)
	}
}

func New(dsn string, maxQueueSize int, batchSize int, batchTimeout time.Duration) (*Client, error) {
	opt, err := ch.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}

	// NOTE: Don't force compression here. On some Docker-for-Mac setups we've seen native-protocol
	// stalls/timeouts that disappear when compression is disabled or left to DSN defaults.
	// If you want compression, set it explicitly in CLICKHOUSE_DSN.

	conn, err := ch.Open(opt)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// conn.Ping() is implemented in the native protocol; when using HTTP DSNs it can fail with
	// handshake errors. Use a simple query to validate connectivity in HTTP mode.
	if opt.Protocol == ch.HTTP {
		var one uint8
		if err := conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
			_ = conn.Close()
			return nil, err
		}
	} else if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	if maxQueueSize <= 0 {
		maxQueueSize = 10000
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	if batchTimeout <= 0 {
		batchTimeout = 5 * time.Second
	}

	c := &Client{
		conn:         conn,
		logCh:        make(chan model.Event, maxQueueSize),
		errCh:        make(chan issue.ErrorOccurrence, maxQueueSize),
		batchSize:    batchSize,
		batchTimeout: batchTimeout,
		stop:         make(chan struct{}),
	}

	c.wg.Add(2)
	go c.runLogs()
	go c.runErrors()

	return c, nil
}

func (c *Client) Close() error {
	select {
	case <-c.stop:
		// already closed
	default:
		close(c.stop)
	}
	c.wg.Wait()
	return c.conn.Close()
}

type LogRow struct {
	TS        time.Time
	Service   string
	Env       string
	Level     string
	Logger    string
	Message   string
	TraceID   string
	SpanID    string
	RequestID string
	ProjectID string
	Attrs     map[string]string
}

func (c *Client) InsertLog(ctx context.Context, ev model.Event) error {
	// Never block request handling for non-error logs.
	select {
	case c.logCh <- ev:
		return nil
	default:
		// Keep errors even if queue is full (best effort).
		if ev.Exception != nil || strings.EqualFold(ev.Level, "error") {
			t := time.NewTimer(250 * time.Millisecond)
			defer t.Stop()
			select {
			case c.logCh <- ev:
				return nil
			case <-t.C:
				return errors.New("log queue full")
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
}

func (c *Client) InsertErrorOccurrence(ctx context.Context, occ issue.ErrorOccurrence) error {
	// Errors are high-signal; we allow brief blocking to avoid dropping.
	select {
	case c.errCh <- occ:
		return nil
	default:
		t := time.NewTimer(750 * time.Millisecond)
		defer t.Stop()
		select {
		case c.errCh <- occ:
			return nil
		case <-t.C:
			return errors.New("error queue full")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *Client) runLogs() {
	defer c.wg.Done()

	t := time.NewTimer(c.batchTimeout)
	defer t.Stop()

	buf := make([]model.Event, 0, c.batchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		c.flushWithRetry("logs", len(buf), func() error { return c.flushLogs(buf) })
		buf = buf[:0]
	}

	for {
		select {
		case <-c.stop:
			flush()
			return
		case ev := <-c.logCh:
			buf = append(buf, ev)
			if len(buf) >= c.batchSize {
				flush()
				resetTimer(t, c.batchTimeout)
			}
		case <-t.C:
			flush()
			resetTimer(t, c.batchTimeout)
		}
	}
}

func (c *Client) runErrors() {
	defer c.wg.Done()

	t := time.NewTimer(c.batchTimeout)
	defer t.Stop()

	buf := make([]issue.ErrorOccurrence, 0, c.batchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		c.flushWithRetry("errors", len(buf), func() error { return c.flushErrors(buf) })
		buf = buf[:0]
	}

	for {
		select {
		case <-c.stop:
			flush()
			return
		case occ := <-c.errCh:
			buf = append(buf, occ)
			if len(buf) >= c.batchSize {
				flush()
				resetTimer(t, c.batchTimeout)
			}
		case <-t.C:
			flush()
			resetTimer(t, c.batchTimeout)
		}
	}
}

func (c *Client) flushLogs(events []model.Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO logs_raw (
			ts, service, env, level, logger, message,
			trace_id, span_id, request_id, project_id,
			http_remote_addr, http_server_addr,
			attrs
		)
	`)
	if err != nil {
		return err
	}
	for _, ev := range events {
		var httpRemoteAddr, httpServerAddr string
		if ev.HTTP != nil {
			httpRemoteAddr = ev.HTTP.RemoteAddr
			httpServerAddr = ev.HTTP.ServerAddr
		}
		if err := batch.Append(
			ev.Timestamp.UTC(),
			ev.Service,
			ev.Env,
			ev.Level,
			ev.Logger,
			ev.Message,
			ev.TraceID,
			ev.SpanID,
			ev.RequestID,
			ev.ProjectID,
			httpRemoteAddr,
			httpServerAddr,
			map[string]string(ev.Attrs),
		); err != nil {
			_ = batch.Abort()
			return err
		}
	}
	return batch.Send()
}

func (c *Client) flushErrors(occs []issue.ErrorOccurrence) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO error_occurrences (
			ts, issue_id, service, env, project_id,
			exception_type, exception_message, stack_fingerprint, title,
			stack_frames,
			trace_id, span_id, request_id,
			http_method, http_route, http_status, http_user_agent, http_remote_addr, http_server_addr,
			user_id_hash, attrs
		)
	`)
	if err != nil {
		return err
	}

	for _, occ := range occs {
		stackFrames := make([]any, 0, len(occ.StackFrames))
		for _, fr := range occ.StackFrames {
			stackFrames = append(stackFrames, map[string]any{
				"filename":  fr.Filename,
				"lineno":    fr.Lineno,
				"function":  fr.Function,
				"context":   fr.Context,
				"is_in_app": fr.IsInApp,
			})
		}

		if err := batch.Append(
			occ.TS.UTC(),
			occ.IssueID,
			occ.Service,
			occ.Env,
			occ.ProjectID,
			occ.ExceptionType,
			occ.ExceptionMessage,
			occ.StackFingerprint,
			occ.Title,
			stackFrames,
			occ.TraceID,
			occ.SpanID,
			occ.RequestID,
			occ.HTTPMethod,
			occ.HTTPRoute,
			occ.HTTPStatus,
			occ.HTTPUserAgent,
			occ.HTTPRemoteAddr,
			occ.HTTPServerAddr,
			occ.UserIDHash,
			occ.Attrs,
		); err != nil {
			_ = batch.Abort()
			return err
		}
	}

	return batch.Send()
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

type ProjectRow struct {
	ID              string `ch:"id"`
	Name            string `ch:"name"`
	MaxEventsPerSec uint32 `ch:"max_events_per_second"`
	MaxStorageGB    uint32 `ch:"max_storage_gb"`
	APIKeyHash      string `ch:"api_key_hash"`
}

func (c *Client) GetProjectByAPIKeyHash(ctx context.Context, apiKeyHash string) (ProjectRow, bool, error) {
	if apiKeyHash == "" {
		return ProjectRow{}, false, nil
	}
	// projects is append-only; always select the latest version per id so rotated keys
	// invalidate old keys.
	// Note: ClickHouse can reject argMax(...) usage directly in HAVING (ILLEGAL_AGGREGATION) in some versions.
	// Use a subquery and filter on the computed value instead.
	q := `
		SELECT
			id, name, max_events_per_second, max_storage_gb, api_key_hash
		FROM (
			SELECT
				id,
				argMax(name, updated_at) as name,
				argMax(max_events_per_second, updated_at) as max_events_per_second,
				argMax(max_storage_gb, updated_at) as max_storage_gb,
				argMax(api_key_hash, updated_at) as api_key_hash
			FROM projects
			GROUP BY id
		)
		WHERE api_key_hash = ?
		LIMIT 1
	`
	var rows []ProjectRow
	if err := c.conn.Select(ctx, &rows, q, apiKeyHash); err != nil {
		return ProjectRow{}, false, err
	}
	if len(rows) == 0 {
		return ProjectRow{}, false, nil
	}
	return rows[0], true, nil
}

type issueSummaryRow struct {
	IssueID          string    `ch:"issue_id"`
	Service          string    `ch:"service"`
	Env              string    `ch:"env"`
	ProjectID        string    `ch:"project_id"`
	Title            string    `ch:"title"`
	Status           string    `ch:"status"`
	StatusUpdatedAt  time.Time `ch:"status_updated_at"`
	FirstSeen        time.Time `ch:"first_seen"`
	LastSeen         time.Time `ch:"last_seen"`
	OccurrenceCount  uint64    `ch:"occurrence_count"`
	AffectedUsers    uint32    `ch:"affected_users"`
	ExceptionType    string    `ch:"exception_type"`
	StackFingerprint string    `ch:"stack_fingerprint"`
	TopFilename      string    `ch:"top_frame_filename"`
	TopLineno        uint32    `ch:"top_frame_lineno"`
	TopFunction      string    `ch:"top_frame_function"`
}

func (c *Client) GetIssueSummary(ctx context.Context, projectID, issueID string) (api.IssueSummary, bool, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" {
		return api.IssueSummary{}, false, errors.New("missing project_id/issue_id")
	}

	q := `
		SELECT
			s.issue_id,
			s.service,
			s.env,
			s.project_id,
			s.title,
			coalesce(st.status, 'open') as status,
			coalesce(st.updated_at, toDateTime64(0, 3, 'UTC')) as status_updated_at,
			s.first_seen,
			s.last_seen,
			s.occurrence_count,
			s.affected_users,
			s.exception_type,
			s.stack_fingerprint,
			s.top_frame_filename,
			s.top_frame_lineno,
			s.top_frame_function
		FROM issues_stats s
		LEFT JOIN (
			SELECT
				project_id, service, env, issue_id,
				argMax(status, ist.updated_at) as status,
				max(ist.updated_at) as updated_at
			FROM issues_state AS ist
			GROUP BY project_id, service, env, issue_id
		) st
		ON st.project_id = s.project_id
		AND st.service = s.service
		AND st.env = s.env
		AND st.issue_id = s.issue_id
		WHERE s.project_id = ?
		AND s.issue_id = ?
		ORDER BY s.last_seen DESC
		LIMIT 1
	`

	rows := make([]issueSummaryRow, 0, 1)
	if err := c.conn.Select(ctx, &rows, q, projectID, issueID); err != nil {
		return api.IssueSummary{}, false, err
	}
	if len(rows) == 0 {
		return api.IssueSummary{}, false, nil
	}

	r := rows[0]
	isRegressed := false
	// Compute regression: if the issue is marked resolved but we have new occurrences after status_updated_at.
	if strings.EqualFold(r.Status, "resolved") && r.LastSeen.After(r.StatusUpdatedAt) {
		isRegressed = true
	}

	return api.IssueSummary{
		IssueID:          r.IssueID,
		Service:          r.Service,
		Env:              r.Env,
		ProjectID:        r.ProjectID,
		Title:            r.Title,
		Status:           r.Status,
		IsRegressed:      isRegressed,
		StatusUpdatedAt:  r.StatusUpdatedAt,
		FirstSeen:        r.FirstSeen,
		LastSeen:         r.LastSeen,
		OccurrenceCount:  r.OccurrenceCount,
		AffectedUsers:    r.AffectedUsers,
		ExceptionType:    r.ExceptionType,
		TopFilename:      r.TopFilename,
		TopLineno:        r.TopLineno,
		TopFunction:      r.TopFunction,
		StackFingerprint: r.StackFingerprint,
	}, true, nil
}

func (c *Client) ListIssues(ctx context.Context, projectID, service, env string, from, to time.Time, limit int) ([]api.IssueSummary, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("missing project_id")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := `
		SELECT
			s.issue_id,
			s.service,
			s.env,
			s.project_id,
			s.title,
			coalesce(st.status, 'open') as status,
			coalesce(st.updated_at, toDateTime64(0, 3, 'UTC')) as status_updated_at,
			s.first_seen,
			s.last_seen,
			s.occurrence_count,
			s.affected_users,
			s.exception_type,
			s.stack_fingerprint,
			s.top_frame_filename,
			s.top_frame_lineno,
			s.top_frame_function
		FROM issues_stats s
		LEFT JOIN (
			SELECT
				project_id, service, env, issue_id,
				argMax(status, ist.updated_at) as status,
				max(ist.updated_at) as updated_at
			FROM issues_state AS ist
			GROUP BY project_id, service, env, issue_id
		) st
		ON st.project_id = s.project_id
		AND st.service = s.service
		AND st.env = s.env
		AND st.issue_id = s.issue_id
		WHERE s.project_id = ?
	`

	rows := make([]issueSummaryRow, 0, limit)
	args := make([]any, 0, 6)
	args = append(args, projectID)
	if service != "" {
		q += " AND s.service = ?\n"
		args = append(args, service)
	}
	if env != "" {
		q += " AND s.env = ?\n"
		args = append(args, env)
	}
	if !from.IsZero() {
		q += " AND s.last_seen >= ?\n"
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		q += " AND s.last_seen < ?\n"
		args = append(args, to.UTC())
	}
	q += " ORDER BY s.last_seen DESC\n LIMIT ?\n"
	args = append(args, limit)

	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}

	out := make([]api.IssueSummary, 0, len(rows))
	for _, r := range rows {
		isRegressed := false
		// Compute regression: if the issue is marked resolved but we have new occurrences after status_updated_at.
		if strings.EqualFold(r.Status, "resolved") && r.LastSeen.After(r.StatusUpdatedAt) {
			isRegressed = true
		}
		out = append(out, api.IssueSummary{
			IssueID:          r.IssueID,
			Service:          r.Service,
			Env:              r.Env,
			ProjectID:        r.ProjectID,
			Title:            r.Title,
			Status:           r.Status,
			IsRegressed:      isRegressed,
			StatusUpdatedAt:  r.StatusUpdatedAt,
			FirstSeen:        r.FirstSeen,
			LastSeen:         r.LastSeen,
			OccurrenceCount:  r.OccurrenceCount,
			AffectedUsers:    r.AffectedUsers,
			ExceptionType:    r.ExceptionType,
			TopFilename:      r.TopFilename,
			TopLineno:        r.TopLineno,
			TopFunction:      r.TopFunction,
			StackFingerprint: r.StackFingerprint,
		})
	}
	return out, nil
}

func (c *Client) UpsertIssueState(ctx context.Context, projectID, service, env, issueID, status, updatedBy, comment string) error {
	now := time.Now().UTC()
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(service) == "" || strings.TrimSpace(env) == "" || strings.TrimSpace(issueID) == "" {
		return errors.New("missing project_id/service/env/issue_id")
	}
	if status == "" {
		status = "open"
	}
	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO issues_state (
			issue_id, service, env, project_id,
			status, is_regressed, regressed_from,
			updated_at, updated_by, comment
		)
	`)
	if err != nil {
		return err
	}
	if err := batch.Append(
		issueID,
		service,
		env,
		projectID,
		status,
		uint8(0),
		"",
		now,
		updatedBy,
		comment,
	); err != nil {
		_ = batch.Abort()
		return err
	}
	return batch.Send()
}

type occurrenceRow struct {
	TS               time.Time         `ch:"ts"`
	IssueID          string            `ch:"issue_id"`
	Service          string            `ch:"service"`
	Env              string            `ch:"env"`
	ProjectID        string            `ch:"project_id"`
	Title            string            `ch:"title"`
	ExceptionType    string            `ch:"exception_type"`
	ExceptionMessage string            `ch:"exception_message"`
	StackFingerprint string            `ch:"stack_fingerprint"`
	StackFrames      []map[string]any  `ch:"stack_frames"`
	TraceID          string            `ch:"trace_id"`
	RequestID        string            `ch:"request_id"`
	HTTPMethod       string            `ch:"http_method"`
	HTTPRoute        string            `ch:"http_route"`
	HTTPStatus       uint16            `ch:"http_status"`
	UserIDHash       string            `ch:"user_id_hash"`
	Attrs            map[string]string `ch:"attrs"`
}

func (c *Client) ListIssueOccurrences(ctx context.Context, projectID, issueID string, from, to time.Time, limit int) ([]api.ErrorOccurrence, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" {
		return nil, errors.New("missing project_id/issue_id")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := `
		SELECT
			ts, issue_id, service, env, project_id,
			title, exception_type, exception_message, stack_fingerprint,
			stack_frames,
			trace_id, request_id,
			http_method, http_route, http_status,
			user_id_hash, attrs
		FROM error_occurrences
		WHERE project_id = ?
		  AND issue_id = ?
	`
	args := make([]any, 0, 5)
	args = append(args, projectID, issueID)
	if !from.IsZero() {
		q += " AND ts >= ?\n"
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		q += " AND ts < ?\n"
		args = append(args, to.UTC())
	}
	q += " ORDER BY ts DESC\n LIMIT ?\n"
	args = append(args, limit)

	rows := make([]occurrenceRow, 0, limit)
	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}

	out := make([]api.ErrorOccurrence, 0, len(rows))
	for _, r := range rows {
		out = append(out, api.ErrorOccurrence{
			TS:               r.TS,
			IssueID:          r.IssueID,
			Service:          r.Service,
			Env:              r.Env,
			ProjectID:        r.ProjectID,
			Title:            r.Title,
			ExceptionType:    r.ExceptionType,
			ExceptionMessage: r.ExceptionMessage,
			StackFingerprint: r.StackFingerprint,
			StackFrames:      r.StackFrames,
			TraceID:          r.TraceID,
			RequestID:        r.RequestID,
			HTTPMethod:       r.HTTPMethod,
			HTTPRoute:        r.HTTPRoute,
			HTTPStatus:       r.HTTPStatus,
			UserIDHash:       r.UserIDHash,
			Attrs:            r.Attrs,
		})
	}
	return out, nil
}

func optionalEq(name, v string) string {
	if v == "" {
		return ""
	}
	// parameter still passed, query references it only when non-empty
	return " AND " + name + " = {" + name + ":String}\n"
}
