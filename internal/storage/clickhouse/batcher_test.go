package clickhouse

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBatcherDropsWhenFullAndCountsWrites(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	var got []int
	b := newBatcher("t", 2, 1, time.Hour, func(ctx context.Context, rows []int) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		mu.Lock()
		got = append(got, rows...)
		mu.Unlock()
		return nil
	})
	defer b.close()
	ctx := context.Background()
	if err := b.enqueue(ctx, 1, 0); err != nil {
		t.Fatal(err)
	}
	<-started // the loop is now blocked inside flush with row 1
	if err := b.enqueue(ctx, 2, 0); err != nil {
		t.Fatal(err)
	}
	if err := b.enqueue(ctx, 3, 0); err != nil {
		t.Fatal(err)
	}
	if err := b.enqueue(ctx, 4, 0); !errors.Is(err, errQueueFull) {
		t.Fatalf("expected queue full, got %v", err)
	}
	if err := b.enqueue(ctx, 5, 30*time.Millisecond); !errors.Is(err, errQueueFull) {
		t.Fatalf("expected queue full after waiting, got %v", err)
	}
	close(release)
	if err := b.flushNow(ctx); err != nil {
		t.Fatal(err)
	}
	st := b.stats()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 || st.Written != 3 || st.Dropped != 2 || st.FlushFailures != 0 || st.Capacity != 2 {
		t.Fatalf("got=%v stats=%+v", got, st)
	}
}

func TestBatcherRetriesThenDrops(t *testing.T) {
	var calls int
	var mu sync.Mutex
	b := newBatcher("t2", 10, 10, time.Hour, func(ctx context.Context, rows []string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("clickhouse down")
	})
	defer b.close()
	ctx := context.Background()
	_ = b.enqueue(ctx, "a", 0)
	_ = b.enqueue(ctx, "b", 0)
	if err := b.flushNow(ctx); err != nil {
		t.Fatal(err)
	}
	st := b.stats()
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 || st.FlushFailures != 3 || st.Dropped != 2 || st.Written != 0 {
		t.Fatalf("calls=%d stats=%+v", calls, st)
	}
}
