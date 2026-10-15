package clickhouse

import (
	"testing"
	"time"
)

func TestSummarizeFixes(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	firstSeen := map[string]time.Time{"a": t0, "b": t0, "c": t0.Add(-30 * 24 * time.Hour)}
	reopened := map[string][]time.Time{
		// b came back after its first fix; its second fix counts from the comeback.
		"b": {t0.Add(48 * time.Hour), t0.Add(500 * time.Hour)},
	}
	fixes := []fixEvent{
		{IssueID: "a", At: t0.Add(30 * time.Minute)},              // within an hour
		{IssueID: "b", At: t0.Add(10 * time.Hour)},                // within a day
		{IssueID: "b", At: t0.Add(48*time.Hour + 3*24*time.Hour)}, // 3 days after it came back
		{IssueID: "c", At: t0},                                    // a month
		{IssueID: "gone", At: t0},                                 // no first-seen: skipped
	}
	got := summarizeFixes(fixes, firstSeen, reopened)
	if got.Fixed != 4 || got.WithinHour != 1 || got.WithinDay != 1 || got.WithinWeek != 1 || got.Longer != 1 {
		t.Fatalf("spread: %+v", got)
	}
	if got.MedianHours != 41 { // the middle of 0.5, 10, 72 and 720 hours
		t.Fatalf("median: %v", got.MedianHours)
	}
	if z := summarizeFixes(nil, nil, nil); z.Fixed != 0 || z.MedianHours != 0 {
		t.Fatalf("empty: %+v", z)
	}
}

func TestAnalyticsBucket(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for span, want := range map[time.Duration]time.Duration{
		24 * time.Hour:       time.Hour,
		7 * 24 * time.Hour:   24 * time.Hour,
		90 * 24 * time.Hour:  24 * time.Hour,
		365 * 24 * time.Hour: 7 * 24 * time.Hour,
	} {
		if got := analyticsBucket(now.Add(-span), now); got != want {
			t.Errorf("%v: %v want %v", span, got, want)
		}
	}
	for span, want := range map[time.Duration]time.Duration{
		24 * time.Hour:       time.Hour,
		48 * time.Hour:       24 * time.Hour,
		365 * 24 * time.Hour: 7 * 24 * time.Hour,
	} {
		if got := errorBucket(now.Add(-span), now); got != want {
			t.Errorf("errors over %v: %v want %v", span, got, want)
		}
	}
}
