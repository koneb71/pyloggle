// Package otlp is the OpenTelemetry door: OTLP/HTTP logs and traces (protobuf or JSON) are
// mapped onto pyloggle events and pushed through the same ingest pipeline as the SDK.
// Only the data types are imported (logs/v1, trace/v1), so there is no gRPC dependency:
// LogsData / TracesData share the wire shape of the Export*ServiceRequest messages.
package otlp

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/model"
)

const maxBody = 32 << 20

// Handler serves POST /v1/logs, /v1/traces and /v1/metrics (accepted and discarded).
type Handler struct {
	p   *ingest.Pipeline
	cfg config.Config
	now func() time.Time
}

func New(p *ingest.Pipeline, cfg config.Config) *Handler {
	return &Handler{p: p, cfg: cfg, now: time.Now}
}

type kind int

const (
	kindLogs kind = iota
	kindTraces
	kindMetrics
)

func (h *Handler) Logs() http.Handler    { return h.handler(kindLogs) }
func (h *Handler) Traces() http.Handler  { return h.handler(kindTraces) }
func (h *Handler) Metrics() http.Handler { return h.handler(kindMetrics) }

func (h *Handler) handler(k kind) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		t, err := h.p.Authenticate(r.Context(), r)
		if err != nil {
			if errors.Is(err, ingest.ErrUnauthorized) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, "auth lookup failed", http.StatusInternalServerError)
			return
		}
		ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
		isJSON := ct == "application/json"
		if !isJSON && ct != "application/x-protobuf" && ct != "application/protobuf" && ct != "" {
			http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
			return
		}
		body, err := readBody(r)
		if err != nil {
			http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var events []model.Event
		switch k {
		case kindLogs:
			var ld logsv1.LogsData
			if err := decode(body, isJSON, &ld); err != nil {
				http.Error(w, "bad payload: "+err.Error(), http.StatusBadRequest)
				return
			}
			events = LogsToEvents(&ld, h.now())
		case kindTraces:
			var td tracev1.TracesData
			if err := decode(body, isJSON, &td); err != nil {
				http.Error(w, "bad payload: "+err.Error(), http.StatusBadRequest)
				return
			}
			events = TracesToEvents(&td, h.cfg.OTLPStoreServerSpans, h.now())
		case kindMetrics:
			// Accepted so exporters do not error; metrics are not stored.
		}
		accepted := 0
		for _, ev := range events {
			if o := h.p.Process(r.Context(), t, ev, len(body)/max(len(events), 1)); o == ingest.Accepted {
				accepted++
			}
		}
		// OTLP success: 200 with an empty Export*Response in the request's encoding.
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.Header().Set("X-Pyloggle-Accepted", itoa(accepted))
		w.WriteHeader(http.StatusOK)
	})
}

func readBody(r *http.Request) ([]byte, error) {
	var rd io.Reader = http.MaxBytesReader(nil, r.Body, maxBody)
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(rd)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		rd = gz
	}
	return io.ReadAll(rd)
}

func decode(body []byte, isJSON bool, m proto.Message) error {
	if isJSON {
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(body, m)
	}
	return proto.Unmarshal(body, m)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
