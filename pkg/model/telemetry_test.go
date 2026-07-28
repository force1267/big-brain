package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// findMetric locates one exported metric by name, across every scope.
func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			if met.Name == name {
				return met, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

// setupReader installs a manual reader as the global MeterProvider and
// restores the noop provider on cleanup — the pattern every test below reuses.
func setupReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(noop.NewMeterProvider()) })
	return reader
}

func TestMonitoredDelegatesAndRecords(t *testing.T) {
	reader := setupReader(t)

	mock := &Mock{Chunks: []string{"a", "b"}}
	m := Monitored(mock, "gpt-test")
	stream, err := m.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got, err := Collect(stream)
	if err != nil || got != "ab" {
		t.Fatalf("delegation broken: %q, %v", got, err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			names[met.Name] = true
		}
	}
	// model.tokens is asserted separately (TestMonitoredRecordsTokensByKind):
	// this Mock reports no Usage, so that counter never records and — like
	// any instrument with zero data points — the SDK exports nothing for it.
	for _, want := range []string{
		"model.calls", "model.call.seconds", "model.chunks",
		"model.ttft.seconds", "model.generation.seconds", "model.usage.missing",
	} {
		if !names[want] {
			t.Fatalf("metric %q not recorded; got %v", want, names)
		}
	}
}

func TestMonitoredPropagatesRejection(t *testing.T) {
	boom := errors.New("boom")
	m := Monitored(&Mock{Reject: boom}, "gpt-test")
	if _, err := m.Stream(context.Background(), nil, Params{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}

func TestMonitoredPropagatesStreamError(t *testing.T) {
	boom := errors.New("boom")
	m := Monitored(&Mock{Chunks: []string{"a"}, Fail: boom}, "gpt-test")
	stream, err := m.Stream(context.Background(), nil, Params{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Collect(stream)
	if !errors.Is(err, boom) || got != "a" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// model.tokens carries a token.kind attribute per non-zero field of Usage,
// and a call that DID report usage does not increment model.usage.missing.
func TestMonitoredRecordsTokensByKind(t *testing.T) {
	reader := setupReader(t)

	mock := &Mock{Chunks: []string{"hi"}, Usage: &Usage{Input: 10, Output: 5, CacheRead: 2}}
	m := Monitored(mock, "gpt-test")
	stream, err := m.Stream(context.Background(), nil, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, _, _, err := CollectAll(stream); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	tokens, ok := findMetric(rm, "model.tokens")
	if !ok {
		t.Fatalf("model.tokens not recorded")
	}
	sum := tokens.Data.(metricdata.Sum[int64])
	got := map[string]int64{}
	for _, dp := range sum.DataPoints {
		kind, ok := dp.Attributes.Value(attribute.Key("token.kind"))
		if !ok {
			t.Fatalf("model.tokens data point missing token.kind: %+v", dp)
		}
		got[kind.AsString()] = dp.Value
	}
	want := map[string]int64{"input": 10, "output": 5, "cache_read": 2}
	for kind, v := range want {
		if got[kind] != v {
			t.Fatalf("token.kind=%s = %d, want %d (all: %v)", kind, got[kind], v, got)
		}
	}
	if _, hasWrite := got["cache_write"]; hasWrite {
		t.Fatalf("zero-valued kinds must not be recorded: %v", got)
	}

	// Usage was reported, so model.usage.missing's Add is never called for
	// this call — like any instrument with zero data points, the SDK exports
	// nothing for it at all.
	if missing, ok := findMetric(rm, "model.usage.missing"); ok {
		for _, dp := range missing.Data.(metricdata.Sum[int64]).DataPoints {
			if dp.Value != 0 {
				t.Fatalf("model.usage.missing = %d, want 0 (usage was reported)", dp.Value)
			}
		}
	}
}

// A call that reports no usage at all increments model.usage.missing — the
// honesty signal for a gap, never a silent zero.
func TestMonitoredUsageMissingIncrements(t *testing.T) {
	reader := setupReader(t)

	m := Monitored(&Mock{Chunks: []string{"hi"}}, "gpt-test")
	stream, err := m.Stream(context.Background(), nil, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := Collect(stream); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	missing, ok := findMetric(rm, "model.usage.missing")
	if !ok {
		t.Fatalf("model.usage.missing not recorded")
	}
	var total int64
	for _, dp := range missing.Data.(metricdata.Sum[int64]).DataPoints {
		total += dp.Value
	}
	if total != 1 {
		t.Fatalf("model.usage.missing total = %d, want 1", total)
	}
}

// model.tool.calls no longer carries a tool attribute — the forwarded call
// name is client-controlled and unbounded, unlike tool.exec.seconds' tool
// attribute (internal/agent), which is author-declared.
func TestMonitoredToolCallsHasNoToolAttribute(t *testing.T) {
	reader := setupReader(t)

	m := Monitored(&Mock{ToolCalls: [][]ToolCall{{{ID: "c1", Name: "read_sensor"}}}}, "gpt-test")
	stream, err := m.Stream(context.Background(), nil, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, _, _, err := CollectAll(stream); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	calls, ok := findMetric(rm, "model.tool.calls")
	if !ok {
		t.Fatalf("model.tool.calls not recorded")
	}
	for _, dp := range calls.Data.(metricdata.Sum[int64]).DataPoints {
		if _, has := dp.Attributes.Value(attribute.Key("tool")); has {
			t.Fatalf("model.tool.calls must not carry a tool attribute: %+v", dp.Attributes)
		}
	}
}

// TTFT is the gap to the FIRST content chunk; the generation window is the
// span from the first to the LAST content chunk — recorded with an injected
// stepping clock so the assertion is exact, never a real-time race.
func TestMonitoredTTFTAndGenerationWindow(t *testing.T) {
	reader := setupReader(t)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	steps := []time.Time{
		start,                             // Stream entry
		start.Add(100 * time.Millisecond), // first content chunk (TTFT)
		start.Add(150 * time.Millisecond), // second content chunk (generation end)
		start.Add(400 * time.Millisecond), // final dur.Record
	}
	i := 0
	clock := func() time.Time {
		tm := steps[i]
		if i < len(steps)-1 {
			i++
		}
		return tm
	}

	mm := Monitored(&Mock{Chunks: []string{"hel", "lo"}}, "gpt-test").(monitoredModel)
	mm.now = clock
	stream, err := mm.Stream(context.Background(), nil, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := Collect(stream); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	ttft, ok := findMetric(rm, "model.ttft.seconds")
	if !ok {
		t.Fatalf("model.ttft.seconds not recorded")
	}
	ttftData := ttft.Data.(metricdata.Histogram[float64])
	if len(ttftData.DataPoints) != 1 || ttftData.DataPoints[0].Sum != (100*time.Millisecond).Seconds() {
		t.Fatalf("ttft = %+v, want %v", ttftData.DataPoints, 100*time.Millisecond)
	}

	gen, ok := findMetric(rm, "model.generation.seconds")
	if !ok {
		t.Fatalf("model.generation.seconds not recorded")
	}
	genData := gen.Data.(metricdata.Histogram[float64])
	if len(genData.DataPoints) != 1 || genData.DataPoints[0].Sum != (50*time.Millisecond).Seconds() {
		t.Fatalf("generation = %+v, want %v", genData.DataPoints, 50*time.Millisecond)
	}

	dur, ok := findMetric(rm, "model.call.seconds")
	if !ok {
		t.Fatalf("model.call.seconds not recorded")
	}
	durData := dur.Data.(metricdata.Histogram[float64])
	if len(durData.DataPoints) != 1 || durData.DataPoints[0].Sum != (400*time.Millisecond).Seconds() {
		t.Fatalf("call.seconds = %+v, want %v", durData.DataPoints, 400*time.Millisecond)
	}
}

// Fewer than two content chunks means a zero-width generation window, which
// is deliberately NOT recorded (it would make the tokens/sec denominator a
// division by zero downstream).
func TestMonitoredSingleChunkSkipsGenerationWindow(t *testing.T) {
	reader := setupReader(t)

	m := Monitored(&Mock{Chunks: []string{"only"}}, "gpt-test")
	stream, err := m.Stream(context.Background(), nil, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := Collect(stream); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if gen, ok := findMetric(rm, "model.generation.seconds"); ok {
		if h := gen.Data.(metricdata.Histogram[float64]); len(h.DataPoints) != 0 {
			t.Fatalf("generation window recorded for a single-chunk reply: %+v", h.DataPoints)
		}
	}
}
