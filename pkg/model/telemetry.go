package model

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// latencyBuckets bounds model.call.seconds and model.generation.seconds: an
// LLM call runs 50ms..2min, not the OTel RPC default's millisecond scale.
var latencyBuckets = metric.WithExplicitBucketBoundaries(
	0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120)

// ttftBuckets bounds model.ttft.seconds: denser at the low end, since that is
// where a perceptible regression shows up.
var ttftBuckets = metric.WithExplicitBucketBoundaries(
	0.025, 0.05, 0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8, 30)

// tokenKinds enumerates model.tokens' token.kind attribute values, in the
// fixed order recordTokens reports them.
var tokenKinds = [...]struct {
	kind string
	get  func(Usage) int64
}{
	{"input", func(u Usage) int64 { return u.Input }},
	{"output", func(u Usage) int64 { return u.Output }},
	{"cache_read", func(u Usage) int64 { return u.CacheRead }},
	{"cache_write", func(u Usage) int64 { return u.CacheWrite }},
	{"reasoning", func(u Usage) int64 { return u.Reasoning }},
}

// Monitored wraps a Model with OTel metrics: call count (by outcome), call
// duration, streamed chunk count, tool-call count, spent tokens, time to
// first token, the generation window, and a counter for calls whose provider
// reported no usage at all. Instruments go through the global meter provider,
// so the wrapper is a no-op until telemetry is enabled — constructors wrap
// unconditionally.
func Monitored(m Model, name string) Model {
	meter := otel.Meter("big-brain/model")
	calls, err1 := meter.Int64Counter("model.calls")
	dur, err2 := meter.Float64Histogram("model.call.seconds", latencyBuckets)
	chunks, err3 := meter.Int64Counter("model.chunks")
	tools, err4 := meter.Int64Counter("model.tool.calls")
	tokens, err5 := meter.Int64Counter("model.tokens")
	ttft, err6 := meter.Float64Histogram("model.ttft.seconds", ttftBuckets)
	generation, err7 := meter.Float64Histogram("model.generation.seconds", latencyBuckets)
	usageMissing, err8 := meter.Int64Counter("model.usage.missing")
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil ||
		err5 != nil || err6 != nil || err7 != nil || err8 != nil {
		return m // metrics must never break the model path
	}
	return monitoredModel{
		inner: m, name: name,
		calls: calls, dur: dur, chunks: chunks, tools: tools,
		tokens: tokens, ttft: ttft, generation: generation, usageMissing: usageMissing,
	}
}

type monitoredModel struct {
	inner        Model
	name         string
	calls        metric.Int64Counter
	dur          metric.Float64Histogram
	chunks       metric.Int64Counter
	tools        metric.Int64Counter // tool calls the model requested — the rate to watch when an agent loops
	tokens       metric.Int64Counter
	ttft         metric.Float64Histogram
	generation   metric.Float64Histogram
	usageMissing metric.Int64Counter
	// now is nil in production (time.Now); a test substitutes a stepping fake
	// for deterministic timing assertions without a Clock interface.
	now func() time.Time
}

var _ Model = monitoredModel{}

func (m monitoredModel) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// recordTokens adds each present token.kind to model.tokens.
func (m monitoredModel) recordTokens(ctx context.Context, attrs metric.MeasurementOption, u Usage) {
	for _, k := range tokenKinds {
		if v := k.get(u); v != 0 {
			m.tokens.Add(ctx, v, attrs, metric.WithAttributes(attribute.String("token.kind", k.kind)))
		}
	}
}

// Stream implements Model, forwarding to the wrapped model.
func (m monitoredModel) Stream(ctx context.Context, msgs []Message, p Params) (<-chan Chunk, error) {
	start := m.clock()
	attrs := metric.WithAttributes(attribute.String("model", m.name))
	stream, err := m.inner.Stream(ctx, msgs, p)
	if err != nil {
		m.calls.Add(ctx, 1, attrs, metric.WithAttributes(attribute.String("outcome", "rejected")))
		return nil, err
	}
	out := make(chan Chunk)
	go func() {
		defer close(out)
		outcome := "ok"
		var first, last time.Time
		var contentChunks int
		sawUsage := false
		for c := range stream {
			switch {
			case c.Err != nil:
				outcome = "error"
			case c.Usage != nil:
				m.recordTokens(ctx, attrs, *c.Usage)
				sawUsage = true
			case c.Call != nil:
				m.tools.Add(ctx, 1, attrs)
			case c.Content != "":
				now := m.clock()
				if contentChunks == 0 {
					m.ttft.Record(ctx, now.Sub(start).Seconds(), attrs)
					first = now
				}
				last = now
				contentChunks++
				m.chunks.Add(ctx, 1, attrs)
			}
			out <- c
		}
		if contentChunks >= 2 {
			m.generation.Record(ctx, last.Sub(first).Seconds(), attrs)
		}
		if !sawUsage {
			m.usageMissing.Add(ctx, 1, attrs)
		}
		m.calls.Add(ctx, 1, attrs, metric.WithAttributes(attribute.String("outcome", outcome)))
		m.dur.Record(ctx, m.clock().Sub(start).Seconds(), attrs)
	}()
	return out, nil
}
