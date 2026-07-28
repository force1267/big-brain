package serve

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// latencyBuckets matches pkg/model/telemetry.go's model.call.seconds buckets:
// an LLM request runs 50ms..2min, not the OTel RPC default's millisecond scale.
var latencyBuckets = metric.WithExplicitBucketBoundaries(
	0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120)

// ttftBuckets matches pkg/model/telemetry.go's model.ttft.seconds buckets:
// denser at the low end, where a perceptible regression shows up.
var ttftBuckets = metric.WithExplicitBucketBoundaries(
	0.025, 0.05, 0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8, 30)

// recordRequestSeconds records request.seconds — the SLO metric — with the
// resolved served flow name (never the client-supplied req.Model, which is
// unbounded), the outcome, and whether the client streamed. Built fresh per
// call, same as pkg/model/telemetry.go's Monitored and internal/agent's
// recordToolExec, so it always targets whichever MeterProvider is current
// and a construction failure degrades to a no-op instead of breaking the
// request. Recorded inline in the openai/anthropic handlers rather than as
// an http.Handler wrapper (the documented MonitoredX shape) because only
// here are sink.Claimed() and the resolved flow name available.
func recordRequestSeconds(ctx context.Context, flowName, outcome string, streaming bool, dur time.Duration) {
	h, err := otel.Meter("big-brain/serve").Float64Histogram("request.seconds", latencyBuckets)
	if err != nil {
		return
	}
	h.Record(ctx, dur.Seconds(), metric.WithAttributes(
		attribute.String("flow", flowName),
		attribute.String("outcome", outcome),
		attribute.Bool("streaming", streaming),
	))
}

// recordRequestTTFT records request.ttft.seconds — time from handler entry to
// the first byte written to the client's SSE sink. Streaming only; a
// non-streaming request or one whose terminal agent never claimed the sink
// records nothing (see docs/design-metrics.md's streaming-vs-non-streaming
// section) — callers gate this on the sink's first Write, which covers both.
func recordRequestTTFT(ctx context.Context, flowName string, dur time.Duration) {
	h, err := otel.Meter("big-brain/serve").Float64Histogram("request.ttft.seconds", ttftBuckets)
	if err != nil {
		return
	}
	h.Record(ctx, dur.Seconds(), metric.WithAttributes(attribute.String("flow", flowName)))
}
