package ingest

import (
	"context"
	"sync"
	"time"
)

// UsageStore reports the estimated on-disk bytes each project holds over the retention window.
type UsageStore interface {
	StoredBytesByProject(ctx context.Context, window time.Duration) (map[string]uint64, error)
}

// usageGuard enforces projects.max_storage_gb as "estimated bytes stored over the retention
// window": the stored figure from the usage table (refreshed every minute, off the hot path)
// plus what this process accepted since the refresh. Non-error rows stop at the quota; errors
// keep flowing up to twice the quota, then everything is dropped.
type usageGuard struct {
	store   UsageStore
	window  time.Duration
	refresh time.Duration

	mu      sync.Mutex
	stored  map[string]uint64
	since   map[string]uint64
	at      time.Time
	loading bool
	day     string
}

func newUsageGuard(store UsageStore, window time.Duration) *usageGuard {
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	return &usageGuard{store: store, window: window, refresh: time.Minute, stored: map[string]uint64{}, since: map[string]uint64{}}
}

// allow reports whether bytes more for the project fit under maxStorageGB (0 = unlimited).
func (g *usageGuard) allow(projectID string, isError bool, bytes int, maxStorageGB uint32, now time.Time) bool {
	if projectID == "" || maxStorageGB == 0 || bytes <= 0 {
		return true
	}
	limit := uint64(maxStorageGB) * 1024 * 1024 * 1024
	g.mu.Lock()
	defer g.mu.Unlock()
	g.maybeRefresh(now)
	used := g.stored[projectID] + g.since[projectID] + uint64(bytes)
	if used > limit && (!isError || used > 2*limit) {
		return false
	}
	g.since[projectID] += uint64(bytes)
	return true
}

// maybeRefresh kicks one background load when the stored figures are stale. Without a store
// the counters reset daily, which is the pre-0.3 behavior.
func (g *usageGuard) maybeRefresh(now time.Time) {
	if g.store == nil {
		if day := now.UTC().Format("2006-01-02"); day != g.day {
			g.day, g.since = day, map[string]uint64{}
		}
		return
	}
	if g.loading || now.Sub(g.at) < g.refresh {
		return
	}
	g.loading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stored, err := g.store.StoredBytesByProject(ctx, g.window)
		g.mu.Lock()
		defer g.mu.Unlock()
		g.loading = false
		g.at = now
		if err != nil || stored == nil {
			return // keep counting from the last snapshot
		}
		g.stored, g.since = stored, map[string]uint64{}
	}()
}

// setStored is for tests: a snapshot without a store.
func (g *usageGuard) setStored(m map[string]uint64, at time.Time) {
	g.mu.Lock()
	g.stored, g.since, g.at = m, map[string]uint64{}, at
	g.mu.Unlock()
}
