package model

import "time"

// MetricPoint is one OTLP metric data point as stored in metrics_points. Gauges and sums carry
// Value; histograms carry Count, Sum, Bounds and BucketCounts; summaries carry Count and Sum.
type MetricPoint struct {
	ProjectID string `json:"project_id"`
	Service   string `json:"service"`
	Env       string `json:"env"`

	Name        string `json:"name"`
	Unit        string `json:"unit,omitempty"`
	Type        string `json:"type"`        // gauge | sum | histogram | summary
	Monotonic   bool   `json:"monotonic"`   // sums only
	Temporality string `json:"temporality"` // unspecified | delta | cumulative
	SeriesHash  uint64 `json:"series_hash"`
	Attrs       Attrs  `json:"attrs,omitempty"`

	TS           time.Time `json:"ts"`
	Value        float64   `json:"value"`
	Count        uint64    `json:"count,omitempty"`
	Sum          float64   `json:"sum,omitempty"`
	Bounds       []float64 `json:"bounds,omitempty"`
	BucketCounts []uint64  `json:"bucket_counts,omitempty"`
}

// Metric types and temporalities as stored (Enum8 names in ClickHouse).
var (
	MetricTypes         = []string{"gauge", "sum", "histogram", "summary"}
	MetricTemporalities = []string{"unspecified", "delta", "cumulative"}
)
