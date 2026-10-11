// Package metrics is a small, dependency-free Prometheus-style registry: counters, gauges and
// histograms with fixed label names, rendered in the text exposition format. It exists because
// the handful of families ingestd exposes do not justify the client_golang dependency tree.
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry holds families in registration order.
type Registry struct {
	mu       sync.Mutex
	families []family
}

// Default is the process-wide registry.
var Default = NewRegistry()

func NewRegistry() *Registry { return &Registry{} }

type family interface {
	name() string
	writeText(w io.Writer)
	snapshot(into map[string]float64)
}

func (r *Registry) register(f family) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.families {
		if existing.name() == f.name() {
			panic("metrics: duplicate family " + f.name())
		}
	}
	r.families = append(r.families, f)
}

// WriteText renders every family in Prometheus text exposition format (version 0.0.4).
func (r *Registry) WriteText(w io.Writer) {
	r.mu.Lock()
	fams := append([]family(nil), r.families...)
	r.mu.Unlock()
	for _, f := range fams {
		f.writeText(w)
	}
}

// Snapshot returns every series as "name{labels}" → value (for JSON consumers like the System page).
func (r *Registry) Snapshot() map[string]float64 {
	r.mu.Lock()
	fams := append([]family(nil), r.families...)
	r.mu.Unlock()
	out := map[string]float64{}
	for _, f := range fams {
		f.snapshot(out)
	}
	return out
}

// --- series storage shared by the three kinds -------------------------------------------

type desc struct {
	fqName string
	help   string
	labels []string
}

func (d desc) check(values []string) {
	if len(values) != len(d.labels) {
		panic(fmt.Sprintf("metrics: %s expects %d label values, got %d", d.fqName, len(d.labels), len(values)))
	}
}

// key joins label values with a separator that cannot appear in a sane label.
func key(values []string) string { return strings.Join(values, "\x1f") }

func splitKey(k string) []string {
	if k == "" {
		return nil
	}
	return strings.Split(k, "\x1f")
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func labelString(names, values []string, extra ...string) string {
	if len(names) == 0 && len(extra) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	write := func(n, v string) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(n)
		b.WriteString(`="`)
		b.WriteString(escapeLabel(v))
		b.WriteByte('"')
	}
	for i, n := range names {
		v := ""
		if i < len(values) {
			v = values[i]
		}
		write(n, v)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		write(extra[i], extra[i+1])
	}
	b.WriteByte('}')
	return b.String()
}

func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

func fmtFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func writeHeader(w io.Writer, d desc, kind string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", d.fqName, escapeHelp(d.help), d.fqName, kind)
}

func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// --- Counter --------------------------------------------------------------------------

type Counter struct {
	desc
	mu   sync.Mutex
	vals map[string]float64
}

// NewCounter registers a monotonically increasing family.
func (r *Registry) NewCounter(name, help string, labels ...string) *Counter {
	c := &Counter{desc: desc{fqName: name, help: help, labels: labels}, vals: map[string]float64{}}
	r.register(c)
	return c
}

func (c *Counter) Inc(labelValues ...string) { c.Add(1, labelValues...) }

func (c *Counter) Add(n float64, labelValues ...string) {
	c.check(labelValues)
	if n < 0 {
		return
	}
	c.mu.Lock()
	c.vals[key(labelValues)] += n
	c.mu.Unlock()
}

// Get returns the current value of one series (tests, System page).
func (c *Counter) Get(labelValues ...string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vals[key(labelValues)]
}

func (c *Counter) name() string { return c.fqName }

func (c *Counter) writeText(w io.Writer) {
	c.mu.Lock()
	vals := make(map[string]float64, len(c.vals))
	for k, v := range c.vals {
		vals[k] = v
	}
	c.mu.Unlock()
	writeHeader(w, c.desc, "counter")
	for _, k := range sortedKeys(vals) {
		fmt.Fprintf(w, "%s%s %s\n", c.fqName, labelString(c.labels, splitKey(k)), fmtFloat(vals[k]))
	}
}

func (c *Counter) snapshot(into map[string]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.vals {
		into[c.fqName+labelString(c.labels, splitKey(k))] = v
	}
}

// --- Gauge ----------------------------------------------------------------------------

type Gauge struct {
	desc
	mu   sync.Mutex
	vals map[string]float64
}

// NewGauge registers a family whose series can go up and down.
func (r *Registry) NewGauge(name, help string, labels ...string) *Gauge {
	g := &Gauge{desc: desc{fqName: name, help: help, labels: labels}, vals: map[string]float64{}}
	r.register(g)
	return g
}

func (g *Gauge) Set(v float64, labelValues ...string) {
	g.check(labelValues)
	g.mu.Lock()
	g.vals[key(labelValues)] = v
	g.mu.Unlock()
}

func (g *Gauge) Add(n float64, labelValues ...string) {
	g.check(labelValues)
	g.mu.Lock()
	g.vals[key(labelValues)] += n
	g.mu.Unlock()
}

func (g *Gauge) Get(labelValues ...string) float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.vals[key(labelValues)]
}

// Reset drops every series (collectors that re-populate on each tick).
func (g *Gauge) Reset() {
	g.mu.Lock()
	g.vals = map[string]float64{}
	g.mu.Unlock()
}

func (g *Gauge) name() string { return g.fqName }

func (g *Gauge) writeText(w io.Writer) {
	g.mu.Lock()
	vals := make(map[string]float64, len(g.vals))
	for k, v := range g.vals {
		vals[k] = v
	}
	g.mu.Unlock()
	writeHeader(w, g.desc, "gauge")
	for _, k := range sortedKeys(vals) {
		fmt.Fprintf(w, "%s%s %s\n", g.fqName, labelString(g.labels, splitKey(k)), fmtFloat(vals[k]))
	}
}

func (g *Gauge) snapshot(into map[string]float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, v := range g.vals {
		into[g.fqName+labelString(g.labels, splitKey(k))] = v
	}
}

// --- Histogram ------------------------------------------------------------------------

type histSeries struct {
	counts []uint64 // per bucket (non-cumulative)
	sum    float64
	count  uint64
}

type Histogram struct {
	desc
	buckets []float64 // upper bounds, ascending; +Inf implied
	mu      sync.Mutex
	series  map[string]*histSeries
}

// DefaultBuckets suits request/flush latencies in seconds.
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// NewHistogram registers a histogram family with the given upper bounds (+Inf is added).
func (r *Registry) NewHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	b := append([]float64(nil), buckets...)
	sort.Float64s(b)
	h := &Histogram{desc: desc{fqName: name, help: help, labels: labels}, buckets: b, series: map[string]*histSeries{}}
	r.register(h)
	return h
}

func (h *Histogram) Observe(v float64, labelValues ...string) {
	h.check(labelValues)
	h.mu.Lock()
	defer h.mu.Unlock()
	k := key(labelValues)
	s := h.series[k]
	if s == nil {
		s = &histSeries{counts: make([]uint64, len(h.buckets))}
		h.series[k] = s
	}
	for i, ub := range h.buckets {
		if v <= ub {
			s.counts[i]++
			break
		}
	}
	s.sum += v
	s.count++
}

// Quantile estimates q (0..1) for one series by linear interpolation inside the bucket.
func (h *Histogram) Quantile(q float64, labelValues ...string) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.series[key(labelValues)]
	if s == nil || s.count == 0 {
		return 0
	}
	target := q * float64(s.count)
	var cum float64
	lower := 0.0
	for i, ub := range h.buckets {
		c := float64(s.counts[i])
		if cum+c >= target {
			if c == 0 {
				return ub
			}
			return lower + (ub-lower)*((target-cum)/c)
		}
		cum += c
		lower = ub
	}
	return h.buckets[len(h.buckets)-1]
}

func (h *Histogram) name() string { return h.fqName }

func (h *Histogram) writeText(w io.Writer) {
	h.mu.Lock()
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	copies := make(map[string]histSeries, len(keys))
	for _, k := range keys {
		s := h.series[k]
		copies[k] = histSeries{counts: append([]uint64(nil), s.counts...), sum: s.sum, count: s.count}
	}
	h.mu.Unlock()
	writeHeader(w, h.desc, "histogram")
	for _, k := range keys {
		s := copies[k]
		lv := splitKey(k)
		var cum uint64
		for i, ub := range h.buckets {
			cum += s.counts[i]
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.fqName, labelString(h.labels, lv, "le", fmtFloat(ub)), cum)
		}
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.fqName, labelString(h.labels, lv, "le", "+Inf"), s.count)
		fmt.Fprintf(w, "%s_sum%s %s\n", h.fqName, labelString(h.labels, lv), fmtFloat(s.sum))
		fmt.Fprintf(w, "%s_count%s %d\n", h.fqName, labelString(h.labels, lv), s.count)
	}
}

func (h *Histogram) snapshot(into map[string]float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, s := range h.series {
		lv := splitKey(k)
		into[h.fqName+"_count"+labelString(h.labels, lv)] = float64(s.count)
		into[h.fqName+"_sum"+labelString(h.labels, lv)] = s.sum
	}
}
