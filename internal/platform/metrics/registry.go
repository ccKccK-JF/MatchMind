package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type collector interface {
	name() string
	write(io.Writer)
}

// Registry is a small dependency-free Prometheus text exposition registry.
// MatchMind currently needs only unlabelled counters, gauges, and histograms.
type Registry struct {
	mu         sync.RWMutex
	collectors map[string]collector
}

func NewRegistry() *Registry {
	return &Registry{collectors: make(map[string]collector)}
}

func (r *Registry) NewCounter(name, help string) *Counter {
	counter := &Counter{metricName: name, help: help}
	r.register(counter)
	return counter
}

func (r *Registry) NewGauge(name, help string) *Gauge {
	gauge := &Gauge{metricName: name, help: help}
	r.register(gauge)
	return gauge
}

func (r *Registry) NewHistogram(name, help string, buckets []float64) *Histogram {
	copyBuckets := append([]float64(nil), buckets...)
	sort.Float64s(copyBuckets)
	histogram := &Histogram{
		metricName: name,
		help:       help,
		buckets:    copyBuckets,
		counts:     make([]uint64, len(copyBuckets)),
	}
	r.register(histogram)
	return histogram
}

func (r *Registry) register(value collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.collectors[value.name()]; exists {
		panic("metrics: duplicate collector " + value.name())
	}
	r.collectors[value.name()] = value
}

func (r *Registry) ServeHTTP(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	r.mu.RLock()
	values := make([]collector, 0, len(r.collectors))
	for _, value := range r.collectors {
		values = append(values, value)
	}
	r.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return values[i].name() < values[j].name() })
	for _, value := range values {
		value.write(response)
	}
}

type Counter struct {
	metricName string
	help       string
	value      atomic.Uint64
}

func (c *Counter) Inc()             { c.Add(1) }
func (c *Counter) Add(delta uint64) { c.value.Add(delta) }
func (c *Counter) Value() uint64    { return c.value.Load() }
func (c *Counter) name() string     { return c.metricName }
func (c *Counter) write(w io.Writer) {
	writeScalar(w, c.metricName, c.help, "counter", float64(c.Value()))
}

type Gauge struct {
	metricName string
	help       string
	bits       atomic.Uint64
}

func (g *Gauge) Set(value float64) { g.bits.Store(math.Float64bits(value)) }
func (g *Gauge) Value() float64    { return math.Float64frombits(g.bits.Load()) }
func (g *Gauge) name() string      { return g.metricName }
func (g *Gauge) write(w io.Writer) { writeScalar(w, g.metricName, g.help, "gauge", g.Value()) }

type Histogram struct {
	metricName string
	help       string
	buckets    []float64

	mu     sync.RWMutex
	counts []uint64
	count  uint64
	sum    float64
}

func (h *Histogram) Observe(value float64) {
	if math.IsNaN(value) {
		return
	}
	h.mu.Lock()
	for index, upperBound := range h.buckets {
		if value <= upperBound {
			h.counts[index]++
		}
	}
	h.count++
	h.sum += value
	h.mu.Unlock()
}

func (h *Histogram) name() string { return h.metricName }

func (h *Histogram) write(w io.Writer) {
	h.mu.RLock()
	counts := append([]uint64(nil), h.counts...)
	count := h.count
	sum := h.sum
	h.mu.RUnlock()

	fmt.Fprintf(w, "# HELP %s %s\n", h.metricName, escapeHelp(h.help))
	fmt.Fprintf(w, "# TYPE %s histogram\n", h.metricName)
	for index, upperBound := range h.buckets {
		fmt.Fprintf(w, "%s_bucket{le=%q} %d\n", h.metricName, formatFloat(upperBound), counts[index])
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", h.metricName, count)
	fmt.Fprintf(w, "%s_sum %s\n", h.metricName, formatFloat(sum))
	fmt.Fprintf(w, "%s_count %d\n", h.metricName, count)
}

func writeScalar(w io.Writer, name, help, metricType string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(help))
	fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType)
	fmt.Fprintf(w, "%s %s\n", name, formatFloat(value))
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func escapeHelp(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "\n", "\\n")
}
