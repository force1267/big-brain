package job

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Monitored wraps a Store with OTel metrics: jobs enqueued and jobs run
// (by pipeline and outcome). No-op until telemetry is enabled (global
// meter provider), so constructors wrap unconditionally.
func Monitored(s Store) Store {
	meter := otel.Meter("big-brain/job")
	enqueued, err1 := meter.Int64Counter("job.enqueued")
	ran, err2 := meter.Int64Counter("job.ran")
	if err1 != nil || err2 != nil {
		return s
	}
	return monitoredStore{inner: s, enqueued: enqueued, ran: ran}
}

type monitoredStore struct {
	inner    Store
	enqueued metric.Int64Counter
	ran      metric.Int64Counter
}

var _ Store = monitoredStore{}

// Enqueue implements Store, forwarding to the wrapped store.
func (s monitoredStore) Enqueue(ctx context.Context, j Job) error {
	err := s.inner.Enqueue(ctx, j)
	out := "ok"
	if err != nil {
		out = "error"
	}
	s.enqueued.Add(ctx, 1, metric.WithAttributes(
		attribute.String("pipeline", j.Pipeline), attribute.String("outcome", out)))
	return err
}

// Sweep implements Store, counting each executed job by outcome.
func (s monitoredStore) Sweep(ctx context.Context, fn func(context.Context, Job) error) (time.Time, error) {
	return s.inner.Sweep(ctx, func(ctx context.Context, j Job) error {
		err := fn(ctx, j)
		out := "ok"
		if err != nil {
			out = "error"
		}
		s.ran.Add(ctx, 1, metric.WithAttributes(
			attribute.String("pipeline", j.Pipeline), attribute.String("outcome", out)))
		return err
	})
}
