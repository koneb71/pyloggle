package issue

import (
	"context"
	"testing"
	"time"
)

func TestStateCacheBumpAndSet(t *testing.T) {
	loads := 0
	c := NewStateCache(time.Minute, func(ctx context.Context, p, i string) (State, bool, error) {
		loads++
		return State{Status: StatusIgnored, SnoozeCount: 12, OccurrenceCount: 10}, true, nil
	})
	if n := c.Bump("p", "i"); n != 0 {
		t.Fatalf("bump before load should be 0, got %d", n)
	}
	st, known, err := c.Get(context.Background(), "p", "i")
	if err != nil || !known || st.SnoozeCount != 12 {
		t.Fatalf("get: %+v %v %v", st, known, err)
	}
	if n := c.Bump("p", "i"); n != 11 {
		t.Fatalf("first bump = %d, want 11", n)
	}
	if n := c.Bump("p", "i"); n != 12 {
		t.Fatalf("second bump = %d, want 12", n)
	}
	c.Set("p", "i", State{Status: StatusOpen})
	if n := c.Bump("p", "i"); n != 1 {
		t.Fatalf("Set resets the seen counter: got %d", n)
	}
	if _, _, _ = c.Get(context.Background(), "p", "i"); loads != 1 {
		t.Fatalf("cached entry must not reload: loads=%d", loads)
	}
}
