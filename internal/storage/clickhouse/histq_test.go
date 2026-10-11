package clickhouse

import (
	"math"
	"testing"
)

func TestHistogramQuantile(t *testing.T) {
	bounds := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}
	counts := []uint64{10, 20, 30, 20, 10, 5, 3, 1, 1} // 100 samples
	p50, ok := HistogramQuantile(0.5, bounds, counts)
	if !ok || p50 < 0.01 || p50 > 0.025 {
		t.Fatalf("p50: %v %v", p50, ok)
	}
	// Rank 50 falls 20 samples into the third bucket (30 samples wide): 0.01 + 0.015 * 20/30.
	if math.Abs(p50-0.02) > 1e-9 {
		t.Fatalf("p50 interpolation: %v", p50)
	}
	p95, _ := HistogramQuantile(0.95, bounds, counts)
	if p95 < 0.25 || p95 > 0.5 {
		t.Fatalf("p95: %v", p95)
	}
	p999, _ := HistogramQuantile(0.999, bounds, counts)
	if p999 != 1 {
		t.Fatalf("+Inf bucket reports the last finite bound: %v", p999)
	}
	if _, ok := HistogramQuantile(0.5, bounds, make([]uint64, 9)); ok {
		t.Fatal("empty histogram must report no value")
	}
	if _, ok := HistogramQuantile(0.5, bounds, []uint64{1, 2}); ok {
		t.Fatal("mismatched lengths must report no value")
	}
	// Single-bucket interpolation: 4 samples in [0, 10): p50 = 5.
	if v, _ := HistogramQuantile(0.5, []float64{10}, []uint64{4, 0}); v != 5 {
		t.Fatalf("interpolation: %v", v)
	}
}

func TestCounterDiffs(t *testing.T) {
	if got := diffCounts([]uint64{5, 7}, []uint64{2, 3}); got[0] != 3 || got[1] != 4 {
		t.Fatalf("diff: %v", got)
	}
	if got := diffCounts([]uint64{1, 9}, []uint64{2, 3}); got[0] != 1 || got[1] != 9 {
		t.Fatalf("reset must keep the current counts: %v", got)
	}
	if got := addCounts([]uint64{1}, []uint64{2, 3}); len(got) != 2 || got[0] != 3 || got[1] != 3 {
		t.Fatalf("add grows: %v", got)
	}
}
