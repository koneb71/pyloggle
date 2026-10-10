package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"pyloggle/internal/timeparse"
)

// Timestamp is a lenient time.Time for the wire format. It accepts RFC3339 with any zone,
// zone-less ISO forms (assumed UTC — what the v0.1 logging handler sent), "YYYY-MM-DD HH:MM:SS",
// and JSON numbers as unix seconds/millis/micros/nanos. Empty and null decode to the zero time.
// Embedding time.Time keeps every existing call site (UTC(), IsZero(), ...) working.
type Timestamp struct{ time.Time }

func (t *Timestamp) UnmarshalJSON(b []byte) error {
	s := bytes.TrimSpace(b)
	if len(s) == 0 || bytes.Equal(s, []byte("null")) {
		t.Time = time.Time{}
		return nil
	}
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(s, &str); err != nil {
			return err
		}
		if strings.TrimSpace(str) == "" {
			t.Time = time.Time{}
			return nil
		}
		parsed, ok := timeparse.ParseAbsolute(str)
		if !ok {
			return fmt.Errorf("model: cannot parse timestamp %q", str)
		}
		t.Time = parsed
		return nil
	}
	parsed, ok := timeparse.FromUnix(string(s))
	if !ok {
		return fmt.Errorf("model: cannot parse timestamp %s", s)
	}
	t.Time = parsed
	return nil
}

// MarshalJSON always emits RFC3339 with millisecond precision in UTC ("" for the zero time).
func (t Timestamp) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(t.UTC().Format("2006-01-02T15:04:05.000Z07:00"))
}

// At wraps a time.Time.
func At(t time.Time) Timestamp { return Timestamp{Time: t} }

// Attrs is a string map on the Go side that decodes any JSON object: scalars are stringified
// (true -> "true", 12 -> "12", 1.5 -> "1.5"), null values are dropped, nested objects and arrays
// are kept as compact JSON. A non-object value (or garbage) decodes to an empty map rather than
// rejecting the whole event.
type Attrs map[string]string

func (a *Attrs) UnmarshalJSON(b []byte) error {
	s := bytes.TrimSpace(b)
	if len(s) == 0 || bytes.Equal(s, []byte("null")) {
		*a = nil
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(s, &raw); err != nil {
		*a = nil
		return nil
	}
	out := make(Attrs, len(raw))
	for k, v := range raw {
		if str, ok := StringifyJSON(v); ok {
			out[k] = str
		}
	}
	*a = out
	return nil
}

// Get returns the value for key or "" (nil-safe).
func (a Attrs) Get(key string) string {
	if a == nil {
		return ""
	}
	return a[key]
}

// Set assigns a value, allocating the map if needed, and returns the (possibly new) map.
func (a Attrs) Set(key, value string) Attrs {
	if a == nil {
		a = Attrs{}
	}
	a[key] = value
	return a
}

// JSONText keeps a JSON value as text: strings verbatim, anything else compact JSON.
// Used for http.body (the v0.1 SDK sent decoded JSON objects) and free-form extra context.
type JSONText string

func (j *JSONText) UnmarshalJSON(b []byte) error {
	str, ok := StringifyJSON(b)
	if !ok {
		*j = ""
		return nil
	}
	*j = JSONText(str)
	return nil
}

func (j JSONText) MarshalJSON() ([]byte, error) { return json.Marshal(string(j)) }

// StringifyJSON renders one JSON value as a string: strings are unquoted, null returns ok=false,
// objects/arrays are compacted, numbers and booleans are kept as written.
func StringifyJSON(v []byte) (string, bool) {
	v = bytes.TrimSpace(v)
	if len(v) == 0 || bytes.Equal(v, []byte("null")) {
		return "", false
	}
	switch v[0] {
	case '"':
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return s, true
		}
		return string(v), true
	case '{', '[':
		var buf bytes.Buffer
		if err := json.Compact(&buf, v); err == nil {
			return buf.String(), true
		}
		return string(v), true
	default:
		return string(v), true
	}
}

// Canonical levels. Everything on the read and write paths speaks these five.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
	LevelFatal = "fatal"
)

// NormalizeLevel maps the many spellings seen in the wild (stdlib logging, syslog, OTEL) onto
// the canonical set. Unknown custom levels are kept, lowercased, so no information is lost.
func NormalizeLevel(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch l {
	case "", "info", "information", "informational", "notice":
		return LevelInfo
	case "debug", "trace", "verbose", "fine", "finer", "finest":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error", "err", "severe":
		return LevelError
	case "fatal", "critical", "crit", "emergency", "emerg", "alert", "panic":
		return LevelFatal
	}
	return l
}

// IsErrorLevel reports whether a (normalized) level counts as an error for sampling, quotas and
// issue creation.
func IsErrorLevel(level string) bool {
	switch NormalizeLevel(level) {
	case LevelError, LevelFatal:
		return true
	}
	return false
}

// LevelAliases returns every stored spelling that should match a canonical level, so queries
// over rows written by older SDKs still match ("warn" also finds "warning").
func LevelAliases(level string) []string {
	switch NormalizeLevel(level) {
	case LevelDebug:
		return []string{"debug", "trace", "verbose"}
	case LevelInfo:
		return []string{"info", "notice", ""}
	case LevelWarn:
		return []string{"warn", "warning"}
	case LevelError:
		return []string{"error", "err"}
	case LevelFatal:
		return []string{"fatal", "critical"}
	}
	return []string{strings.ToLower(strings.TrimSpace(level))}
}
