package issue

import (
	"context"
	"testing"
	"time"
)

func TestStateCache(t *testing.T) {
	calls := 0
	loader := func(ctx context.Context, projectID, issueID string) (string, bool, bool, error) {
		calls++
		if issueID == "known" {
			return StatusResolved, false, true, nil
		}
		return "", false, false, nil
	}
	c := NewStateCache(time.Minute, loader)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }

	st, known, _ := c.Get(context.Background(), "p", "known")
	if !known || st.Status != StatusResolved || calls != 1 {
		t.Fatalf("first load: %+v %v %d", st, known, calls)
	}
	c.Get(context.Background(), "p", "known")
	if calls != 1 {
		t.Fatalf("second get must hit the cache")
	}
	_, known, _ = c.Get(context.Background(), "p", "new")
	c.Get(context.Background(), "p", "new")
	if known || calls != 2 {
		t.Fatalf("unknown issues are cached as unknown: known=%v calls=%d", known, calls)
	}
	c.Set("p", "new", State{Status: StatusOpen, Regressed: true})
	st, known, _ = c.Get(context.Background(), "p", "new")
	if !known || !st.Regressed {
		t.Fatalf("Set must be visible: %+v", st)
	}
	now = now.Add(2 * time.Minute)
	c.Get(context.Background(), "p", "known")
	if calls != 3 {
		t.Fatalf("expired entries reload")
	}
}
