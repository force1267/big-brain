package notify

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Monitored wraps a Channel with OTel metrics: notifications sent, by
// outcome. No-op until telemetry is enabled (global meter provider), so
// constructors wrap unconditionally.
func Monitored(c Channel) Channel {
	meter := otel.Meter("big-brain/notify")
	sent, err := meter.Int64Counter("notify.sent")
	if err != nil {
		return c
	}
	return monitoredChannel{inner: c, sent: sent}
}

type monitoredChannel struct {
	inner Channel
	sent  metric.Int64Counter
}

var _ Channel = monitoredChannel{}

// Notify implements Channel, forwarding to the wrapped channel.
func (c monitoredChannel) Notify(ctx context.Context, m Message) error {
	err := c.inner.Notify(ctx, m)
	out := "ok"
	if err != nil {
		out = "error"
	}
	c.sent.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", out)))
	return err
}
