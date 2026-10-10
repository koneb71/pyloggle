package ingest

import (
	"bufio"
	"compress/gzip"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"pyloggle/internal/config"
	"pyloggle/internal/issue"
	"pyloggle/internal/model"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/tenant"
)

type Handler struct {
	cfg config.Config
	ch  *clickhouse.Client
	lim *limiter
	res *tenant.Resolver
	rt  *streamRouter
	qt  *quotaTracker
}

func NewHandler(cfg config.Config, ch *clickhouse.Client, res *tenant.Resolver) *Handler {
	if res == nil {
		res = tenant.NewResolver(ch, 60*time.Second)
	}
	return &Handler{
		cfg: cfg,
		ch:  ch,
		lim: newLimiter(cfg),
		res: res,
		rt:  newStreamRouter(cfg, ch),
		qt:  newQuotaTracker(),
	}
}

// secretEqual compares secrets in constant time; an empty configured secret never matches.
func secretEqual(provided, configured string) bool {
	if configured == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(configured)) == 1
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Multi-tenant: require a valid API key if provided; otherwise fall back to default project.
	// If you want to require keys always, set PYLOGGLE_DEFAULT_PROJECT_ID empty and do not send events without a key.
	projectID := h.cfg.DefaultProjID
	rps := h.cfg.RateLimitRPS
	burst := h.cfg.RateLimitBurst
	var maxStorageGB uint32

	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" && !secretEqual(apiKey, h.cfg.SharedSecret) {
		p, ok, err := h.res.ResolveByAPIKey(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "auth lookup failed", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		projectID = p.ID
		maxStorageGB = p.MaxStorageGB
		if p.MaxEventsPerSec > 0 {
			rps = float64(p.MaxEventsPerSec)
			// simple burst heuristic if not configured
			if burst <= 0 {
				burst = rps * 2
			}
			if burst < rps {
				burst = rps
			}
		}
	}

	body, closer, err := maybeGzipBody(r)
	if err != nil {
		http.Error(w, "bad gzip body", http.StatusBadRequest)
		return
	}
	if closer != nil {
		defer closer.Close()
	}

	sc := bufio.NewScanner(body)
	// allow larger lines (stack traces can be big)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		now      = time.Now().UTC()
		n        int
		rejected int
		errs     []string
	)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var ev model.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Count and report rejected lines instead of dropping them silently.
			rejected++
			if len(errs) < 5 {
				errs = append(errs, "line "+itoa(n+rejected)+": "+err.Error())
			}
			continue
		}
		ev.Normalize()
		ev.Source = "sdk"
		// Normalize commonly used structured fields into attrs so downstream consumers (UI)
		// can display them without needing new API fields.
		if ev.HTTP != nil {
			if ev.HTTP.RemoteAddr != "" {
				ev.Attrs = ev.Attrs.Set("http.remote_addr", ev.HTTP.RemoteAddr)
			}
			if ev.HTTP.ServerAddr != "" {
				ev.Attrs = ev.Attrs.Set("http.server_addr", ev.HTTP.ServerAddr)
			}
		}
		if ev.Timestamp.IsZero() {
			ev.Timestamp = model.At(now)
		}
		if ev.Service == "" {
			ev.Service = h.cfg.DefaultSvc
		}
		if ev.Env == "" {
			ev.Env = h.cfg.DefaultEnv
		}
		// Enforce project_id from resolved tenant (or default).
		ev.ProjectID = projectID

		// Quota protection (coarse daily budget): drop non-errors when over budget.
		if !h.qt.allow(projectID, isErrorEvent(ev), len(line), maxStorageGB, now) {
			continue
		}

		// Resource protection: apply rate limiting + sampling for non-errors.
		// Errors (level=error or exception present) are always kept.
		if h.lim.shouldDrop(ev, rps, burst) {
			continue
		}

		// Always store raw log row.
		_ = h.rt.EnqueueLog(r.Context(), ev)

		// Store error occurrence if exception present (or level=error/fatal), unless the
		// producer flagged the line as a duplicate of an already-reported exception.
		if ev.IsError() && !ev.SkipIssue {
			occ := issue.FromEvent(ev)
			_ = h.rt.EnqueueErrorOccurrence(r.Context(), occ)
		}

		n++
	}

	if err := sc.Err(); err != nil && err != io.EOF {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	resp := map[string]any{"accepted": n, "rejected": rejected}
	if len(errs) > 0 {
		resp["errors"] = errs
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func maybeGzipBody(r *http.Request) (io.Reader, io.Closer, error) {
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, nil, err
		}
		return gr, gr, nil
	}
	return r.Body, nil, nil
}

func itoa(n int) string {
	// tiny local helper to avoid pulling in strconv everywhere; ok for this one spot.
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
