package clickhouse

import (
	"context"
	"errors"
	"strings"
	"time"

	"pyloggle/internal/api"
)

type logRow struct {
	TS        time.Time         `ch:"ts"`
	Service   string            `ch:"service"`
	Env       string            `ch:"env"`
	Level     string            `ch:"level"`
	Logger    string            `ch:"logger"`
	Message   string            `ch:"message"`
	TraceID   string            `ch:"trace_id"`
	SpanID    string            `ch:"span_id"`
	RequestID string            `ch:"request_id"`
	ProjectID string            `ch:"project_id"`
	Attrs     map[string]string `ch:"attrs"`
}

func (c *Client) ListLogs(ctx context.Context, projectID, service, env, level, q, traceID, requestID string, from, to time.Time, limit int) ([]api.LogEvent, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("missing project_id")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-1 * time.Hour)
	}

	qry := `
		SELECT
			ts, service, env, level, logger, message,
			trace_id, span_id, request_id, project_id, attrs
		FROM logs_raw
		WHERE project_id = ?
		  AND ts >= ?
		  AND ts < ?
	`

	rows := make([]logRow, 0, limit)
	args := make([]any, 0, 10)
	args = append(args, projectID, from.UTC(), to.UTC())
	if service != "" {
		qry += " AND service = ?\n"
		args = append(args, service)
	}
	if env != "" {
		qry += " AND env = ?\n"
		args = append(args, env)
	}
	if level != "" {
		qry += " AND level = ?\n"
		args = append(args, level)
	}
	if traceID != "" {
		qry += " AND trace_id = ?\n"
		args = append(args, traceID)
	}
	if requestID != "" {
		qry += " AND request_id = ?\n"
		args = append(args, requestID)
	}
	if q != "" {
		qry += " AND positionCaseInsensitiveUTF8(message, ?) > 0\n"
		args = append(args, q)
	}
	qry += " ORDER BY ts DESC\n LIMIT ?\n"
	args = append(args, limit)

	if err := c.conn.Select(ctx, &rows, qry, args...); err != nil {
		return nil, err
	}

	out := make([]api.LogEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, api.LogEvent{
			TS:        r.TS,
			Service:   r.Service,
			Env:       r.Env,
			Level:     r.Level,
			Logger:    r.Logger,
			Message:   r.Message,
			TraceID:   r.TraceID,
			SpanID:    r.SpanID,
			RequestID: r.RequestID,
			ProjectID: r.ProjectID,
			Attrs:     r.Attrs,
		})
	}
	return out, nil
}
