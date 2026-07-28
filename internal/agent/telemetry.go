package agent

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// toolExecBuckets bounds tool.exec.seconds: local Go, much faster than a
// network model call.
var toolExecBuckets = metric.WithExplicitBucketBoundaries(
	0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30)

// recordToolExec times one local tool handler invocation. tool is safe as an
// attribute here (unlike model.tool.calls' forwarded name): it comes from
// bb.OnCall in author code, not off the wire. The instrument is built fresh
// per call (mirrors pkg/model/telemetry.go's Monitored, which does the same
// at model construction) so it always targets whichever MeterProvider is
// current — a package-level instrument would freeze onto the provider
// installed at package load and never see a later otel.SetMeterProvider.
// Construction failure degrades to a no-op instead of breaking Resolve.
func recordToolExec(ctx context.Context, tool string, start time.Time) {
	h, err := otel.Meter("big-brain/agent").Float64Histogram("tool.exec.seconds", toolExecBuckets)
	if err != nil {
		return
	}
	h.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.String("tool", tool)))
}
