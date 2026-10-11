package clickhouse

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

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

	flushLogsReq chan chan struct{}
	flushErrsReq chan chan struct{}

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
		flushLogsReq: make(chan chan struct{}),
		flushErrsReq: make(chan chan struct{}),
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

// InsertLog enqueues a log row. Non-error rows never block; errors wait briefly.
func (c *Client) InsertLog(ctx context.Context, ev model.Event) error {
	select {
	case c.logCh <- ev:
		return nil
	default:
		if ev.IsError() {
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

// InsertErrorOccurrence enqueues an occurrence; errors are high-signal so we allow brief blocking.
func (c *Client) InsertErrorOccurrence(ctx context.Context, occ issue.ErrorOccurrence) error {
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

// EnqueueLog / EnqueueErrorOccurrence make *Client usable directly as an ingest sink.
func (c *Client) EnqueueLog(ctx context.Context, ev model.Event) error {
	return c.InsertLog(ctx, ev)
}

func (c *Client) EnqueueErrorOccurrence(ctx context.Context, occ issue.ErrorOccurrence) error {
	return c.InsertErrorOccurrence(ctx, occ)
}

// Flush writes everything queued so far and waits for it (tests, graceful shutdown).
func (c *Client) Flush(ctx context.Context) error {
	for _, req := range []chan chan struct{}{c.flushLogsReq, c.flushErrsReq} {
		done := make(chan struct{})
		select {
		case req <- done:
		case <-c.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
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
	drain := func() {
		for {
			select {
			case ev := <-c.logCh:
				buf = append(buf, ev)
				if len(buf) >= c.batchSize {
					flush()
				}
			default:
				flush()
				return
			}
		}
	}

	for {
		select {
		case <-c.stop:
			drain()
			return
		case ev := <-c.logCh:
			buf = append(buf, ev)
			if len(buf) >= c.batchSize {
				flush()
				resetTimer(t, c.batchTimeout)
			}
		case done := <-c.flushLogsReq:
			drain()
			close(done)
			resetTimer(t, c.batchTimeout)
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
	drain := func() {
		for {
			select {
			case occ := <-c.errCh:
				buf = append(buf, occ)
				if len(buf) >= c.batchSize {
					flush()
				}
			default:
				flush()
				return
			}
		}
	}

	for {
		select {
		case <-c.stop:
			drain()
			return
		case occ := <-c.errCh:
			buf = append(buf, occ)
			if len(buf) >= c.batchSize {
				flush()
				resetTimer(t, c.batchTimeout)
			}
		case done := <-c.flushErrsReq:
			drain()
			close(done)
			resetTimer(t, c.batchTimeout)
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
			ts, event_id, project_id, service, env, level, logger, message,
			trace_id, span_id, request_id, issue_id, release, hostname, source,
			http_method, http_route, http_status, http_url, http_remote_addr, http_server_addr,
			duration_ms, user_id_hash, exception_type, attrs, tags
		)
	`)
	if err != nil {
		return err
	}
	for _, ev := range events {
		var h model.HTTPInfo
		if ev.HTTP != nil {
			h = *ev.HTTP
		}
		var userHash, excType string
		if ev.User != nil {
			userHash = ev.User.IDHash
		}
		if ev.Exception != nil {
			excType = ev.Exception.Type
		}
		issueID := ev.Attrs.Get("pyloggle.issue_id")
		if err := batch.Append(
			ev.Timestamp.UTC(),
			ev.EventID,
			ev.ProjectID,
			ev.Service,
			ev.Env,
			ev.Level,
			ev.Logger,
			ev.Message,
			ev.TraceID,
			ev.SpanID,
			ev.RequestID,
			issueID,
			ev.Release,
			ev.Hostname,
			ev.Source,
			h.Method,
			h.Route,
			h.Status,
			h.URL,
			h.RemoteAddr,
			h.ServerAddr,
			float32(ev.DurationMS),
			userHash,
			excType,
			nonNilMap(ev.Attrs),
			nonNilMap(ev.Tags),
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
			ts, event_id, project_id, service, env, issue_id,
			level, logger, message,
			exception_type, exception_module, exception_message, stack_fingerprint, fingerprint_version, title, culprit,
			stack_frames, frames_json, chain_json, notes, breadcrumbs_json,
			trace_id, span_id, request_id,
			http_method, http_route, http_status, http_url, http_query, http_headers, http_body,
			http_user_agent, http_remote_addr, http_server_addr, http_client_ip,
			user_id_hash, user_id, user_email, user_username, user_ip,
			release, hostname, runtime_name, runtime_version, runtime_platform, runtime_pid, runtime_thread,
			sdk_name, sdk_version, source, mechanism_type, mechanism_handled,
			tags, attrs, extra_json
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
		handled := uint8(1)
		if !occ.MechanismHandled {
			handled = 0
		}
		notes := occ.Notes
		if notes == nil {
			notes = []string{}
		}
		if err := batch.Append(
			occ.TS.UTC(),
			occ.EventID,
			occ.ProjectID,
			occ.Service,
			occ.Env,
			occ.IssueID,
			occ.Level,
			occ.Logger,
			occ.Message,
			occ.ExceptionType,
			occ.ExceptionModule,
			occ.ExceptionMessage,
			occ.StackFingerprint,
			occ.FingerprintVersion,
			occ.Title,
			occ.Culprit,
			stackFrames,
			jsonOrEmpty(occ.Frames),
			jsonOrEmpty(occ.Chain),
			notes,
			jsonOrEmpty(occ.Breadcrumbs),
			occ.TraceID,
			occ.SpanID,
			occ.RequestID,
			occ.HTTP.Method,
			occ.HTTP.Route,
			occ.HTTP.Status,
			occ.HTTP.URL,
			occ.HTTP.Query,
			nonNilMap(occ.HTTP.Headers),
			string(occ.HTTP.Body),
			occ.HTTP.UserAgent,
			occ.HTTP.RemoteAddr,
			occ.HTTP.ServerAddr,
			occ.HTTP.ClientIP,
			occ.User.IDHash,
			occ.User.ID,
			occ.User.Email,
			occ.User.Username,
			occ.User.IP,
			occ.Release,
			occ.Hostname,
			occ.Runtime.Name,
			occ.Runtime.Version,
			occ.Runtime.Platform,
			uint32(occ.Runtime.PID),
			occ.Runtime.Thread,
			occ.SDK.Name,
			occ.SDK.Version,
			occ.Source,
			occ.MechanismType,
			handled,
			nonNilMap(occ.Tags),
			nonNilMap(occ.Attrs),
			occ.Extra,
		); err != nil {
			_ = batch.Abort()
			return err
		}
	}

	return batch.Send()
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// jsonOrEmpty serializes v, or returns "" for nil/empty slices so the column stays small.
func jsonOrEmpty(v any) string {
	switch x := v.(type) {
	case []model.Frame:
		if len(x) == 0 {
			return ""
		}
	case []model.ChainedException:
		if len(x) == 0 {
			return ""
		}
	case []model.Breadcrumb:
		if len(x) == 0 {
			return ""
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
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
