package model

import (
	"encoding/json"
	"time"
)

// Span is one OpenTelemetry span as stored in the spans table. Every kind is kept; the
// server-side HTTP columns are filled from the semantic-convention attributes so route
// statistics and the waterfall do not have to parse the attribute map.
type Span struct {
	ProjectID string `json:"project_id"`
	Service   string `json:"service"`
	Env       string `json:"env"`
	Release   string `json:"release,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	Scope     string `json:"scope,omitempty"` // instrumentation scope name

	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`   // unspecified | internal | server | client | producer | consumer
	Status       string `json:"status"` // unset | ok | error
	StatusMsg    string `json:"status_message,omitempty"`

	StartTS    time.Time `json:"start_ts"`
	DurationUS uint64    `json:"duration_us"`

	HTTPMethod string `json:"http_method,omitempty"`
	HTTPRoute  string `json:"http_route,omitempty"`
	HTTPStatus uint16 `json:"http_status,omitempty"`

	Attrs  Attrs       `json:"attrs,omitempty"`
	Events []SpanEvent `json:"events,omitempty"`
}

// SpanEvent is a timestamped annotation on a span (exceptions, log points).
type SpanEvent struct {
	TS    time.Time `json:"ts"`
	Name  string    `json:"name"`
	Attrs Attrs     `json:"attrs,omitempty"`
}

// MaxSpanEventsBytes caps the JSON-encoded events of one span.
const MaxSpanEventsBytes = 64 << 10

// EventsJSON encodes the events, dropping trailing events once the cap is exceeded (the
// span then carries attrs["pyloggle.events_truncated"] = "true").
func (s *Span) EventsJSON() string {
	if len(s.Events) == 0 {
		return ""
	}
	b, err := json.Marshal(s.Events)
	if err != nil {
		return ""
	}
	for len(b) > MaxSpanEventsBytes && len(s.Events) > 1 {
		s.Events = s.Events[:len(s.Events)-1]
		s.Attrs = s.Attrs.Set("pyloggle.events_truncated", "true")
		b, _ = json.Marshal(s.Events)
	}
	if len(b) > MaxSpanEventsBytes {
		s.Events = nil
		s.Attrs = s.Attrs.Set("pyloggle.events_truncated", "true")
		return ""
	}
	return string(b)
}

// EndTS is the span end.
func (s Span) EndTS() time.Time { return s.StartTS.Add(time.Duration(s.DurationUS) * time.Microsecond) }

// Span kinds and statuses as stored (Enum8 names in ClickHouse).
var (
	SpanKinds    = []string{"unspecified", "internal", "server", "client", "producer", "consumer"}
	SpanStatuses = []string{"unset", "ok", "error"}
)
