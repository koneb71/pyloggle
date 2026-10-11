package tenant

import (
	"context"
	"sync"
	"time"

	"pyloggle/internal/storage/clickhouse"
)

type Project struct {
	ID              string
	Name            string
	MaxEventsPerSec uint32
	MaxStorageGB    uint32
	APIKeyHash      string
}

type cacheItem struct {
	p       Project
	expires time.Time
}

// Resolver maps API keys to projects with a small TTL cache to keep ingest fast.
type Resolver struct {
	ch  *clickhouse.Client
	ttl time.Duration

	mu    sync.Mutex
	cache map[string]cacheItem // api_key_hash -> project
}

func NewResolver(ch *clickhouse.Client, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &Resolver{
		ch:    ch,
		ttl:   ttl,
		cache: map[string]cacheItem{},
	}
}

func (r *Resolver) ResolveByAPIKey(ctx context.Context, apiKey string) (Project, bool, error) {
	if apiKey == "" {
		return Project{}, false, nil
	}
	h := APIKeyHash(apiKey)

	now := time.Now()
	r.mu.Lock()
	if it, ok := r.cache[h]; ok && now.Before(it.expires) {
		p := it.p
		r.mu.Unlock()
		return p, true, nil
	}
	r.mu.Unlock()

	p, ok, err := r.ch.GetProjectByAPIKeyHash(ctx, h)
	if err != nil {
		return Project{}, false, err
	}
	if !ok {
		return Project{}, false, nil
	}

	proj := Project{
		ID:              p.ID,
		Name:            p.Name,
		MaxEventsPerSec: p.MaxEventsPerSec,
		MaxStorageGB:    p.MaxStorageGB,
		APIKeyHash:      p.APIKeyHash,
	}
	r.mu.Lock()
	r.cache[h] = cacheItem{p: proj, expires: now.Add(r.ttl)}
	r.mu.Unlock()

	return proj, true, nil
}

// Invalidate forgets one cached key hash (rotation).
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
