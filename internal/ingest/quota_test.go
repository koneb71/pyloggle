package ingest

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeUsage struct {
	mu    sync.Mutex
	calls int
	m     map[string]uint64
}

func (f *fakeUsage) StoredBytesByProject(context.Context, time.Duration) (map[string]uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.m, nil
}

func TestUsageGuardThresholds(t *testing.T) {
	g := newUsageGuard(nil, 0)
	now := time.Now()
	gb := uint64(1 << 30)
	g.setStored(map[string]uint64{"p": gb - 100}, now)
	if !g.allow("p", false, 50, 1, now) {
		t.Fatal("under the quota must pass")
	}
	if g.allow("p", false, 100, 1, now) {
		t.Fatal("non-error rows stop at the quota")
	}
	if !g.allow("p", true, 100, 1, now) {
		t.Fatal("errors keep flowing past the quota")
	}
	g.setStored(map[string]uint64{"p": 2*gb + 1}, now)
	if g.allow("p", true, 1, 1, now) {
		t.Fatal("errors stop at twice the quota")
	}
	if !g.allow("p", false, 1, 0, now) || !g.allow("", false, 1, 1, now) {
		t.Fatal("no quota / no project: always allowed")
	}
	if !g.allow("q", false, 10, 1, now) {
		t.Fatal("other projects are independent")
	}
}

func TestUsageGuardRefreshesOffTheHotPath(t *testing.T) {
	store := &fakeUsage{m: map[string]uint64{"p": 5 << 30}}
	g := newUsageGuard(store, 24*time.Hour)
	now := time.Now()
	// First call kicks a refresh; the answer uses the empty snapshot and allows.
	if !g.allow("p", false, 10, 1, now) {
		t.Fatal("first call must not block on storage")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		g.mu.Lock()
		loaded := g.stored["p"] > 0
		g.mu.Unlock()
		if loaded || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if g.allow("p", false, 10, 1, now) {
		t.Fatal("after the refresh the stored bytes count")
	}
	store.mu.Lock()
	calls := store.calls
	store.mu.Unlock()
	if calls != 1 {
		t.Fatalf("one refresh per interval, got %d", calls)
	}
	// Daily reset without a store.
	g2 := newUsageGuard(nil, 0)
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	g2.allow("p", false, 1<<30, 1, day)
	if g2.allow("p", false, 1, 1, day) {
		t.Fatal("same day: full")
	}
	if !g2.allow("p", false, 1, 1, day.Add(24*time.Hour)) {
		t.Fatal("next day: reset")
	}
}
