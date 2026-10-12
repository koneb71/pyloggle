package tenant

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

type memKeys struct {
	mu      sync.Mutex
	keys    map[string]clickhouse.ResolvedKey // hash -> key
	lookups int
	touched []map[clickhouse.KeyRef]time.Time
	fail    error
}

func (m *memKeys) ResolveAPIKey(_ context.Context, hash string) (clickhouse.ResolvedKey, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookups++
	if m.fail != nil {
		return clickhouse.ResolvedKey{}, false, m.fail
	}
	k, ok := m.keys[hash]
	return k, ok, nil
}

func (m *memKeys) TouchAPIKeys(_ context.Context, used map[clickhouse.KeyRef]time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touched = append(m.touched, used)
	return nil
}

func TestResolverCacheScopesAndTouches(t *testing.T) {
	store := &memKeys{keys: map[string]clickhouse.ResolvedKey{
		APIKeyHash("pl_read"): {KeyID: "k_r", Name: "ro", Scopes: []string{ScopeRead}, Project: clickhouse.ProjectRow{ID: "p1", Name: "One", MaxStorageGB: 2}},
	}}
	r := NewResolverWithStore(store, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { return now }
	r.lastFlush = now

	p, ok, err := r.ResolveByAPIKey(context.Background(), "pl_read")
	if err != nil || !ok || p.ID != "p1" || p.KeyID != "k_r" || !p.Has(ScopeRead) || p.Has(ScopeWrite) || p.Has(ScopeIngest) || p.MaxStorageGB != 2 {
		t.Fatalf("resolve: %+v %v %v", p, ok, err)
	}
	if _, ok, _ := r.ResolveByAPIKey(context.Background(), "nope"); ok {
		t.Fatal("unknown key resolved")
	}
	if _, ok, _ := r.ResolveByAPIKey(context.Background(), ""); ok {
		t.Fatal("empty key resolved")
	}
	// Cached: a second call does not hit the store.
	before := store.lookups
	if _, ok, _ := r.ResolveByAPIKey(context.Background(), "pl_read"); !ok || store.lookups != before {
		t.Fatalf("cache miss: lookups=%d", store.lookups)
	}
	// Scopes handed out are copies: a caller cannot widen the cached entry.
	p.Scopes[0] = ScopeWrite
	if q, _, _ := r.ResolveByAPIKey(context.Background(), "pl_read"); !q.Has(ScopeRead) || q.Has(ScopeWrite) {
		t.Fatalf("cached scopes mutated: %v", q.Scopes)
	}
	// Invalidate forces a lookup; InvalidateProject too.
	r.Invalidate(APIKeyHash("pl_read"))
	if _, _, _ = r.ResolveByAPIKey(context.Background(), "pl_read"); store.lookups != before+1 {
		t.Fatalf("invalidate did not refetch: %d", store.lookups)
	}
	r.InvalidateProject("p1")
	if _, _, _ = r.ResolveByAPIKey(context.Background(), "pl_read"); store.lookups != before+2 {
		t.Fatalf("invalidate project did not refetch: %d", store.lookups)
	}
	// Expiry.
	now = now.Add(2 * time.Minute)
	if _, _, _ = r.ResolveByAPIKey(context.Background(), "pl_read"); store.lookups != before+3 {
		t.Fatalf("ttl expiry did not refetch: %d", store.lookups)
	}
	// Touches: nothing flushed yet (under five minutes), FlushTouches writes the latest time.
	if len(store.touched) != 0 {
		t.Fatalf("early flush: %v", store.touched)
	}
	if err := r.FlushTouches(context.Background()); err != nil || len(store.touched) != 1 {
		t.Fatalf("flush: %v %v", err, store.touched)
	}
	if at := store.touched[0][clickhouse.KeyRef{ProjectID: "p1", KeyID: "k_r"}]; !at.Equal(now) {
		t.Fatalf("touch time: %v want %v", at, now)
	}
	if err := r.FlushTouches(context.Background()); err != nil || len(store.touched) != 1 {
		t.Fatal("empty flush must not write")
	}
	// After touchEvery, a resolve kicks off a background flush.
	now = now.Add(6 * time.Minute)
	_, _, _ = r.ResolveByAPIKey(context.Background(), "pl_read")
	deadline := time.Now().Add(2 * time.Second)
	for {
		store.mu.Lock()
		n := len(store.touched)
		store.mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(store.touched) != 2 {
		t.Fatalf("background flush did not run: %d", len(store.touched))
	}
	// Store errors surface.
	store.fail = errors.New("ch down")
	r.Invalidate(APIKeyHash("pl_read"))
	if _, _, err := r.ResolveByAPIKey(context.Background(), "pl_read"); err == nil {
		t.Fatal("store error swallowed")
	}
}

func TestProjectScopes(t *testing.T) {
	p := Project{ID: "p"}
	if p.Has(ScopeRead) {
		t.Fatal("no scopes must allow nothing")
	}
	tp := Trusted(p)
	for _, s := range AllScopes {
		if !tp.Has(s) || !ValidScope(s) {
			t.Fatalf("trusted lacks %s", s)
		}
	}
	if ValidScope("admin") || ValidScope("") {
		t.Fatal("unknown scope accepted")
	}
}
