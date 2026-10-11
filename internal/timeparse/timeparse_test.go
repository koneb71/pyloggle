package timeparse

import (
	"testing"
	"time"
)

func TestParseForms(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"", time.Time{}, false},
		{"garbage", time.Time{}, false},
		{"now", now, true},
		{"-15m", now.Add(-15 * time.Minute), true},
		{"-2h", now.Add(-2 * time.Hour), true},
		{"-1d", now.Add(-24 * time.Hour), true},
		{"-1w", now.Add(-7 * 24 * time.Hour), true},
		{"2025-01-01T00:00:00Z", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"2025-01-01T02:00:00+02:00", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"2025-01-01T00:00:00.123Z", time.Date(2025, 1, 1, 0, 0, 0, 123e6, time.UTC), true},
		{"2025-01-01T00:00:00", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"2025-01-01T00:00", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"2025-01-01 00:00:00", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"2025-01-01", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"1735689600", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},    // unix seconds
		{"1735689600000", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true}, // unix millis (was rejected before)
		{"1735689600123", time.Date(2025, 1, 1, 0, 0, 0, 123e6, time.UTC), true},
		{"1735689600000000", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true},    // micros
		{"1735689600000000000", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), true}, // nanos
		{"1735689600.5", time.Date(2025, 1, 1, 0, 0, 0, 500e6, time.UTC), true},
		{"0", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := ParseAt(c.in, now)
		if ok != c.ok {
			t.Errorf("%q: ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

func TestParseRange(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	f, to := ParseRange("", "", time.Hour, now)
	if !to.Equal(now) || !f.Equal(now.Add(-time.Hour)) {
		t.Fatalf("defaults: %v %v", f, to)
	}
	f, to = ParseRange("2025-06-01T11:00:00Z", "2025-06-01T10:00:00Z", time.Hour, now)
	if !f.Before(to) {
		t.Fatalf("inverted range not swapped: %v %v", f, to)
	}
}

func TestParseEndExclusive(t *testing.T) {
	now := time.Now()
	got, ok := ParseEndExclusive("2025-01-01T10:30", now)
	if !ok || !got.Equal(time.Date(2025, 1, 1, 10, 31, 0, 0, time.UTC)) {
		t.Fatalf("minute precision not advanced: %v %v", got, ok)
	}
	got, ok = ParseEndExclusive("2025-01-01T10:30:15Z", now)
	if !ok || !got.Equal(time.Date(2025, 1, 1, 10, 30, 15, 0, time.UTC)) {
		t.Fatalf("second precision changed: %v %v", got, ok)
	}
}
