package clickhouse

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/koneb71/pyloggle/internal/metrics"
)

var errQueueFull = errors.New("queue full")

// BatcherStats is one write queue's health, reported on /healthz?verbose=1 and the System page.
type BatcherStats struct {
	Kind          string `json:"kind"`
	Depth         int    `json:"depth"`
	Capacity      int    `json:"capacity"`
	Written       uint64 `json:"written"`
	Dropped       uint64 `json:"dropped"`
	FlushFailures uint64 `json:"flush_failures"`
}

// batcher collects rows of one kind and inserts them in batches of `size` or every `timeout`.
// It is the single write path for logs, occurrences, spans and metric points.
type batcher[T any] struct {
	kind    string
	ch      chan T
	size    int
	timeout time.Duration
	flush   func(context.Context, []T) error

	stop     chan struct{}
	done     chan struct{}
	flushReq chan chan struct{}
	once     sync.Once

	written     atomic.Uint64
	dropped     atomic.Uint64
	failures    atomic.Uint64
	lastDropLog atomic.Int64
}

func newBatcher[T any](kind string, queue, size int, timeout time.Duration, flush func(context.Context, []T) error) *batcher[T] {
	if queue <= 0 {
		queue = 10000
	}
	if size <= 0 {
		size = 500
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	b := &batcher[T]{
		kind:     kind,
		ch:       make(chan T, queue),
		size:     size,
		timeout:  timeout,
		flush:    flush,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		flushReq: make(chan chan struct{}),
	}
	metrics.CHQueueCapacity.Set(float64(queue), kind)
	go b.run()
	return b
}

// enqueue adds one row. wait == 0 never blocks (the row is dropped and counted when the queue is
// full); otherwise it waits up to `wait` for room.
func (b *batcher[T]) enqueue(ctx context.Context, v T, wait time.Duration) error {
	select {
	case b.ch <- v:
		return nil
	default:
	}
	if wait <= 0 {
		b.dropped.Add(1)
		metrics.CHRowsDropped.Inc(b.kind)
		return errQueueFull
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case b.ch <- v:
		return nil
	case <-t.C:
		b.dropped.Add(1)
		metrics.CHRowsDropped.Inc(b.kind)
		return errQueueFull
	case <-ctx.Done():
		return ctx.Err()
	}
}

// flushNow drains the queue and writes everything, then returns.
func (b *batcher[T]) flushNow(ctx context.Context) error {
	done := make(chan struct{})
	select {
	case b.flushReq <- done:
	case <-b.stop:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// close stops the loop after a final drain and waits for it.
func (b *batcher[T]) close() {
	b.once.Do(func() { close(b.stop) })
	<-b.done
}

func (b *batcher[T]) stats() BatcherStats {
	return BatcherStats{Kind: b.kind, Depth: len(b.ch), Capacity: cap(b.ch), Written: b.written.Load(), Dropped: b.dropped.Load(), FlushFailures: b.failures.Load()}
}

func (b *batcher[T]) run() {
	defer close(b.done)
	t := time.NewTimer(b.timeout)
	defer t.Stop()
	buf := make([]T, 0, b.size)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		b.flushWithRetry(buf)
		buf = buf[:0]
	}
	drain := func() {
		for {
			select {
			case v := <-b.ch:
				buf = append(buf, v)
				if len(buf) >= b.size {
					flush()
				}
			default:
				flush()
				return
			}
		}
	}
	for {
		select {
		case <-b.stop:
			drain()
			return
		case v := <-b.ch:
			buf = append(buf, v)
			if len(buf) >= b.size {
				flush()
				resetTimer(t, b.timeout)
			}
		case done := <-b.flushReq:
			drain()
			close(done)
			resetTimer(t, b.timeout)
		case <-t.C:
			flush()
			resetTimer(t, b.timeout)
		}
		metrics.CHQueueDepth.Set(float64(len(b.ch)), b.kind)
	}
}

// flushWithRetry tries a batch three times with backoff; rows are dropped (and counted) only
// after the last attempt fails. Drop messages are rate-limited to one per 10s.
func (b *batcher[T]) flushWithRetry(rows []T) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := time.Now()
		err = b.flush(ctx, rows)
		cancel()
		metrics.CHFlushSeconds.Observe(time.Since(start).Seconds(), b.kind)
		if err == nil {
			b.written.Add(uint64(len(rows)))
			metrics.CHRowsWritten.Add(float64(len(rows)), b.kind)
			return
		}
		b.failures.Add(1)
		metrics.CHFlushFailures.Inc(b.kind)
		if attempt < 2 {
			time.Sleep(time.Duration(500<<attempt) * time.Millisecond)
		}
	}
	b.dropped.Add(uint64(len(rows)))
	metrics.CHRowsDropped.Add(float64(len(rows)), b.kind)
	now := time.Now().Unix()
	if last := b.lastDropLog.Load(); now-last >= 10 && b.lastDropLog.CompareAndSwap(last, now) {
		log.Printf("clickhouse: dropped %d %s rows after 3 failed flush attempts: %v", len(rows), b.kind, err)
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}
