package ingest

import (
	"hash/fnv"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/model"
)

// limiter applies rate limiting + sampling for non-error events.
// Errors are always kept.
type limiter struct {
	cfg config.Config

	mu   sync.Mutex
	bkts map[string]*tokenBucket
}

func newLimiter(cfg config.Config) *limiter {
	return &limiter{
		cfg:  cfg,
		bkts: map[string]*tokenBucket{},
	}
}

// outcome applies the rate limit and sampling to a non-error event. Errors are always kept.
// rateLimitRPS/burst are per (project, service) and typically come from the projects table.
func (l *limiter) outcome(ev model.Event, rateLimitRPS float64, rateLimitBurst float64) Outcome {
	if isErrorEvent(ev) {
		return Accepted
	}

	// Rate limit first (cheap, deterministic)
	if rateLimitRPS > 0 {
		key := ev.ProjectID + "|" + ev.Service
		if !l.take(key, rateLimitRPS, rateLimitBurst) {
			return DroppedRateLimit
		}
	}

	// Then sample by level.
	p := sampleProb(l.cfg, ev.Level)
	if p >= 1 {
		return Accepted
	}
	if p <= 0 {
		return DroppedSampled
	}

	// Deterministic sampling. Seeding by request id keeps every line of a kept request
	// together; without one, the (logger, message, level) triple decides so a given line
	// does not flap in and out.
	var seed uint64
	if l.cfg.SampleByRequest && ev.RequestID != "" {
		seed = stableHash64("req\n" + ev.RequestID)
	} else {
		seed = stableHash64(ev.Logger + "\n" + ev.Message + "\n" + ev.Level)
	}
	r := rand.New(rand.NewSource(int64(seed))).Float64()
	if r > p {
		return DroppedSampled
	}
	return Accepted
}

// shouldDrop is kept for callers that only need a boolean.
func (l *limiter) shouldDrop(ev model.Event, rateLimitRPS float64, rateLimitBurst float64) bool {
	return l.outcome(ev, rateLimitRPS, rateLimitBurst) != Accepted
}

func (l *limiter) take(key string, rateLimitRPS float64, rateLimitBurst float64) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.bkts[key]
	if b == nil {
		b = &tokenBucket{
			rate:  rateLimitRPS,
			burst: rateLimitBurst,
			tok:   rateLimitBurst,
			last:  now,
		}
		l.bkts[key] = b
	} else {
		// Update in case config changed per-project.
		b.rate = rateLimitRPS
		b.burst = rateLimitBurst
		if b.tok > b.burst {
			b.tok = b.burst
		}
	}
	return b.take(now)
}

func sampleProb(cfg config.Config, level string) float64 {
	switch strings.ToLower(level) {
	case "debug":
		return cfg.SampleDebug
	case "info":
		return cfg.SampleInfo
	case "warn", "warning":
		return cfg.SampleWarn
	case "error", "fatal", "critical":
		return 1
	default:
		// unknown -> treat as info
		return cfg.SampleInfo
	}
}

func isErrorEvent(ev model.Event) bool {
	return ev.Exception != nil || model.IsErrorLevel(ev.Level)
}

func stableHash64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

type tokenBucket struct {
	rate  float64
	burst float64
	tok   float64
	last  time.Time
}

func (b *tokenBucket) take(now time.Time) bool {
	if b.rate <= 0 {
		return true
	}
	dt := now.Sub(b.last).Seconds()
	if dt > 0 {
		b.tok += dt * b.rate
		if b.tok > b.burst {
			b.tok = b.burst
		}
		b.last = now
	}
	if b.tok >= 1 {
		b.tok -= 1
		return true
	}
	return false
}
