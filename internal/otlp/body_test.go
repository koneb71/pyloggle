package otlp

import (
	"strings"
	"testing"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/koneb71/pyloggle/internal/model"
)

// serverSpan is one server span carrying the request body the Python package puts on it.
func serverSpan(body string, truncated bool, status tracev1.Status_StatusCode) *tracev1.TracesData {
	str := func(k, v string) *commonv1.KeyValue {
		return &commonv1.KeyValue{Key: k, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: v}}}
	}
	attrs := []*commonv1.KeyValue{str("http.request.method", "POST"), str("http.route", "/signup"), str("http.request.body", body)}
	if truncated {
		attrs = append(attrs, &commonv1.KeyValue{Key: "http.request.body.truncated", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}}})
	}
	start := uint64(1_760_000_000_000_000_000)
	return &tracev1.TracesData{ResourceSpans: []*tracev1.ResourceSpans{{
		Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{str("service.name", "shop")}},
		ScopeSpans: []*tracev1.ScopeSpans{{Spans: []*tracev1.Span{{
			TraceId: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, SpanId: []byte{1, 2, 3, 4, 5, 6, 7, 8},
			Name: "POST /signup", Kind: tracev1.Span_SPAN_KIND_SERVER, StartTimeUnixNano: start, EndTimeUnixNano: start + 3_000_000,
			Status: &tracev1.Status{Code: status}, Attributes: attrs,
		}}}},
	}}}
}

func TestServerSpanBodyReachesTheRequestLineAndTheSpan(t *testing.T) {
	body := `{"email":"ada@example.com","password":"[REDACTED]"}`
	events, spans := TracesToEventsAndSpans(serverSpan(body, true, tracev1.Status_STATUS_CODE_OK), true, time.Now())
	if len(events) != 1 || events[0].HTTP == nil || string(events[0].HTTP.Body) != body {
		t.Fatalf("request line body: %+v", events)
	}
	if events[0].Attrs.Get("http.request.body") != "" || events[0].Attrs.Get(model.BodyTruncatedAttr) != "true" {
		t.Fatalf("body belongs in http.body, the cut mark in attrs: %v", events[0].Attrs)
	}
	if len(spans) != 1 || spans[0].Attrs["http.request.body"] != body {
		t.Fatalf("span keeps the body for the step panel: %v", spans[0].Attrs)
	}

	// A failed request keeps its body too: it becomes the error's Web request.
	events, _ = TracesToEventsAndSpans(serverSpan(body, false, tracev1.Status_STATUS_CODE_ERROR), true, time.Now())
	if len(events) != 1 || events[0].HTTP == nil || string(events[0].HTTP.Body) != body || events[0].Attrs.Get(model.BodyTruncatedAttr) != "" {
		t.Fatalf("failed request body: %+v", events)
	}
}

func TestSpanBodyIsCapped(t *testing.T) {
	huge := strings.Repeat("é", model.MaxHTTPBodyBytes) // two bytes each: twice the cap
	_, spans := TracesToEventsAndSpans(serverSpan(huge, false, tracev1.Status_STATUS_CODE_OK), true, time.Now())
	got := spans[0].Attrs["http.request.body"]
	if len(got) > model.MaxHTTPBodyBytes || len(got) < model.MaxHTTPBodyBytes-1 || !strings.HasPrefix(huge, got) {
		t.Fatalf("span body not cut on a character boundary at the cap: %d bytes", len(got))
	}
	if spans[0].Attrs[model.BodyTruncatedAttr] != "true" {
		t.Fatalf("cut span body must be marked: %v", spans[0].Attrs[model.BodyTruncatedAttr])
	}
}

func TestPreflightAndHeadGetNoRequestLine(t *testing.T) {
	for _, method := range []string{"OPTIONS", "HEAD", "GET"} {
		td := serverSpan("", false, tracev1.Status_STATUS_CODE_OK)
		sp := td.ResourceSpans[0].ScopeSpans[0].Spans[0]
		sp.Attributes[0] = &commonv1.KeyValue{Key: "http.request.method", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: method}}}
		events, spans := TracesToEventsAndSpans(td, true, time.Now())
		if len(spans) != 1 {
			t.Fatalf("%s: the span itself is always kept", method)
		}
		if want := map[string]int{"OPTIONS": 0, "HEAD": 0, "GET": 1}[method]; len(events) != want {
			t.Fatalf("%s: %d request lines, want %d", method, len(events), want)
		}
	}
}
