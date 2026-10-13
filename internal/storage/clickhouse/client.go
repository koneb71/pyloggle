package clickhouse

import (
	"context"
	"encoding/json"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/koneb71/pyloggle/internal/issue"
	"github.com/koneb71/pyloggle/internal/model"
)

// Client is a thin wrapper around ClickHouse operations we need.
type Client struct {
	conn ch.Conn

	logs   *batcher[model.Event]
	errs   *batcher[issue.ErrorOccurrence]
	spans  *batcher[model.Span]
	points *batcher[model.MetricPoint]

	batchSize    int
	batchTimeout time.Duration
	queueSize    int
}

// Stats reports batching health for /healthz?verbose=1 and the System page. The four legacy
// counters stay for compatibility; Queues carries every write queue.
type Stats struct {
	DroppedLogs   uint64         `json:"dropped_logs"`
	DroppedErrors uint64         `json:"dropped_errors"`
	QueuedLogs    int            `json:"queued_logs"`
	QueuedErrors  int            `json:"queued_errors"`
	Queues        []BatcherStats `json:"queues"`
}

func (c *Client) Stats() Stats {
	ls, es, ss, ps := c.logs.stats(), c.errs.stats(), c.spans.stats(), c.points.stats()
	return Stats{
		DroppedLogs:   ls.Dropped,
		DroppedErrors: es.Dropped,
		QueuedLogs:    ls.Depth,
		QueuedErrors:  es.Depth,
		Queues:        []BatcherStats{ls, es, ss, ps},
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

	c := &Client{conn: conn, batchSize: batchSize, batchTimeout: batchTimeout, queueSize: maxQueueSize}
	c.logs = newBatcher("logs", maxQueueSize, batchSize, batchTimeout, c.flushLogs)
	c.errs = newBatcher("errors", maxQueueSize, batchSize, batchTimeout, c.flushErrors)
	c.spans = newBatcher("spans", maxQueueSize*4, batchSize*4, batchTimeout, c.flushSpans)
	c.points = newBatcher("metrics", maxQueueSize*4, batchSize*4, batchTimeout, c.flushPoints)
	return c, nil
}

func (c *Client) batchers() []interface{ close() } {
	return []interface{ close() }{c.logs, c.errs, c.spans, c.points}
}

func (c *Client) Close() error {
	for _, b := range c.batchers() {
		b.close()
	}
	return c.conn.Close()
}

// InsertLog enqueues a log row. Non-error rows never block; errors wait briefly.
func (c *Client) InsertLog(ctx context.Context, ev model.Event) error {
	wait := time.Duration(0)
	if ev.IsError() {
		wait = 250 * time.Millisecond
	}
	err := c.logs.enqueue(ctx, ev, wait)
	if err == errQueueFull && !ev.IsError() {
		return nil // low-signal rows are dropped silently (counted in stats)
	}
	return err
}

// InsertErrorOccurrence enqueues an occurrence; errors are high-signal so we allow brief blocking.
func (c *Client) InsertErrorOccurrence(ctx context.Context, occ issue.ErrorOccurrence) error {
	return c.errs.enqueue(ctx, occ, 750*time.Millisecond)
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
	if err := c.logs.flushNow(ctx); err != nil {
		return err
	}
	if err := c.errs.flushNow(ctx); err != nil {
		return err
	}
	if err := c.spans.flushNow(ctx); err != nil {
		return err
	}
	return c.points.flushNow(ctx)
}

func (c *Client) flushLogs(ctx context.Context, events []model.Event) error {
	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO logs_raw (
			ts, event_id, project_id, service, env, level, logger, message,
			trace_id, span_id, request_id, issue_id, release, hostname, source,
			http_method, http_route, http_status, http_url, http_remote_addr, http_server_addr, http_body,
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
			string(h.Body),
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

func (c *Client) flushErrors(ctx context.Context, occs []issue.ErrorOccurrence) error {
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

type ProjectRow struct {
	ID              string `ch:"id"`
	Name            string `ch:"name"`
	MaxEventsPerSec uint32 `ch:"max_events_per_second"`
	MaxStorageGB    uint32 `ch:"max_storage_gb"`
	RetentionDays   uint16 `ch:"retention_days"`
	APIKeyHash      string `ch:"api_key_hash"`
}
