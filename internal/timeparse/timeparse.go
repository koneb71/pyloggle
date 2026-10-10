// Package timeparse parses the many time formats accepted by query parameters:
// RFC3339 (any zone), zone-less ISO forms (assumed UTC), "YYYY-MM-DD HH:MM:SS",
// unix seconds/millis/micros/nanos, and relative forms ("now", "-15m", "-2h", "-1d", "-1w").
package timeparse

import (
	"math"
	"strconv"
	"strings"
	"time"
)

var absoluteLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// Parse returns the parsed time in UTC and true, or (zero, false) if s is empty or unparseable.
func Parse(s string) (time.Time, bool) {
	return ParseAt(s, time.Now())
}

// ParseAt is Parse with an explicit "now" for relative forms.
func ParseAt(s string, now time.Time) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if strings.EqualFold(s, "now") {
		return now.UTC(), true
	}
	if d, ok := parseRelative(s); ok {
		return now.Add(d).UTC(), true
	}
	if t, ok := FromUnix(s); ok {
		return t, true
	}
	for _, layout := range absoluteLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// ParseAbsolute is ParseAt without the relative forms ("now", "-15m"): the right choice for
// timestamps carried on the wire, where only absolute or numeric values make sense.
func ParseAbsolute(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, ok := FromUnix(s); ok {
		return t, true
	}
	for _, layout := range absoluteLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// FromUnix interprets a numeric string as a unix timestamp, picking the unit by magnitude:
// seconds (< 1e11), milliseconds (< 1e15), microseconds (< 1e18), else nanoseconds.
func FromUnix(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n <= 0 {
			return time.Time{}, false
		}
		return FromUnixInt(n), true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		if f <= 0 {
			return time.Time{}, false
		}
		return FromUnixFloat(f), true
	}
	return time.Time{}, false
}

// FromUnixInt picks the unit by magnitude (see FromUnix).
func FromUnixInt(n int64) time.Time {
	switch {
	case n < 1e11:
		return time.Unix(n, 0).UTC()
	case n < 1e15:
		return time.UnixMilli(n).UTC()
	case n < 1e18:
		return time.UnixMicro(n).UTC()
	default:
		return time.Unix(0, n).UTC()
	}
}

// FromUnixFloat picks the unit by magnitude and keeps the fractional part.
func FromUnixFloat(f float64) time.Time {
	switch {
	case f < 1e11:
		sec := math.Floor(f)
		return time.Unix(int64(sec), int64((f-sec)*1e9)).UTC()
	case f < 1e15:
		return time.UnixMicro(int64(f * 1e3)).UTC()
	case f < 1e18:
		return time.UnixMicro(int64(f)).UTC()
	default:
		return time.Unix(0, int64(f)).UTC()
	}
}

func parseRelative(s string) (time.Duration, bool) {
	if len(s) < 2 || s[0] != '-' {
		return 0, false
	}
	body := s[1:]
	unit := body[len(body)-1]
	numStr := body[:len(body)-1]
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	var mult time.Duration
	switch unit {
	case 's':
		mult = time.Second
	case 'm':
		mult = time.Minute
	case 'h':
		mult = time.Hour
	case 'd':
		mult = 24 * time.Hour
	case 'w':
		mult = 7 * 24 * time.Hour
	default:
		return 0, false
	}
	return -time.Duration(n * float64(mult)), true
}

// ParseRange resolves a (from, to) pair: an empty "to" means now, an empty "from" means to-def,
// and an inverted pair is swapped. Both results are UTC.
func ParseRange(from, to string, def time.Duration, now time.Time) (time.Time, time.Time) {
	if def <= 0 {
		def = time.Hour
	}
	t, ok := ParseAt(to, now)
	if !ok {
		t = now.UTC()
	}
	f, ok := ParseAt(from, now)
	if !ok {
		f = t.Add(-def)
	}
	if f.After(t) {
		f, t = t, f
	}
	return f, t
}

// ParseEndExclusive parses an end bound; a minute-precision input (datetime-local without
// seconds) is advanced by one minute so the minute itself is included in [from, to).
func ParseEndExclusive(s string, now time.Time) (time.Time, bool) {
	t, ok := ParseAt(s, now)
	if !ok {
		return t, false
	}
	s = strings.TrimSpace(s)
	if len(s) == len("2006-01-02T15:04") && (s[10] == 'T' || s[10] == ' ') {
		return t.Add(time.Minute), true
	}
	return t, true
}
