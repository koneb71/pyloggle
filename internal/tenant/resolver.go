package tenant

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

// KeyStore is what the resolver needs from storage (*clickhouse.Client in production).
type KeyStore interface {
	ResolveAPIKey(ctx context.Context, keyHash string) (clickhouse.ResolvedKey, bool, error)
	TouchAPIKeys(ctx context.Context, lastUsed map[clickhouse.KeyRef]time.Time) error
}

type cacheItem struct {
	p       Project
	expires time.Time
}

// Resolver maps API keys to projects with a small TTL cache to keep ingest fast, and records
// when each key was last used (flushed in batches, never per request).
type Resolver struct {
	store      KeyStore
	ttl        time.Duration
	touchEvery time.Duration
	now        func() time.Time

	mu        sync.Mutex
	cache     map[string]cacheItem            // api_key_hash -> project
	touched   map[clickhouse.KeyRef]time.Time // key -> last use since the previous flush
	lastFlush time.Time
	flushing  bool
}

func NewResolver(ch *clickhouse.Client, ttl time.Duration) *Resolver {
	return NewResolverWithStore(ch, ttl)
}

// NewResolverWithStore builds a resolver over any KeyStore (tests pass a fake).
func NewResolverWithStore(store KeyStore, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &Resolver{store: store, ttl: ttl, touchEvery: 5 * time.Minute, now: time.Now,
		cache: map[string]cacheItem{}, touched: map[clickhouse.KeyRef]time.Time{}, lastFlush: time.Now()}
}

func (r *Resolver) ResolveByAPIKey(ctx context.Context, apiKey string) (Project, bool, error) {
	if apiKey == "" {
		return Project{}, false, nil
	}
	h := APIKeyHash(apiKey)

	now := r.now()
	r.mu.Lock()
	if it, ok := r.cache[h]; ok && now.Before(it.expires) {
		p := it.p
		p.Scopes = append([]string(nil), it.p.Scopes...) // callers never share the cached slice
		r.touchLocked(p, now)
		r.mu.Unlock()
		return p, true, nil
	}
	r.mu.Unlock()

	k, ok, err := r.store.ResolveAPIKey(ctx, h)
	if err != nil {
		return Project{}, false, err
	}
	if !ok {
		return Project{}, false, nil
	}

	proj := Project{
		ID:              k.Project.ID,
		Name:            k.Project.Name,
		MaxEventsPerSec: k.Project.MaxEventsPerSec,
		MaxStorageGB:    k.Project.MaxStorageGB,
		APIKeyHash:      h,
		KeyID:           k.KeyID,
		Scopes:          append([]string(nil), k.Scopes...),
	}
	cached := proj
	cached.Scopes = append([]string(nil), proj.Scopes...)
	r.mu.Lock()
	r.cache[h] = cacheItem{p: cached, expires: now.Add(r.ttl)}
	r.touchLocked(proj, now)
	r.mu.Unlock()

	return proj, true, nil
}

// touchLocked records a use and starts a background flush when the previous one is old enough.
func (r *Resolver) touchLocked(p Project, now time.Time) {
	if p.KeyID == "" {
		return
	}
	r.touched[clickhouse.KeyRef{ProjectID: p.ID, KeyID: p.KeyID}] = now
	if r.flushing || now.Sub(r.lastFlush) < r.touchEvery {
		return
	}
	batch := r.touched
	r.touched = map[clickhouse.KeyRef]time.Time{}
	r.lastFlush = now
	r.flushing = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := r.store.TouchAPIKeys(ctx, batch); err != nil {
			log.Printf("api keys: last_used flush: %v", err)
		}
		r.mu.Lock()
		r.flushing = false
		r.mu.Unlock()
	}()
}

// FlushTouches writes the pending last-used times now (shutdown, tests).
func (r *Resolver) FlushTouches(ctx context.Context) error {
	r.mu.Lock()
	batch := r.touched
	r.touched = map[clickhouse.KeyRef]time.Time{}
	r.lastFlush = r.now()
	r.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	return r.store.TouchAPIKeys(ctx, batch)
}

// Invalidate forgets one cached key hash (revocation, rotation).
func (r *Resolver) Invalidate(apiKeyHash string) {
	r.mu.Lock()
	delete(r.cache, apiKeyHash)
	r.mu.Unlock()
}

// InvalidateProject forgets every cached key of a project (deletion, quota changes).
func (r *Resolver) InvalidateProject(projectID string) {
	r.mu.Lock()
	for h, it := range r.cache {
		if it.p.ID == projectID {
			delete(r.cache, h)
		}
	}
	r.mu.Unlock()
}
