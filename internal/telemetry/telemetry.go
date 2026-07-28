package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Start installs a MeterProvider selected by BIG_BRAIN_TELEMETRY: "stdout"
// prints metrics locally, "otlp" ships them to BIG_BRAIN_OTLP_ENDPOINT (gRPC),
// and anything else — including unset, the default — leaves the global no-op
// provider in place. Every Monitored wrapper in the codebase already degrades
// to that automatically, so "off" needs no code path here.
//
// The returned shutdown flushes and stops the provider; call it once,
// deferred, before the process exits. It is a no-op when telemetry is off.
func Start(ctx context.Context) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }
	switch os.Getenv("BIG_BRAIN_TELEMETRY") {
	case "stdout":
		exp, err := stdoutmetric.New()
		if err != nil {
			return nil, err
		}
		return install(exp), nil
	case "otlp":
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithInsecure()}
		if ep := os.Getenv("BIG_BRAIN_OTLP_ENDPOINT"); ep != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpoint(ep))
		}
		exp, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return nil, err
		}
		return install(exp), nil
	default:
		return noop, nil
	}
}

// install sets exp's reader as the global MeterProvider and returns its
// Shutdown, bound so callers never need to hold the provider itself.
func install(exp sdkmetric.Exporter) func(context.Context) error {
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)))
	otel.SetMeterProvider(provider)
	return provider.Shutdown
}
