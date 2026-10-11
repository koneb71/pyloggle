package ingest

import (
	"bufio"
	"compress/gzip"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pyloggle/internal/config"
	"pyloggle/internal/model"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/tenant"
)

// Handler is the NDJSON door (POST /v1/ingest): one JSON event per line.
type Handler struct {
	p *Pipeline
}

// NewHandler wires the production pipeline: per-stream queues in front of the ClickHouse client.
func NewHandler(cfg config.Config, ch *clickhouse.Client, res *tenant.Resolver) *Handler {
	if res == nil {
		res = tenant.NewResolver(ch, 60*time.Second)
	}
	return &Handler{p: NewPipeline(cfg, newStreamRouter(cfg, ch), ch, res)}
}

// NewHandlerFromPipeline is used by tests and the OTLP door.
func NewHandlerFromPipeline(p *Pipeline) *Handler { return &Handler{p: p} }

// Pipeline returns the shared pipeline (the OTLP door and the UI reuse it).
func (h *Handler) Pipeline() *Pipeline { return h.p }

// maxLineBytes bounds one NDJSON line (stack traces with locals can be large).
const maxLineBytes = 4 * 1024 * 1024

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	t, err := h.p.Authenticate(r.Context(), r)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "auth lookup failed", http.StatusInternalServerError)
		return
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
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var (
		accepted, rejected, dropped int
		errs                        []string
		lineNo                      int
		outcomes                    = map[string]int{}
	)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lineNo++
		var ev model.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			rejected++
			if len(errs) < 5 {
				errs = append(errs, "line "+strconv.Itoa(lineNo)+": "+err.Error())
			}
			continue
		}
		switch o := h.p.Process(r.Context(), t, ev, len(line)); o {
		case Accepted:
			accepted++
		default:
			dropped++
			outcomes[o.String()]++
		}
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	resp := map[string]any{"accepted": accepted, "rejected": rejected}
	if dropped > 0 {
		resp["dropped"] = outcomes
	}
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

// secretEqual compares secrets in constant time; an empty configured secret never matches.
func secretEqual(provided, configured string) bool {
	if configured == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(configured)) == 1
}
