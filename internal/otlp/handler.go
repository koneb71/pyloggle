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
	"strconv"
	"strings"
	"time"

	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/metrics"
	"pyloggle/internal/model"
)

const maxBody = 32 << 20

// Handler serves POST /v1/logs, /v1/traces and /v1/metrics.
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

func (k kind) door() string {
	switch k {
	case kindTraces:
		return "otlp_traces"
	case kindMetrics:
		return "otlp_metrics"
	}
	return "otlp_logs"
}

func (h *Handler) handler(k kind) http.Handler {
	door := k.door()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		status := http.StatusOK
		defer func() { metrics.IngestRequests.Inc(door, strconv.Itoa(status)) }()
		t, err := h.p.Authenticate(r.Context(), r)
		if err != nil {
			if errors.Is(err, ingest.ErrUnauthorized) {
				status = http.StatusUnauthorized
				http.Error(w, "unauthorized", status)
				return
			}
			if errors.Is(err, ingest.ErrInsufficientScope) {
				status = http.StatusForbidden
				http.Error(w, err.Error(), status)
				return
			}
			status = http.StatusInternalServerError
			http.Error(w, "auth lookup failed", status)
			return
		}
		ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
		isJSON := ct == "application/json"
		if !isJSON && ct != "application/x-protobuf" && ct != "application/protobuf" && ct != "" {
			status = http.StatusUnsupportedMediaType
			http.Error(w, "unsupported content type", status)
			return
		}
		body, err := readBody(r)
		if err != nil {
			status = http.StatusBadRequest
			http.Error(w, "bad body: "+err.Error(), status)
			return
		}
		bad := func(err error) {
			status = http.StatusBadRequest
			metrics.IngestRejected.Inc(door)
			http.Error(w, "bad payload: "+err.Error(), status)
		}
		var events []model.Event
		var spans []model.Span
		var points []model.MetricPoint
		switch k {
		case kindLogs:
			var ld logsv1.LogsData
			if err := decode(body, isJSON, &ld); err != nil {
				bad(err)
				return
			}
			events = LogsToEvents(&ld, h.now())
		case kindTraces:
			var td tracev1.TracesData
			if err := decode(body, isJSON, &td); err != nil {
				bad(err)
				return
			}
			events, spans = TracesToEventsAndSpans(&td, h.cfg.OTLPStoreServerSpans, h.now())
			if !h.cfg.OTLPStoreSpans {
				spans = nil
			}
		case kindMetrics:
			var md metricsv1.MetricsData
			if err := decode(body, isJSON, &md); err != nil {
				bad(err)
				return
			}
			if h.cfg.OTLPStoreMetrics {
				points = MetricsToPoints(&md, h.now())
			}
		}
		accepted := 0
		for _, ev := range events {
			if o := h.p.Process(r.Context(), t, ev, len(body)/max(len(events), 1)); o == ingest.Accepted {
				accepted++
			}
		}
		if len(spans) > 0 {
			rawLen := len(body)
			if len(events) > 0 {
				rawLen = 0 // the events already paid for the request bytes
			}
			n, _ := h.p.ProcessSpans(r.Context(), t, spans, rawLen)
			accepted += n
		}
		if len(points) > 0 {
			n, _ := h.p.ProcessMetrics(r.Context(), t, points, len(body))
			accepted += n
		}
		metrics.IngestBytes.Add(float64(len(body)), t.ProjectID, door)
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
