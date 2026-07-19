package memory

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Monitored wraps a Memory with OTel metrics: facts remembered and
// recalls served, by outcome. No-op until telemetry is enabled (global
// meter provider), so constructors wrap unconditionally.
func Monitored(m Memory) Memory {
	meter := otel.Meter("big-brain/memory")
	remembered, err1 := meter.Int64Counter("memory.remembered")
	recalls, err2 := meter.Int64Counter("memory.recalls")
	if err1 != nil || err2 != nil {
		return m
	}
	return monitoredMemory{inner: m, remembered: remembered, recalls: recalls}
}

type monitoredMemory struct {
	inner      Memory
	remembered metric.Int64Counter
	recalls    metric.Int64Counter
}

var _ Memory = monitoredMemory{}

func outcome(err error) metric.MeasurementOption {
	if err != nil {
		return metric.WithAttributes(attribute.String("outcome", "error"))
	}
	return metric.WithAttributes(attribute.String("outcome", "ok"))
}

// Remember implements Memory, forwarding to the wrapped store.
func (m monitoredMemory) Remember(ctx context.Context, f Fact) error {
	err := m.inner.Remember(ctx, f)
	m.remembered.Add(ctx, 1, outcome(err))
	return err
}

// Recall implements Memory, forwarding to the wrapped store.
func (m monitoredMemory) Recall(ctx context.Context, limit int) ([]Fact, error) {
	facts, err := m.inner.Recall(ctx, limit)
	m.recalls.Add(ctx, 1, outcome(err))
	return facts, err
}
