package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/model"
	"github.com/koneb71/pyloggle/internal/tenant"
)

// Every request is counted once: request lines (even the ones sampling drops), crashes (once,
// however many doors report them) and OpenTelemetry server spans that are not listed on Logs.
func TestPipelineCountsRequests(t *testing.T) {
	cfg := testConfig()
	cfg.SampleInfo = 0 // no info line is kept: the counts must not care
	sink := &memSink{}
	p := NewPipeline(cfg, sink, &memStates{}, nil)
	now := time.Date(2026, 10, 15, 4, 12, 31, 0, time.UTC)
	p.now = func() time.Time { return now }
	tn := Tenant{ProjectID: "p"}
	ctx := context.Background()

	line := model.Event{Timestamp: model.At(now), Service: "shop", Level: "info", Message: "request", DurationMS: 12,
		HTTP: &model.HTTPInfo{Method: "GET", Route: "/orders", Status: 200}, User: &model.UserInfo{IDHash: "u1"}}
	if o := p.Process(ctx, tn, line, 100); o != DroppedSampled {
		t.Fatalf("the line itself is sampled away: %v", o)
	}
	if len(sink.counts) != 1 || sink.counts[0].ProjectID != "p" || sink.counts[0].Env != cfg.DefaultEnv || sink.counts[0].Users[0] != "u1" {
		t.Fatalf("a sampled request line is still counted: %+v", sink.counts)
	}

	counted := line
	counted.CountOnly = true
	if o := p.Process(ctx, tn, counted, 100); o != Accepted || len(sink.counts) != 2 || len(sink.logs) != 0 {
		t.Fatalf("count-only: %v counts=%d logs=%d", o, len(sink.counts), len(sink.logs))
	}
	failed := counted
	failed.Level, failed.HTTP = "error", &model.HTTPInfo{Method: "GET", Route: "/orders", Status: 503}
	if o := p.Process(ctx, tn, failed, 100); o != Accepted || len(sink.occs) != 0 || len(sink.logs) != 0 || sink.counts[2].Errors != 1 {
		t.Fatalf("a count-only failure is counted, never an issue or a line: %v occs=%d", o, len(sink.occs))
	}

	frames := []model.Frame{{Filename: "/srv/app/x.py", Function: "f", Lineno: 1}}
	crash := model.Event{Service: "shop", Level: "error", Message: "boom", TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		HTTP: &model.HTTPInfo{Method: "POST", Route: "/pay", Status: 500}, Exception: &model.Exception{Type: "ValueError", Frames: frames}}
	again := crash
	again.Source, again.DurationMS = "otlp", 40
	p.Process(ctx, tn, crash, 100)
	p.Process(ctx, tn, again, 100)
	var pay int
	for _, c := range sink.counts {
		if c.Route == "/pay" {
			pay++
		}
	}
	if pay != 1 || len(sink.occs) != 1 {
		t.Fatalf("one crash reported twice is one request: counted %d, occurrences %d", pay, len(sink.occs))
	}

	p.Process(ctx, tn, model.Event{Service: "shop", Level: "info", Message: "hello"}, 100)
	if len(sink.counts) != 4 {
		t.Fatalf("a plain message is not a request: %d", len(sink.counts))
	}
}

func TestCountsDoor(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRPS, cfg.RateLimitBurst = 1, 1
	cfg.DefaultProjID = "" // no key, no project
	sink := &memSink{}
	p := NewPipeline(cfg, sink, nil, memResolver{keys: map[string]tenant.Project{"k1": tenant.Trusted(tenant.Project{ID: "proj1", MaxEventsPerSec: 1})}})
	h := NewHandlerFromPipeline(p).Counts()

	post := func(body string, gz bool, key string) (int, map[string]any) {
		t.Helper()
		var b bytes.Buffer
		if gz {
			w := gzip.NewWriter(&b)
			_, _ = w.Write([]byte(body))
			_ = w.Close()
		} else {
			b.WriteString(body)
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/ingest/requests", &b)
		if gz {
			req.Header.Set("Content-Encoding", "gzip")
		}
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec.Code, resp
	}

	lines := strings.Join([]string{
		`{"minute":"2026-10-15T04:12:00Z","service":"shop","env":"prod","release":"1.4.0","method":"POST","route":"/orders","requests":3,"errors":1,"durations_ms":[12.5,8.1,30],"users":["u1","u2"]}`,
		`{"minute":"2026-10-15T04:12:00Z","service":"shop","method":"GET","route":"/","requests":0}`,
		`not json`,
		`{"method":"GET","route":"/health","requests":2}`,
	}, "\n")
	if code, _ := post(lines, false, ""); code != http.StatusUnauthorized {
		t.Fatalf("a key is required: %d", code)
	}
	code, resp := post(lines, true, "k1")
	if code != http.StatusAccepted || resp["accepted"] != float64(2) || resp["rejected"] != float64(2) {
		t.Fatalf("door: %d %v", code, resp)
	}
	if len(sink.counts) != 2 || sink.counts[0].ProjectID != "proj1" || sink.counts[0].Requests != 3 || len(sink.counts[0].DurationsMS) != 3 || sink.counts[1].Service != cfg.DefaultSvc || sink.counts[1].Minute.IsZero() {
		t.Fatalf("stored counts: %+v", sink.counts)
	}
	// One rate-limit token per batch and app: shop's burst of 1 is spent.
	code, resp = post(`{"service":"shop","method":"GET","route":"/","requests":1}`, false, "k1")
	if code != http.StatusAccepted || resp["accepted"] != float64(0) || resp["dropped"] == nil {
		t.Fatalf("rate limited batch: %d %v", code, resp)
	}
}
