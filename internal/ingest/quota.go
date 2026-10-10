package ingest

import (
	"sync"
	"time"
)

// quotaTracker is an in-memory quota guard to protect a single ingestd instance.
//
// Today we interpret projects.max_storage_gb as a coarse daily ingest budget (in GB)
// since ClickHouse per-project storage attribution is non-trivial without extra tables.
// This is primarily a safety mechanism to stop a misconfigured/noisy client from
// consuming unlimited resources.
type quotaTracker struct {
	mu sync.Mutex
	m  map[string]quotaState // project_id -> state
}

type quotaState struct {
	day   string
	bytes uint64
}

func newQuotaTracker() *quotaTracker {
	return &quotaTracker{m: map[string]quotaState{}}
}

// allow returns true if the event should be accepted under quota.
// Errors are always allowed.
func (q *quotaTracker) allow(projectID string, isError bool, bytes int, maxStorageGB uint32, now time.Time) bool {
	if isError {
		return true
	}
	if projectID == "" || maxStorageGB == 0 || bytes <= 0 {
		return true
	}
	limit := uint64(maxStorageGB) * 1024 * 1024 * 1024
	day := now.UTC().Format("2006-01-02")

	q.mu.Lock()
	st := q.m[projectID]
	if st.day != day {
		st = quotaState{day: day}
	}
	if st.bytes+uint64(bytes) > limit {
		// do not increment when denied
		q.mu.Unlock()
		return false
	}
	st.bytes += uint64(bytes)
	q.m[projectID] = st
	q.mu.Unlock()
	return true
}
