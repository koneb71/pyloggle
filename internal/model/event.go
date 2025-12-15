package model

import "time"

// Event is the wire format accepted by /v1/ingest (one JSON object per line).
// The Python SDK will emit this shape.
type Event struct {
	Timestamp time.Time `json:"ts"`

	Service   string `json:"service"`
	Env       string `json:"env"`
	ProjectID string `json:"project_id"`

	Level     string `json:"level"`  // error,warn,info,debug
	Logger    string `json:"logger"` // logger name
	Message   string `json:"message"`
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id"`
	RequestID string `json:"request_id"`

	Attrs map[string]string `json:"attrs"`

	Exception *Exception `json:"exception,omitempty"`
	HTTP      *HTTPInfo  `json:"http,omitempty"`
	User      *UserInfo  `json:"user,omitempty"`
}

type Exception struct {
	Type    string  `json:"type"`
	Message string  `json:"message"`
	Frames  []Frame `json:"frames"`
}

type Frame struct {
	Filename string `json:"filename"`
	Lineno   uint32 `json:"lineno"`
	Function string `json:"function"`
	Context  string `json:"context"`
	InApp    bool   `json:"in_app"`
}

type HTTPInfo struct {
	Method     string `json:"method"`
	Route      string `json:"route"`
	Status     uint16 `json:"status"`
	UserAgent  string `json:"user_agent"`
	RemoteAddr string `json:"remote_addr"`
}

type UserInfo struct {
	IDHash string `json:"id_hash"` // already hashed in SDK
}


