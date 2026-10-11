package sysinfo

import (
	"context"
	"errors"
	"testing"
	"time"

	"pyloggle/internal/metrics"
	"pyloggle/internal/storage/clickhouse"
)

type fakeSource struct {
	info clickhouse.SystemInfo
	err  error
}

func (f fakeSource) SystemInfo(context.Context) (clickhouse.SystemInfo, error) { return f.info, f.err }

func TestCollectorSetsGaugesAndSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	src := fakeSource{info: clickhouse.SystemInfo{Version: "24.3.18.7", DiskFree: 10, DiskTotal: 100, MutationsPending: 2,
		Tables: []clickhouse.TableInfo{{Name: "logs_raw", Rows: 5, Bytes: 500, Uncompressed: 2000, Parts: 3, OldestPartition: "2026-10-01"}, {Name: "projects", Rows: 1, OldestPartition: "all"}}}}
	c := New(src, time.Minute)
	c.now = func() time.Time { return now }
	c.Collect(context.Background())
	snap := c.Snapshot()
	if !snap.Up || snap.Info.Version != "24.3.18.7" || len(snap.Info.Tables) != 2 || snap.At != now {
		t.Fatalf("snapshot: %+v", snap)
	}
	if metrics.CHUp.Get() != 1 || metrics.CHTableBytes.Get("logs_raw") != 500 || metrics.CHTableParts.Get("logs_raw") != 3 || metrics.CHMutationsPending.Get() != 2 || metrics.CHDiskFree.Get() != 10 {
		t.Fatalf("gauges: up=%v bytes=%v parts=%v", metrics.CHUp.Get(), metrics.CHTableBytes.Get("logs_raw"), metrics.CHTableParts.Get("logs_raw"))
	}
	if age := metrics.CHOldestPartitionAge.Get("logs_raw"); age != 10 {
		t.Fatalf("oldest partition age: %v", age)
	}
	if metrics.CHOldestPartitionAge.Get("projects") != 0 {
		t.Fatal("'all' partition has no age")
	}
	// A failing probe keeps the last snapshot but flags it.
	c.src = fakeSource{err: errors.New("down")}
	c.Collect(context.Background())
	snap = c.Snapshot()
	if snap.Up || snap.Error != "down" || snap.Info.Version != "24.3.18.7" || metrics.CHUp.Get() != 0 {
		t.Fatalf("failed probe: %+v", snap)
	}
}

func TestPartitionAge(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	for in, want := range map[string]float64{"2026-10-01": 10, "202609": 40, "20261001": 10, "'2026-10-01'": 10} {
		if got, ok := PartitionAge(in, now); !ok || got != want {
			t.Fatalf("%s: %v %v", in, got, ok)
		}
	}
	if _, ok := PartitionAge("tuple()", now); ok {
		t.Fatal("non-date partitions have no age")
	}
}
