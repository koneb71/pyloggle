package issue

import (
	"context"
	"sync"
	"time"
)

// Status values stored in issues_state.
const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
	StatusIgnored  = "ignored"
)

// State is the lifecycle state of an issue.
type State struct {
	Status    string
	Regressed bool
}

// StateLoader fetches the current state from storage; found=false means the issue is unknown.
type StateLoader func(ctx context.Context, projectID, issueID string) (status string, regressed bool, found bool, err error)

// StateCache memoizes issue states so the ingest hot path does not query ClickHouse per event.
type StateCache struct {
	ttl    time.Duration
	loader StateLoader
	now    func() time.Time

	mu    sync.Mutex
	items map[string]stateItem
}

type stateItem struct {
	st      State
	known   bool
	expires time.Time
}

func NewStateCache(ttl time.Duration, loader StateLoader) *StateCache {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &StateCache{ttl: ttl, loader: loader, now: time.Now, items: map[string]stateItem{}}
}

// Get returns the state and whether the issue is known (has a stored state). Unknown issues are
// cached as unknown so a storm of first occurrences does not hammer storage.
func (c *StateCache) Get(ctx context.Context, projectID, issueID string) (State, bool, error) {
	key := projectID + "|" + issueID
	now := c.now()
	c.mu.Lock()
	if it, ok := c.items[key]; ok && now.Before(it.expires) {
		c.mu.Unlock()
		return it.st, it.known, nil
	}
	c.mu.Unlock()

	var st State
	known := false
	if c.loader != nil {
		status, regressed, found, err := c.loader(ctx, projectID, issueID)
		if err != nil {
			return State{}, false, err
		}
		if found {
			st = State{Status: status, Regressed: regressed}
			known = true
		}
	}
	c.mu.Lock()
	c.items[key] = stateItem{st: st, known: known, expires: now.Add(c.ttl)}
	if len(c.items) > 50000 { // crude bound; entries expire anyway
		for k := range c.items {
			delete(c.items, k)
			if len(c.items) <= 25000 {
				break
			}
		}
	}
	c.mu.Unlock()
	return st, known, nil
}

// Set records a state change made by this process (ingest regression or a UI action).
func (c *StateCache) Set(projectID, issueID string, st State) {
	c.mu.Lock()
	c.items[projectID+"|"+issueID] = stateItem{st: st, known: true, expires: c.now().Add(c.ttl)}
	c.mu.Unlock()
}
