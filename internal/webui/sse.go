package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pyloggle/internal/auth"
)

type ssePayload struct {
	ChangedAt string `json:"changed_at"`
}

func (u *UI) sseIssues(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.ProjectFromContext(r.Context())
	q := r.URL.Query()
	service := strings.TrimSpace(q.Get("service"))
	env := strings.TrimSpace(q.Get("env"))

	changedAtFn := func(ctx context.Context) (time.Time, error) {
		return u.ch.IssuesChangedAt(ctx, p.ID, service, env)
	}
	u.serveSSE(w, r, changedAtFn)
}

func (u *UI) sseLogs(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.ProjectFromContext(r.Context())
	qp := r.URL.Query()

	service := strings.TrimSpace(qp.Get("service"))
	env := strings.TrimSpace(qp.Get("env"))
	level := strings.TrimSpace(qp.Get("level"))
	search := strings.TrimSpace(qp.Get("q"))
	traceID := strings.TrimSpace(qp.Get("trace_id"))
	requestID := strings.TrimSpace(qp.Get("request_id"))

	from := parseTimeUI(qp.Get("from"))
	to := parseTimeUI(qp.Get("to"))

	changedAtFn := func(ctx context.Context) (time.Time, error) {
		return u.ch.LogsChangedAt(ctx, p.ID, service, env, level, search, traceID, requestID, from, to)
	}
	u.serveSSE(w, r, changedAtFn)
}

func (u *UI) sseDashboards(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.ProjectFromContext(r.Context())
	qp := r.URL.Query()

	service := strings.TrimSpace(qp.Get("service"))
	env := strings.TrimSpace(qp.Get("env"))
	from := parseTimeUI(qp.Get("from"))
	to := parseTimeUI(qp.Get("to"))

	changedAtFn := func(ctx context.Context) (time.Time, error) {
		return u.ch.DashboardsChangedAt(ctx, p.ID, service, env, from, to)
	}
	u.serveSSE(w, r, changedAtFn)
}

func (u *UI) serveSSE(w http.ResponseWriter, r *http.Request, changedAtFn func(context.Context) (time.Time, error)) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")

	// Prime the cursor.
	last, _ := func() (time.Time, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		return changedAtFn(ctx)
	}()

	// Send an initial "hello" so the client can confirm connectivity quickly.
	_, _ = fmt.Fprintf(w, "event: hello\ndata: %s\n\n", mustJSON(ssePayload{ChangedAt: last.UTC().Format(time.RFC3339Nano)}))
	fl.Flush()

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			// SSE comment line, ignored by the client but keeps proxies from buffering/closing.
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		case <-tick.C:
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			cur, err := changedAtFn(ctx)
			cancel()
			if err != nil {
				// Don't kill the stream for transient DB errors; just retry next tick.
				continue
			}
			if !cur.IsZero() && cur.After(last) {
				last = cur
				_, _ = fmt.Fprintf(w, "event: update\ndata: %s\n\n", mustJSON(ssePayload{ChangedAt: last.UTC().Format(time.RFC3339Nano)}))
				fl.Flush()
			}
		}
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}


