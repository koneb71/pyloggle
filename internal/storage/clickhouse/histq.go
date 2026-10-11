package clickhouse

import "math"

// HistogramQuantile estimates quantile q (0..1) from explicit-bounds histogram buckets the way
// Prometheus' histogram_quantile does: find the bucket holding the rank, interpolate linearly
// inside it. counts has one more element than bounds (the last one is the +Inf bucket, whose
// value is reported as the highest finite bound). Returns false for an empty histogram.
func HistogramQuantile(q float64, bounds []float64, counts []uint64) (float64, bool) {
	if len(counts) == 0 || len(counts) != len(bounds)+1 {
		return 0, false
	}
	var total uint64
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return 0, false
	}
	if q <= 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	rank := q * float64(total)
	var before float64
	for i, c := range counts {
		cum := before + float64(c)
		if cum >= rank && c > 0 {
			if i == len(bounds) { // +Inf bucket
				if len(bounds) == 0 {
					return 0, false
				}
				return bounds[len(bounds)-1], true
			}
			lower := 0.0
			if i > 0 {
				lower = bounds[i-1]
			}
			upper := bounds[i]
			if upper < lower || math.IsInf(upper, 0) {
				return lower, true
			}
			frac := (rank - before) / float64(c)
			if frac < 0 {
				frac = 0
			}
			if frac > 1 {
				frac = 1
			}
			return lower + (upper-lower)*frac, true
		}
		before = cum
	}
	if len(bounds) == 0 {
		return 0, false
	}
	return bounds[len(bounds)-1], true
}

// diffCounts returns cur - prev element-wise, or cur when any element decreased (a counter
// reset) or the lengths differ.
func diffCounts(cur, prev []uint64) []uint64 {
	if len(prev) != len(cur) {
		return cur
	}
	out := make([]uint64, len(cur))
	for i := range cur {
		if cur[i] < prev[i] {
			return cur
		}
		out[i] = cur[i] - prev[i]
	}
	return out
}

// addCounts adds b into a (growing a when b is longer).
func addCounts(a, b []uint64) []uint64 {
	if len(b) > len(a) {
		n := make([]uint64, len(b))
		copy(n, a)
		a = n
	}
	for i, v := range b {
		a[i] += v
	}
	return a
}
