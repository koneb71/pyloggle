// Package sysinfo samples ClickHouse (parts, disk, mutations, version) on a schedule and
// publishes the figures as Prometheus gauges and as a snapshot for the System page.
package sysinfo

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/koneb71/pyloggle/internal/metrics"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
	"github.com/koneb71/pyloggle/internal/version"
)

// Source is the slice of storage the collector needs (the ClickHouse client in production).
type Source interface {
	SystemInfo(ctx context.Context) (clickhouse.SystemInfo, error)
}

// Snapshot is the last successful sample plus whether the probe is currently failing.
type Snapshot struct {
	Up        bool                  `json:"up"`
	At        time.Time             `json:"at"`
	Error     string                `json:"error,omitempty"`
	Info      clickhouse.SystemInfo `json:"info"`
	StartedAt time.Time             `json:"started_at"`
}

type Collector struct {
	src      Source
	interval time.Duration
	now      func() time.Time

	mu   sync.Mutex
	last Snapshot
}

func New(src Source, interval time.Duration) *Collector {
	if interval <= 0 {
		interval = time.Minute
	}
	c := &Collector{src: src, interval: interval, now: time.Now}
	c.last.StartedAt = c.now()
	return c
}

// Run samples immediately and then every interval until ctx ends.
func (c *Collector) Run(ctx context.Context) {
	c.Collect(ctx)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Collect(ctx)
		}
	}
}

// Collect takes one sample and updates the gauges.
func (c *Collector) Collect(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := c.src.SystemInfo(ctx)
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.last.Up, c.last.Error = false, err.Error()
		metrics.CHUp.Set(0)
		return
	}
	c.last = Snapshot{Up: true, At: now, Info: info, StartedAt: c.last.StartedAt}
	metrics.CHUp.Set(1)
	metrics.CHDiskFree.Set(float64(info.DiskFree))
	metrics.CHDiskTotal.Set(float64(info.DiskTotal))
	metrics.CHMutationsPending.Set(float64(info.MutationsPending))
	for _, g := range []*metrics.Gauge{metrics.CHTableBytes, metrics.CHTableUncompressed, metrics.CHTableRows, metrics.CHTableParts, metrics.CHOldestPartitionAge} {
		g.Reset()
	}
	for _, t := range info.Tables {
		metrics.CHTableBytes.Set(float64(t.Bytes), t.Name)
		metrics.CHTableUncompressed.Set(float64(t.Uncompressed), t.Name)
		metrics.CHTableRows.Set(float64(t.Rows), t.Name)
		metrics.CHTableParts.Set(float64(t.Parts), t.Name)
		if age, ok := PartitionAge(t.OldestPartition, now); ok {
			metrics.CHOldestPartitionAge.Set(age, t.Name)
		}
	}
	metrics.BuildInfo.Reset()
	metrics.BuildInfo.Set(1, version.Version, runtime.Version(), info.Version)
}

// Snapshot returns the last sample.
func (c *Collector) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// PartitionAge turns a partition name (2026-10-01 daily, 202610 monthly, tuple or 'all')
// into days before now; false when it does not look like a date.
func PartitionAge(partition string, now time.Time) (float64, bool) {
	p := strings.Trim(strings.TrimSpace(partition), "'()")
	for _, layout := range []string{"2006-01-02", "200601", "20060102"} {
		if t, err := time.Parse(layout, p); err == nil {
			return now.Sub(t).Hours() / 24, true
		}
	}
	return 0, false
}
