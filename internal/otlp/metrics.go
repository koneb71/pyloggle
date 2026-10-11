package otlp

import (
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"

	"pyloggle/internal/model"
)

const (
	maxMetricNameBytes = 200
	maxMetricAttrKeys  = 16
)

// MetricsToPoints maps an OTLP metrics payload onto stored points. Gauges and sums keep their
// value; histograms keep count, sum, bounds and bucket counts; summaries and exponential
// histograms keep count and sum only (their quantiles and scaled buckets are not stored).
// Cardinality guards: names over 200 bytes are dropped, attribute sets are cut to 16 keys.
func MetricsToPoints(md *metricsv1.MetricsData, now time.Time) []model.MetricPoint {
	var out []model.MetricPoint
	for _, rm := range md.GetResourceMetrics() {
		ri := readResource(rm.GetResource())
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				name := m.GetName()
				if name == "" || len(name) > maxMetricNameBytes {
					continue
				}
				base := model.MetricPoint{Service: ri.service, Env: ri.env, Name: name, Unit: m.GetUnit()}
				switch d := m.GetData().(type) {
				case *metricsv1.Metric_Gauge:
					for _, dp := range d.Gauge.GetDataPoints() {
						p := base
						p.Type, p.Temporality = "gauge", "unspecified"
						fillNumber(&p, dp, now)
						out = append(out, p)
					}
				case *metricsv1.Metric_Sum:
					for _, dp := range d.Sum.GetDataPoints() {
						p := base
						p.Type, p.Monotonic, p.Temporality = "sum", d.Sum.GetIsMonotonic(), temporality(d.Sum.GetAggregationTemporality())
						fillNumber(&p, dp, now)
						out = append(out, p)
					}
				case *metricsv1.Metric_Histogram:
					for _, dp := range d.Histogram.GetDataPoints() {
						p := base
						p.Type, p.Temporality = "histogram", temporality(d.Histogram.GetAggregationTemporality())
						p.TS = pointTime(dp.GetTimeUnixNano(), now)
						p.Attrs, p.SeriesHash = pointAttrs(dp.GetAttributes())
						p.Count, p.Sum = dp.GetCount(), dp.GetSum()
						p.Bounds, p.BucketCounts = dp.GetExplicitBounds(), dp.GetBucketCounts()
						if len(p.BucketCounts) != len(p.Bounds)+1 {
							p.Bounds, p.BucketCounts = nil, nil // malformed: keep count/sum only
						}
						out = append(out, p)
					}
				case *metricsv1.Metric_ExponentialHistogram:
					for _, dp := range d.ExponentialHistogram.GetDataPoints() {
						p := base
						p.Type, p.Temporality = "histogram", temporality(d.ExponentialHistogram.GetAggregationTemporality())
						p.TS = pointTime(dp.GetTimeUnixNano(), now)
						p.Attrs, p.SeriesHash = pointAttrs(dp.GetAttributes())
						p.Count, p.Sum = dp.GetCount(), dp.GetSum()
						out = append(out, p)
					}
				case *metricsv1.Metric_Summary:
					for _, dp := range d.Summary.GetDataPoints() {
						p := base
						p.Type, p.Temporality = "summary", "cumulative"
						p.TS = pointTime(dp.GetTimeUnixNano(), now)
						p.Attrs, p.SeriesHash = pointAttrs(dp.GetAttributes())
						p.Count, p.Sum = dp.GetCount(), dp.GetSum()
						out = append(out, p)
					}
				}
			}
		}
	}
	return out
}

func fillNumber(p *model.MetricPoint, dp *metricsv1.NumberDataPoint, now time.Time) {
	p.TS = pointTime(dp.GetTimeUnixNano(), now)
	p.Attrs, p.SeriesHash = pointAttrs(dp.GetAttributes())
	switch v := dp.GetValue().(type) {
	case *metricsv1.NumberDataPoint_AsDouble:
		p.Value = v.AsDouble
	case *metricsv1.NumberDataPoint_AsInt:
		p.Value = float64(v.AsInt)
	}
}

func pointTime(n uint64, now time.Time) time.Time {
	if t := unixNano(n); !t.IsZero() {
		return t.Time
	}
	return now.UTC()
}

func temporality(t metricsv1.AggregationTemporality) string {
	switch t {
	case metricsv1.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA:
		return "delta"
	case metricsv1.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE:
		return "cumulative"
	}
	return "unspecified"
}

// pointAttrs renders the data point attributes (at most 16 keys, sorted) and hashes them so
// one time series has one stable identity.
func pointAttrs(kvs []*commonv1.KeyValue) (model.Attrs, uint64) {
	attrs := make(model.Attrs, len(kvs))
	for _, kv := range kvs {
		attrs[kv.GetKey()] = anyValueString(kv.GetValue())
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > maxMetricAttrKeys {
		for _, k := range keys[maxMetricAttrKeys:] {
			delete(attrs, k)
		}
		keys = keys[:maxMetricAttrKeys]
		attrs["pyloggle.attrs_truncated"] = "true"
	}
	return attrs, SeriesHash(keys, attrs)
}

// SeriesHash is FNV-64a over the sorted key=value pairs.
func SeriesHash(sortedKeys []string, attrs model.Attrs) uint64 {
	h := fnv.New64a()
	for _, k := range sortedKeys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(attrs[k]))
		_, _ = h.Write([]byte{1})
	}
	return h.Sum64()
}

// statusCode parses the semconv status attribute (0 when absent).
func statusCode(attrs model.Attrs) int {
	n, _ := strconv.Atoi(attrs[model.AttrHTTPStatusCode])
	return n
}
