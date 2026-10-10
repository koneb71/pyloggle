// Package model defines the wire format accepted by POST /v1/ingest (one JSON object per line)
// and produced by the OTLP mapping. Version 2 is a strict superset of what the 0.1 Python SDK
// sends: every v0.1 line still decodes (see testdata/sdk_v0.1.ndjson and event_test.go).
package model

// Event is one log line or error occurrence.
type Event struct {
	// EventID is a client-generated uuid4 hex; the server fills it in when empty.
	EventID   string    `json:"event_id,omitempty"`
	Timestamp Timestamp `json:"ts"`

	Service     string `json:"service"`
	Env         string `json:"env"`
	Environment string `json:"environment,omitempty"` // alias for env (OTEL wording); used when Env is empty
	ProjectID   string `json:"project_id"`            // always overwritten by the resolved tenant
	Release     string `json:"release,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	Transaction string `json:"transaction,omitempty"` // route / task name when not an HTTP request

	Level     string `json:"level"` // canonical: debug, info, warn, error, fatal (see NormalizeLevel)
	Logger    string `json:"logger"`
	Message   string `json:"message"`
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id"`
	RequestID string `json:"request_id"`

	Attrs       Attrs    `json:"attrs"`
	Tags        Attrs    `json:"tags,omitempty"`
	Extra       JSONText `json:"extra,omitempty"`       // free-form context (SDK set_context), kept as JSON text
	Fingerprint []string `json:"fingerprint,omitempty"` // grouping override; "{{ default }}" expands to the server default
	DurationMS  float64  `json:"duration_ms,omitempty"`

	Exception   *Exception   `json:"exception,omitempty"`
	HTTP        *HTTPInfo    `json:"http,omitempty"`
	User        *UserInfo    `json:"user,omitempty"`
	Runtime     *RuntimeInfo `json:"runtime,omitempty"`
	SDK         *SDKInfo     `json:"sdk,omitempty"`
	Breadcrumbs []Breadcrumb `json:"breadcrumbs,omitempty"`
	Mechanism   *Mechanism   `json:"mechanism,omitempty"`

	// SkipIssue stores the line as a log row only. The SDK sets it on log records that carry an
	// exception it already reported through another path, so the log stays complete without
	// creating a second (message-fingerprinted) issue.
	SkipIssue bool `json:"skip_issue,omitempty"`

	// Source is set by the server: "sdk" or "otlp".
	Source string `json:"-"`
}

// Exception is the raised exception. Frames are oldest-first (Python traceback order), so the
// crash site is the LAST frame; the server walks them reversed to find the culprit.
type Exception struct {
	Type    string  `json:"type"`
	Module  string  `json:"module,omitempty"`
	Message string  `json:"message"`
	Frames  []Frame `json:"frames"`
	// Chain lists the exceptions this one was raised from, nearest first
	// (__cause__ / __context__ in Python).
	Chain []ChainedException `json:"chain,omitempty"`
	Notes []string           `json:"notes,omitempty"`
}

// ChainedException is one link in an exception chain (or one member of an exception group).
type ChainedException struct {
	Type     string   `json:"type"`
	Module   string   `json:"module,omitempty"`
	Message  string   `json:"message"`
	Frames   []Frame  `json:"frames"`
	IsCause  bool     `json:"is_cause"`           // true: explicit "raise ... from" (direct cause)
	Relation string   `json:"relation,omitempty"` // cause | context | group
	Notes    []string `json:"notes,omitempty"`
}

// Frame is one stack frame.
type Frame struct {
	Filename    string   `json:"filename"`
	AbsPath     string   `json:"abs_path,omitempty"`
	Module      string   `json:"module,omitempty"`
	Function    string   `json:"function"`
	Lineno      uint32   `json:"lineno"`
	Colno       uint32   `json:"colno,omitempty"`
	Context     string   `json:"context"`
	PreContext  []string `json:"pre_context,omitempty"`
	PostContext []string `json:"post_context,omitempty"`
	Vars        Attrs    `json:"vars,omitempty"`
	// InApp is nil when the producer did not say; the server then applies its path heuristic.
	// An explicit value from the SDK is honored.
	InApp *bool `json:"in_app"`
}

// IsInApp resolves the in-app flag with the server heuristic as fallback.
func (f Frame) IsInApp(heuristic func(filename string) bool) bool {
	if f.InApp != nil {
		return *f.InApp
	}
	if heuristic == nil {
		return false
	}
	return heuristic(f.Filename)
}

// HTTPInfo describes the request during which the event happened.
type HTTPInfo struct {
	Method     string   `json:"method"`
	Route      string   `json:"route"`
	Status     uint16   `json:"status"`
	URL        string   `json:"url,omitempty"`
	Query      string   `json:"query,omitempty"`
	Headers    Attrs    `json:"headers,omitempty"` // scrubbed client-side
	Body       JSONText `json:"body,omitempty"`    // opt-in; v0.1 sent decoded JSON objects
	UserAgent  string   `json:"user_agent"`
	RemoteAddr string   `json:"remote_addr"`
	ServerAddr string   `json:"server_addr"`
	ClientIP   string   `json:"client_ip,omitempty"`
}

// UserInfo identifies the affected user. IDHash is always set by the SDK; the raw fields are
// only sent when the SDK is configured with send_default_pii.
type UserInfo struct {
	IDHash   string `json:"id_hash"`
	ID       string `json:"id,omitempty"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
	IP       string `json:"ip,omitempty"`
}

// RuntimeInfo describes the process that produced the event.
type RuntimeInfo struct {
	Name     string `json:"name,omitempty"`     // e.g. "CPython"
	Version  string `json:"version,omitempty"`  // e.g. "3.12.4"
	Platform string `json:"platform,omitempty"` // e.g. "Linux-6.8-x86_64"
	PID      int    `json:"pid,omitempty"`
	Thread   string `json:"thread,omitempty"`
}

// SDKInfo names the producer.
type SDKInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Breadcrumb is one entry of the trail that led to an error event.
type Breadcrumb struct {
	TS       Timestamp `json:"ts"`
	Type     string    `json:"type,omitempty"`     // default | http | log | navigation | query
	Category string    `json:"category,omitempty"` // logger name, "http.request", ...
	Level    string    `json:"level,omitempty"`
	Message  string    `json:"message"`
	Data     Attrs     `json:"data,omitempty"`
}

// Mechanism says how the exception was captured; Handled nil means true.
type Mechanism struct {
	Type    string `json:"type"`
	Handled *bool  `json:"handled"`
}

// IsHandled resolves the nil default.
func (m *Mechanism) IsHandled() bool {
	if m == nil || m.Handled == nil {
		return true
	}
	return *m.Handled
}

// Normalize applies the aliases and canonical forms every producer path relies on. It is
// idempotent and never fails: ingest calls it right after decoding.
func (e *Event) Normalize() {
	if e.Env == "" && e.Environment != "" {
		e.Env = e.Environment
	}
	e.Level = NormalizeLevel(e.Level)
	if e.Exception != nil {
		if e.Exception.Type == "" {
			e.Exception.Type = "Exception"
		}
	}
	for i := range e.Breadcrumbs {
		if e.Breadcrumbs[i].Level != "" {
			e.Breadcrumbs[i].Level = NormalizeLevel(e.Breadcrumbs[i].Level)
		}
	}
}

// IsError reports whether the event should become an error occurrence candidate.
func (e *Event) IsError() bool {
	return e.Exception != nil || IsErrorLevel(e.Level)
}
