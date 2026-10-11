package ingest

import (
	"context"
	"errors"
	"sync"
	"time"

	"pyloggle/internal/config"
	"pyloggle/internal/issue"
	"pyloggle/internal/model"
	"pyloggle/internal/storage/clickhouse"
)

// streamRouter provides bounded per-(project,service) queues to prevent a single noisy
// stream from starving others, while keeping ingest request handling fast.
//
// It sits in front of the ClickHouse client (which already does global batching).
type streamRouter struct {
	cfg config.Config
	ch  *clickhouse.Client

	mu      sync.Mutex
	streams map[string]*stream

	stopOnce sync.Once
	stopCh   chan struct{}

	overflow *stream
}

type stream struct {
	key string

	logCh chan model.Event
	errCh chan issue.ErrorOccurrence

	once sync.Once
}

func newStreamRouter(cfg config.Config, ch *clickhouse.Client) *streamRouter {
	r := &streamRouter{
		cfg:     cfg,
		ch:      ch,
		streams: make(map[string]*stream),
		stopCh:  make(chan struct{}),
	}
	// Always have an overflow stream so we can "always keep errors" even when stream
	// cardinality explodes.
	r.overflow = r.newStreamLocked("_overflow_")
	return r
}

func (r *streamRouter) Close() {
	r.stopOnce.Do(func() { close(r.stopCh) })
}

func (r *streamRouter) EnqueueLog(ctx context.Context, ev model.Event) error {
	s, overflowed := r.getStream(ev.ProjectID, ev.Service)
	if overflowed && !isErrorEvent(ev) {
		// refuse to accept non-error logs for new streams once we're at capacity
		return nil
	}

	// Non-error logs never block request handling.
	if !isErrorEvent(ev) {
		select {
		case s.logCh <- ev:
			return nil
		default:
			return nil
		}
	}

	// Errors are high-signal: apply brief backpressure.
	t := time.NewTimer(250 * time.Millisecond)
	defer t.Stop()
	select {
	case s.logCh <- ev:
		return nil
	case <-t.C:
		return errors.New("stream log queue full")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *streamRouter) EnqueueErrorOccurrence(ctx context.Context, occ issue.ErrorOccurrence) error {
	s, _ := r.getStream(occ.ProjectID, occ.Service)

	// Errors are high-signal: allow longer backpressure.
	select {
	case s.errCh <- occ:
		return nil
	default:
		t := time.NewTimer(750 * time.Millisecond)
		defer t.Stop()
		select {
		case s.errCh <- occ:
			return nil
		case <-t.C:
			return errors.New("stream error queue full")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// EnqueueSpans hands spans straight to the client's own batcher: they arrive in bulk per
// request and are never high-signal enough to need per-stream fairness.
func (r *streamRouter) EnqueueSpans(ctx context.Context, spans []model.Span) error {
	return r.ch.InsertSpans(ctx, spans)
}

// EnqueueMetricPoints hands metric points to the client's batcher, like spans.
func (r *streamRouter) EnqueueMetricPoints(ctx context.Context, points []model.MetricPoint) error {
	return r.ch.InsertMetricPoints(ctx, points)
}

// getStream returns the stream for (projectID, service). If stream capacity is exceeded,
// returns the overflow stream, and overflowed=true.
func (r *streamRouter) getStream(projectID, service string) (*stream, bool) {
	key := projectID + "|" + service

	r.mu.Lock()
	defer r.mu.Unlock()

	if s := r.streams[key]; s != nil {
		s.start(r)
		return s, false
	}

	// Cap the number of streams to keep goroutines bounded.
	if r.cfg.MaxStreams > 0 && len(r.streams) >= r.cfg.MaxStreams {
		r.overflow.start(r)
		return r.overflow, true
	}

	s := r.newStreamLocked(key)
	s.start(r)
	return s, false
}

func (r *streamRouter) newStreamLocked(key string) *stream {
	q := r.cfg.MaxQueuePerStream
	if q <= 0 {
		q = 2000
	}
	s := &stream{
		key:   key,
		logCh: make(chan model.Event, q),
		errCh: make(chan issue.ErrorOccurrence, q),
	}
	r.streams[key] = s
	return s
}

func (s *stream) start(r *streamRouter) {
	s.once.Do(func() {
		go s.run(r)
	})
}

func (s *stream) run(r *streamRouter) {
	for {
		select {
		case <-r.stopCh:
			return
		case ev := <-s.logCh:
			// Use background context: these are already buffered and should not be
			// tied to the request lifetime.
			_ = r.ch.InsertLog(context.Background(), ev)
		case occ := <-s.errCh:
			_ = r.ch.InsertErrorOccurrence(context.Background(), occ)
		}
	}
}
