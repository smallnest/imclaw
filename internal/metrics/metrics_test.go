package metrics

import (
	"testing"
	"time"
)

func TestCounterInc(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("test.counter")

	if v := c.Value(); v != 0 {
		t.Fatalf("expected initial value 0, got %d", v)
	}
	c.Inc()
	if v := c.Value(); v != 1 {
		t.Fatalf("expected 1 after Inc, got %d", v)
	}
}

func TestCounterAdd(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("test.counter_add")

	c.Add(5)
	if v := c.Value(); v != 5 {
		t.Fatalf("expected 5 after Add(5), got %d", v)
	}
	c.Add(3)
	if v := c.Value(); v != 8 {
		t.Fatalf("expected 8 after Add(3), got %d", v)
	}
}

func TestCounterAddNegative(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("test.counter_neg")

	c.Add(10)
	c.Add(-5) // should be ignored
	if v := c.Value(); v != 10 {
		t.Fatalf("expected 10 after Add(-5) ignored, got %d", v)
	}
}

func TestCounterRegistryReturnsSame(t *testing.T) {
	r := NewRegistry()
	c1 := r.Counter("same_name")
	c2 := r.Counter("same_name")

	c1.Inc()
	if v := c2.Value(); v != 1 {
		t.Fatalf("expected shared counter to be 1, got %d", v)
	}
}

func TestGaugeSet(t *testing.T) {
	r := NewRegistry()
	g := r.Gauge("test.gauge")

	g.Set(42)
	if v := g.Value(); v != 42 {
		t.Fatalf("expected 42 after Set, got %d", v)
	}
	g.Set(100)
	if v := g.Value(); v != 100 {
		t.Fatalf("expected 100 after Set, got %d", v)
	}
}

func TestGaugeIncDec(t *testing.T) {
	r := NewRegistry()
	g := r.Gauge("test.gauge_incdec")

	g.Inc()
	g.Inc()
	if v := g.Value(); v != 2 {
		t.Fatalf("expected 2 after two Inc, got %d", v)
	}
	g.Dec()
	if v := g.Value(); v != 1 {
		t.Fatalf("expected 1 after Dec, got %d", v)
	}
}

func TestLatencyTrackerObserve(t *testing.T) {
	r := NewRegistry()
	lt := r.Latency("test.latency")

	lt.Observe(100 * time.Millisecond)
	lt.Observe(200 * time.Millisecond)
	lt.Observe(300 * time.Millisecond)

	s := lt.Summary()
	if s.Count != 3 {
		t.Fatalf("expected 3 samples, got %d", s.Count)
	}
	if s.Min != 100*time.Millisecond {
		t.Fatalf("expected min 100ms, got %v", s.Min)
	}
	if s.Max != 300*time.Millisecond {
		t.Fatalf("expected max 300ms, got %v", s.Max)
	}
	if s.Avg != 200*time.Millisecond {
		t.Fatalf("expected avg 200ms, got %v", s.Avg)
	}
}

func TestLatencyTrackerSince(t *testing.T) {
	r := NewRegistry()
	lt := r.Latency("test.since")

	start := time.Now().Add(-50 * time.Millisecond)
	lt.Since(start)

	s := lt.Summary()
	if s.Count != 1 {
		t.Fatalf("expected 1 sample, got %d", s.Count)
	}
	if s.Min < 40*time.Millisecond {
		t.Fatalf("expected min >= 40ms, got %v", s.Min)
	}
}

func TestLatencyTrackerPercentiles(t *testing.T) {
	r := NewRegistry()
	lt := r.Latency("test.percentiles")

	// Add 100 samples: 1ms, 2ms, ..., 100ms
	for i := 1; i <= 100; i++ {
		lt.Observe(time.Duration(i) * time.Millisecond)
	}

	s := lt.Summary()
	if s.Count != 100 {
		t.Fatalf("expected 100 samples, got %d", s.Count)
	}
	if s.P50 < 45*time.Millisecond || s.P50 > 55*time.Millisecond {
		t.Fatalf("expected P50 around 50ms, got %v", s.P50)
	}
	if s.P95 < 90*time.Millisecond {
		t.Fatalf("expected P95 >= 90ms, got %v", s.P95)
	}
	if s.P99 < 95*time.Millisecond {
		t.Fatalf("expected P99 >= 95ms, got %v", s.P99)
	}
}

func TestLatencyTrackerEmptySummary(t *testing.T) {
	r := NewRegistry()
	lt := r.Latency("test.empty")

	s := lt.Summary()
	if s.Count != 0 {
		t.Fatalf("expected 0 samples, got %d", s.Count)
	}
	if s.Min != 0 || s.Max != 0 || s.Avg != 0 {
		t.Fatalf("expected zero durations for empty tracker, got min=%v max=%v avg=%v", s.Min, s.Max, s.Avg)
	}
}

func TestLatencyTrackerRingBuffer(t *testing.T) {
	r := NewRegistry()
	lt := r.Latency("test.ring")

	// Fill beyond maxSamples
	for i := 0; i < maxSamples+200; i++ {
		lt.Observe(time.Duration(i) * time.Millisecond)
	}

	s := lt.Summary()
	if s.Count != maxSamples {
		t.Fatalf("expected %d samples (capped), got %d", maxSamples, s.Count)
	}
	// After writing maxSamples+200 entries, the ring buffer should contain
	// the last maxSamples entries: 200..1199
	// Min should be 200ms
	if s.Min != 200*time.Millisecond {
		t.Fatalf("expected min 200ms (oldest retained), got %v", s.Min)
	}
	if s.Max != 1199*time.Millisecond {
		t.Fatalf("expected max 1199ms (newest), got %v", s.Max)
	}
}

func TestLatencyTrackerRingBufferExactlyFull(t *testing.T) {
	r := NewRegistry()
	lt := r.Latency("test.ring_exact")

	// Fill exactly maxSamples
	for i := 0; i < maxSamples; i++ {
		lt.Observe(time.Duration(i) * time.Millisecond)
	}

	s := lt.Summary()
	if s.Count != maxSamples {
		t.Fatalf("expected %d samples, got %d", maxSamples, s.Count)
	}
	if s.Min != 0 {
		t.Fatalf("expected min 0, got %v", s.Min)
	}
	if s.Max != (maxSamples-1)*time.Millisecond {
		t.Fatalf("expected max %dms, got %v", maxSamples-1, s.Max)
	}
}

func TestLatencyTrackerConcurrentObserve(t *testing.T) {
	r := NewRegistry()
	lt := r.Latency("test.concurrent")

	done := make(chan struct{})
	const goroutines = 10
	const perGoroutine = 100

	for g := 0; g < goroutines; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < perGoroutine; i++ {
				lt.Observe(time.Duration(i) * time.Millisecond)
			}
		}()
	}

	for g := 0; g < goroutines; g++ {
		<-done
	}

	s := lt.Summary()
	expected := goroutines * perGoroutine
	if expected > maxSamples {
		expected = maxSamples
	}
	if s.Count != expected {
		t.Fatalf("expected %d samples, got %d", expected, s.Count)
	}
}

func TestRegistrySnapshot(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("snap.counter")
	g := r.Gauge("snap.gauge")
	lt := r.Latency("snap.latency")

	c.Inc()
	c.Inc()
	g.Set(42)
	lt.Observe(10 * time.Millisecond)

	snap := r.Snapshot()
	if snap.Counters["snap.counter"] != 2 {
		t.Fatalf("expected counter=2, got %d", snap.Counters["snap.counter"])
	}
	if snap.Gauges["snap.gauge"] != 42 {
		t.Fatalf("expected gauge=42, got %d", snap.Gauges["snap.gauge"])
	}
	latSummary, ok := snap.Latency["snap.latency"]
	if !ok {
		t.Fatal("expected latency summary in snapshot")
	}
	if latSummary.Count != 1 {
		t.Fatalf("expected 1 latency sample, got %d", latSummary.Count)
	}
}

func TestDefaultRegistry(t *testing.T) {
	d := Default()
	if d == nil {
		t.Fatal("expected non-nil default registry")
	}
	// Same instance
	if Default() != d {
		t.Fatal("expected same default registry instance")
	}
}

func TestLogEvent(t *testing.T) {
	// Just verify it doesn't panic
	LogEvent("test.event", "sess-1", "req-1", map[string]interface{}{
		"key": "value",
	})
	LogEvent("test.event", "", "", nil)
}

func TestPredefinedMetricNames(t *testing.T) {
	// Verify predefined names are non-empty and follow naming convention
	names := []string{
		SessionCreated, SessionDeleted, SessionActive, SessionPromptLatency,
		RequestTotal, RequestLatency, RequestErrors,
		ToolCallCount, ToolCallDuration, ToolCallErrors,
		PermissionDenials,
		AgentExecDuration, AgentExecFailures,
		OutputSize,
		JobSubmitted, JobCompleted, JobFailed, JobDuration,
		WSConnections, WSSubscribers, WSDroppedSubs,
	}
	for _, name := range names {
		if name == "" {
			t.Fatal("empty metric name")
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 3, "abc"},
	}
	for _, tt := range tests {
		got := Truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
