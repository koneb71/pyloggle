package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/issue"
	"pyloggle/internal/model"
)

func TestParsePythonTracebackChain(t *testing.T) {
	raw, _ := os.ReadFile("testdata/logs_python_exception.json")
	var ld logsv1.LogsData
	if err := protojson.Unmarshal(raw, &ld); err != nil {
		t.Fatal(err)
	}
	var stack string
	for _, kv := range ld.ResourceLogs[0].ScopeLogs[0].LogRecords[1].Attributes {
		if kv.Key == "exception.stacktrace" {
			stack = kv.Value.GetStringValue()
		}
	}
	typ, msg, frames, chain, ok := ParsePythonTraceback(stack)
	if !ok || typ != "app.errors.PaymentError" || msg != "card declined" {
		t.Fatalf("top: %q %q ok=%v", typ, msg, ok)
	}
	if len(frames) != 2 || frames[0].Function != "charge" || frames[0].Lineno != 88 || frames[1].Context != `raise PaymentError("card declined") from exc` {
		t.Fatalf("frames: %+v", frames)
	}
	if frames[0].Context != "gateway.call(order)" {
		t.Fatalf("caret line must be skipped, got context %q", frames[0].Context)
	}
	if len(chain) != 1 || chain[0].Type != "httpx.ConnectError" || !chain[0].IsCause || len(chain[0].Frames) != 2 || chain[0].Frames[1].Filename != "/usr/lib/python3.12/site-packages/httpx/_client.py" {
		t.Fatalf("chain: %+v", chain)
	}
	if chain[0].Message != "[Errno 111] Connection refused" {
		t.Fatalf("cause message: %q", chain[0].Message)
	}
	// During-handling relation and an exception without message.
	typ, msg, frames, chain, ok = ParsePythonTraceback("Traceback (most recent call last):\n  File \"a.py\", line 1, in f\n    x()\nValueError\n\nDuring handling of the above exception, another exception occurred:\n\nTraceback (most recent call last):\n  File \"a.py\", line 3, in f\n    y()\nRuntimeError: second\n")
	if !ok || typ != "RuntimeError" || msg != "second" || len(chain) != 1 || chain[0].IsCause || chain[0].Relation != "context" || chain[0].Type != "ValueError" || chain[0].Message != "" {
		t.Fatalf("context chain: %q %q %+v", typ, msg, chain)
	}
	_ = frames
	if _, _, _, _, ok := ParsePythonTraceback("java.lang.NullPointerException\n\tat com.x.Y.z(Y.java:10)"); ok {
		t.Fatalf("non-python stack must not parse")
	}
}

func TestLogsMapping(t *testing.T) {
	raw, _ := os.ReadFile("testdata/logs_python_exception.json")
	var ld logsv1.LogsData
	if err := protojson.Unmarshal(raw, &ld); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, 1, 1, 0, 0, 10, 0, time.UTC)
	evs := LogsToEvents(&ld, now)
	if len(evs) != 3 {
		t.Fatalf("events: %d", len(evs))
	}
	info := evs[0]
	if info.Service != "otel-fastapi" || info.Env != "staging" || info.Release != "2.1.0" || info.Hostname != "web-7" || info.Source != "otlp" {
		t.Fatalf("resource: %+v", info)
	}
	if info.Level != "info" || info.Logger != "app.orders" || info.Message != "order received" || info.Attrs["order_id"] != "17" || info.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || info.SpanID != "00f067aa0ba902b7" {
		t.Fatalf("info record: %+v", info)
	}
	if info.Runtime == nil || info.Runtime.Version != "3.12.4" || info.Runtime.PID != 4242 || info.SDK == nil || info.SDK.Version != "1.27.0" {
		t.Fatalf("runtime/sdk: %+v %+v", info.Runtime, info.SDK)
	}
	if !info.Timestamp.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ts: %v", info.Timestamp)
	}
	errEv := evs[1]
	if errEv.Level != "error" || errEv.Exception == nil || errEv.Exception.Type != "PaymentError" || errEv.Exception.Module != "app.errors" || errEv.Exception.Message != "card declined" {
		t.Fatalf("exception: %+v", errEv.Exception)
	}
	if len(errEv.Exception.Frames) != 2 || len(errEv.Exception.Chain) != 1 || errEv.Mechanism == nil || errEv.Mechanism.IsHandled() {
		t.Fatalf("frames/chain/mechanism: %+v %+v", errEv.Exception, errEv.Mechanism)
	}
	if errEv.HTTP == nil || errEv.HTTP.Method != "POST" || errEv.HTTP.Route != "/orders/{id}/charge" || errEv.HTTP.Status != 502 || errEv.HTTP.ClientIP != "10.0.0.9" || errEv.HTTP.URL == "" {
		t.Fatalf("http: %+v", errEv.HTTP)
	}
	if errEv.User == nil || errEv.User.ID != "u-17" || errEv.RequestID != "req-abc" {
		t.Fatalf("user/request: %+v %q", errEv.User, errEv.RequestID)
	}
	if _, leaked := errEv.Attrs["exception.stacktrace"]; leaked {
		t.Fatalf("parsed stack must not be duplicated in attrs")
	}
	// Observed time + kvlist body + warn severity.
	third := evs[2]
	if third.Level != "warn" || third.Message != `{"k":"v"}` || !third.Timestamp.Equal(time.Date(2025, 1, 1, 0, 0, 2, 0, time.UTC)) {
		t.Fatalf("third: %+v", third)
	}
	// Through the issue grouper the exception produces a stable issue with the right culprit.
	occ := issue.FromEvent(errEv)
	if occ.Culprit != "app/gateway.py in call" || occ.ExceptionType != "PaymentError" {
		t.Fatalf("culprit: %q type %q", occ.Culprit, occ.ExceptionType)
	}
}

func TestTracesMapping(t *testing.T) {
	raw, _ := os.ReadFile("testdata/traces_exception.json")
	var td tracev1.TracesData
	if err := protojson.Unmarshal(raw, &td); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	evs := TracesToEvents(&td, false, now)
	if len(evs) != 2 {
		t.Fatalf("without server spans: %d events (want exception + error span)", len(evs))
	}
	exc := evs[0]
	if exc.Exception == nil || exc.Exception.Type != "KeyError" || exc.Level != "error" || exc.HTTP == nil || exc.HTTP.Status != 500 || exc.Transaction != "POST /jobs" || exc.DurationMS != 250 {
		t.Fatalf("exception event: %+v http=%+v", exc, exc.HTTP)
	}
	if len(exc.Exception.Frames) != 1 || exc.Exception.Frames[0].Function != "create" {
		t.Fatalf("frames: %+v", exc.Exception.Frames)
	}
	failed := evs[1]
	if failed.Level != "error" || failed.Message != "connection reset" || failed.Exception != nil {
		t.Fatalf("error span: %+v", failed)
	}
	evs = TracesToEvents(&td, true, now)
	if len(evs) != 3 {
		t.Fatalf("with server spans: %d", len(evs))
	}
	req := evs[1]
	if req.Message != "request" || req.Level != "info" || req.Attrs["latency_ms"] != "5" || req.HTTP == nil || req.HTTP.Status != 200 {
		t.Fatalf("request log: %+v", req)
	}
}

type memSink struct {
	mu   sync.Mutex
	logs []model.Event
	occs []issue.ErrorOccurrence
}

func (m *memSink) EnqueueLog(_ context.Context, ev model.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, ev)
	return nil
}
func (m *memSink) EnqueueErrorOccurrence(_ context.Context, occ issue.ErrorOccurrence) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.occs = append(m.occs, occ)
	return nil
}

func TestHandlerJSONProtobufGzip(t *testing.T) {
	cfg := config.FromEnv()
	cfg.DefaultProjID = "default"
	cfg.SampleInfo, cfg.SampleDebug = 1, 1
	sink := &memSink{}
	h := New(ingest.NewPipeline(cfg, sink, nil, nil), cfg)

	raw, _ := os.ReadFile("testdata/logs_python_exception.json")
	// JSON
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Logs().ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "{}" {
		t.Fatalf("json: %d %s", rec.Code, rec.Body.String())
	}
	if len(sink.logs) != 3 || len(sink.occs) != 1 || sink.occs[0].Source != "otlp" {
		t.Fatalf("stored: logs=%d occs=%d", len(sink.logs), len(sink.occs))
	}
	// Protobuf + gzip
	var ld logsv1.LogsData
	_ = protojson.Unmarshal(raw, &ld)
	pb, _ := proto.Marshal(&ld)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(pb)
	_ = zw.Close()
	req = httptest.NewRequest(http.MethodPost, "/v1/logs", &gz)
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "gzip")
	rec = httptest.NewRecorder()
	h.Logs().ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/x-protobuf" || rec.Header().Get("X-Pyloggle-Accepted") != "3" {
		t.Fatalf("protobuf: %d %v", rec.Code, rec.Header())
	}
	// Traces
	traw, _ := os.ReadFile("testdata/traces_exception.json")
	req = httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(traw))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Traces().ServeHTTP(rec, req)
	// two logs posts (json + protobuf) produced one occurrence each; traces add the exception
	// event and the failed span
	if rec.Code != 200 || len(sink.occs) != 4 {
		t.Fatalf("traces: %d occs=%d", rec.Code, len(sink.occs))
	}
	if sink.occs[2].TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || sink.occs[2].SpanID != "00f067aa0ba902b7" {
		t.Fatalf("ids must be hex: %q %q", sink.occs[2].TraceID, sink.occs[2].SpanID)
	}
	// Metrics are accepted and discarded; bad payloads are 400; wrong types 415.
	req = httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Metrics().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("metrics: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Logs().ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("bad json: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	h.Logs().ServeHTTP(rec, req)
	if rec.Code != 415 {
		t.Fatalf("content type: %d", rec.Code)
	}
}
