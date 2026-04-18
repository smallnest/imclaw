// Package metrics provides lightweight observability for IMClaw via structured logs.
//
// All metrics are emitted through Go's standard log package with a consistent
// [metrics] prefix and structured key=value pairs, making them grep-friendly
// and easy to feed into log aggregation pipelines.
package metrics

import (
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ---- Counters ----

// Counter is an atomically incremented metric counter.
type Counter struct {
	name  string
	count atomic.Int64
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	v := c.count.Add(1)
	log.Printf("[metrics] counter %s=%d", c.name, v)
}

// Add increments the counter by n. Panics if n is negative; counters
// must only increase. Use Gauge for values that can decrease.
func (c *Counter) Add(n int64) {
	if n < 0 {
		log.Printf("[metrics] counter %s: Add called with negative value %d, ignoring", c.name, n)
		return
	}
	v := c.count.Add(n)
	log.Printf("[metrics] counter %s=%d delta=%d", c.name, v, n)
}

// Value returns the current counter value.
func (c *Counter) Value() int64 {
	return c.count.Load()
}

// ---- Latency Tracker ----

// maxSamples caps the number of latency samples retained per tracker.
// Using a bounded ring buffer prevents unbounded memory growth in
// long-running processes.
const maxSamples = 1000

// LatencyTracker measures duration distributions for named operations.
// It retains at most maxSamples recent observations in a ring buffer.
type LatencyTracker struct {
	name    string
	mu      sync.Mutex
	samples [maxSamples]time.Duration
	head    int  // next write position
	count   int  // total observations written (capped at maxSamples)
}

// Observe records a duration and emits a structured log line.
func (lt *LatencyTracker) Observe(d time.Duration) {
	lt.mu.Lock()
	lt.samples[lt.head] = d
	lt.head = (lt.head + 1) % maxSamples
	if lt.count < maxSamples {
		lt.count++
	}
	lt.mu.Unlock()

	log.Printf("[metrics] latency %s duration_ms=%.2f", lt.name, float64(d)/float64(time.Millisecond))
}

// Since returns a duration from the given start time. It is a convenience
// wrapper intended for one-line usage: defer tracker.Since(time.Now())
func (lt *LatencyTracker) Since(start time.Time) {
	lt.Observe(time.Since(start))
}

// Summary returns aggregate statistics (count, min, max, avg, p50, p95, p99).
// Returns zero values if no samples have been recorded.
func (lt *LatencyTracker) Summary() LatencySummary {
	lt.mu.Lock()
	n := lt.count
	samples := make([]time.Duration, n)
	// Ring buffer: if count < maxSamples, data is in samples[0:count].
	// Otherwise, head marks the oldest entry.
	if n < maxSamples {
		copy(samples, lt.samples[:n])
	} else {
		copy(samples, lt.samples[lt.head:])
		copy(samples[maxSamples-lt.head:], lt.samples[:lt.head])
	}
	lt.mu.Unlock()

	return computeSummary(lt.name, samples)
}

// LatencySummary holds aggregate latency statistics.
type LatencySummary struct {
	Name  string
	Count int
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
	P50   time.Duration
	P95   time.Duration
	P99   time.Duration
}

// ---- Gauge ----

// Gauge tracks a point-in-time integer value.
type Gauge struct {
	name  string
	value atomic.Int64
}

// Set updates the gauge value.
func (g *Gauge) Set(v int64) {
	g.value.Store(v)
	log.Printf("[metrics] gauge %s=%d", g.name, v)
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() int64 {
	v := g.value.Add(1)
	log.Printf("[metrics] gauge %s=%d", g.name, v)
	return v
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() int64 {
	v := g.value.Add(-1)
	log.Printf("[metrics] gauge %s=%d", g.name, v)
	return v
}

// Value returns the current gauge value.
func (g *Gauge) Value() int64 {
	return g.value.Load()
}

// ---- Registry ----

// Registry groups named metrics for a subsystem.
type Registry struct {
	mu       sync.Mutex
	counters map[string]*Counter
	latency  map[string]*LatencyTracker
	gauges   map[string]*Gauge
}

// NewRegistry creates a new metrics registry.
func NewRegistry() *Registry {
	return &Registry{
		counters: make(map[string]*Counter),
		latency:  make(map[string]*LatencyTracker),
		gauges:   make(map[string]*Gauge),
	}
}

// Counter returns (or creates) a counter by name.
func (r *Registry) Counter(name string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{name: name}
	r.counters[name] = c
	return c
}

// Latency returns (or creates) a latency tracker by name.
func (r *Registry) Latency(name string) *LatencyTracker {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lt, ok := r.latency[name]; ok {
		return lt
	}
	lt := &LatencyTracker{name: name}
	r.latency[name] = lt
	return lt
}

// Gauge returns (or creates) a gauge by name.
func (r *Registry) Gauge(name string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &Gauge{name: name}
	r.gauges[name] = g
	return g
}

// Snapshot returns a point-in-time snapshot of all metrics.
func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snap := Snapshot{
		Counters: make(map[string]int64, len(r.counters)),
		Gauges:   make(map[string]int64, len(r.gauges)),
		Latency:  make(map[string]LatencySummary, len(r.latency)),
	}
	for name, c := range r.counters {
		snap.Counters[name] = c.Value()
	}
	for name, g := range r.gauges {
		snap.Gauges[name] = g.Value()
	}
	for name, lt := range r.latency {
		snap.Latency[name] = lt.Summary()
	}
	return snap
}

// Snapshot is a point-in-time view of all registry metrics.
type Snapshot struct {
	Counters map[string]int64
	Gauges   map[string]int64
	Latency  map[string]LatencySummary
}

// ---- Global default registry ----

var defaultRegistry = NewRegistry()

// Default returns the global default metrics registry.
func Default() *Registry {
	return defaultRegistry
}

// Predefined metric names following dashboard-friendly conventions.
// Naming: <subsystem>.<metric_name>
const (
	// Session metrics
	SessionCreated       = "session.created"
	SessionDeleted       = "session.deleted"
	SessionActive        = "session.active_count"
	SessionPromptLatency = "session.prompt_latency"

	// Request metrics
	RequestTotal     = "request.total"
	RequestLatency   = "request.latency"
	RequestErrors    = "request.errors"

	// Tool metrics
	ToolCallCount    = "tool.call_count"
	ToolCallDuration = "tool.call_duration"
	ToolCallErrors   = "tool.call_errors"

	// Permission metrics
	PermissionDenials = "permission.denials"

	// Agent metrics
	AgentExecDuration = "agent.exec_duration"
	AgentExecFailures = "agent.exec_failures"

	// Output metrics
	OutputSize = "output.size_bytes"

	// Job metrics
	JobSubmitted  = "job.submitted"
	JobCompleted  = "job.completed"
	JobFailed     = "job.failed"
	JobDuration   = "job.duration"

	// Connection metrics
	WSConnections    = "ws.active_connections"
	WSSubscribers    = "ws.active_subscribers"
	WSDroppedSubs    = "ws.dropped_subscribers"
)

// ---- Event logging helpers ----

// LogEvent emits a structured event log line for key operational events.
func LogEvent(event, sessionID, requestID string, extra map[string]interface{}) {
	pairs := make([]interface{}, 0, 2+len(extra)*2)
	pairs = append(pairs, "event", event)
	if sessionID != "" {
		pairs = append(pairs, "session_id", sessionID)
	}
	if requestID != "" {
		pairs = append(pairs, "request_id", requestID)
	}
	for k, v := range extra {
		pairs = append(pairs, k, v)
	}
	log.Printf("[metrics] event "+repeatFormat(len(pairs)/2), pairs...)
}

func repeatFormat(n int) string {
	const pair = " %s=%v"
	result := ""
	for i := 0; i < n; i++ {
		result += pair
	}
	return result
}

// ---- Internal helpers ----

func computeSummary(name string, samples []time.Duration) LatencySummary {
	s := LatencySummary{
		Name:  name,
		Count: len(samples),
	}
	if len(samples) == 0 {
		return s
	}

	// Sort a copy to compute percentiles
	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Sort(sortableDurations(sorted))

	n := len(sorted)
	s.Min = sorted[0]
	s.Max = sorted[n-1]

	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	s.Avg = total / time.Duration(n)
	s.P50 = sorted[n*50/100]
	s.P95 = sorted[n*95/100]
	s.P99 = sorted[n*99/100]

	return s
}

type sortableDurations []time.Duration

func (d sortableDurations) Len() int           { return len(d) }
func (d sortableDurations) Less(i, j int) bool { return d[i] < d[j] }
func (d sortableDurations) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }

// Truncate shortens s to at most maxLen bytes, appending "..." if truncated.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
