package ingest

import (
	"strings"
	"sync"
	"time"
)

// traceDedupe remembers the first error occurrence seen per (project, trace, exception type)
// for a short while, so one crash reported twice through different doors (the SDK's report
// and an OpenTelemetry exception log or span event for the same trace) becomes one issue
// occurrence plus a duplicate-marked log line, in whichever order the two arrive.
type traceDedupe struct {
	mu   sync.Mutex
	ttl  time.Duration
	max  int
	seen map[string]dedupeEntry
	fifo []dedupeKey
}

type dedupeEntry struct {
	EventID string
	IssueID string
	At      time.Time
}

type dedupeKey struct {
	key string
	at  time.Time
}

func newTraceDedupe(ttl time.Duration, max int) *traceDedupe {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	if max <= 0 {
		max = 20000
	}
	return &traceDedupe{ttl: ttl, max: max, seen: map[string]dedupeEntry{}}
}

// dedupeType reduces "app.errors.PaymentError" and "PaymentError" to the same class name: the
// SDK sends the bare name, OpenTelemetry instrumentations may qualify it with the module.
func dedupeType(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		t = t[i+1:]
	}
	return t
}

func (d *traceDedupe) key(projectID, traceID, excType string) string {
	return projectID + "|" + traceID + "|" + dedupeType(excType)
}

// first returns the earlier occurrence for the key when one is still remembered; otherwise it
// records this one and reports it as the first.
func (d *traceDedupe) first(projectID, traceID, excType, eventID, issueID string, now time.Time) (dedupeEntry, bool) {
	k := d.key(projectID, traceID, excType)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sweepLocked(now)
	if e, ok := d.seen[k]; ok {
		return e, true
	}
	d.seen[k] = dedupeEntry{EventID: eventID, IssueID: issueID, At: now}
	d.fifo = append(d.fifo, dedupeKey{key: k, at: now})
	return dedupeEntry{}, false
}

func (d *traceDedupe) sweepLocked(now time.Time) {
	cut := now.Add(-d.ttl)
	n := 0
	for n < len(d.fifo) && (d.fifo[n].at.Before(cut) || len(d.fifo)-n >= d.max) {
		if e, ok := d.seen[d.fifo[n].key]; ok && e.At.Equal(d.fifo[n].at) {
			delete(d.seen, d.fifo[n].key)
		}
		n++
	}
	if n > 0 {
		d.fifo = append([]dedupeKey(nil), d.fifo[n:]...)
	}
}
