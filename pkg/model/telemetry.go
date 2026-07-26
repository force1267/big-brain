package model

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Monitored wraps a Model with OTel metrics: call count (by outcome),
// call duration, and streamed chunk count, all tagged with the backing
// model's name. Instruments go through the global meter provider, so the
// wrapper is a no-op until telemetry is enabled — constructors wrap
// unconditionally.
func Monitored(m Model, name string) Model {
	meter := otel.Meter("big-brain/model")
	calls, err1 := meter.Int64Counter("model.calls")
	dur, err2 := meter.Float64Histogram("model.call.seconds")
	chunks, err3 := meter.Int64Counter("model.chunks")
	tools, err4 := meter.Int64Counter("model.tool.calls")
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return m // metrics must never break the model path
	}
	return monitoredModel{inner: m, name: name, calls: calls, dur: dur, chunks: chunks, tools: tools}
}

type monitoredModel struct {
	inner  Model
	name   string
	calls  metric.Int64Counter
	dur    metric.Float64Histogram
	chunks metric.Int64Counter
	tools  metric.Int64Counter // tool calls the model requested — the rate to watch when an agent loops
}

var _ Model = monitoredModel{}

// Stream implements Model, forwarding to the wrapped model.
func (m monitoredModel) Stream(ctx context.Context, msgs []Message, p Params) (<-chan Chunk, error) {
	start := time.Now()
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
		for c := range stream {
			switch {
			case c.Err != nil:
				outcome = "error"
			case c.Call != nil:
				m.tools.Add(ctx, 1, attrs, metric.WithAttributes(attribute.String("tool", c.Call.Name)))
			default:
				m.chunks.Add(ctx, 1, attrs)
			}
			out <- c
		}
		m.calls.Add(ctx, 1, attrs, metric.WithAttributes(attribute.String("outcome", outcome)))
		m.dur.Record(ctx, time.Since(start).Seconds(), attrs)
	}()
	return out, nil
}
