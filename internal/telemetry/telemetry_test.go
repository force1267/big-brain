package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Unset (or any other value) leaves the global MeterProvider untouched — every
// Monitored wrapper in the codebase already degrades to a no-op against it, so
// "off" needs no installation of its own.
func TestStartNoopByDefault(t *testing.T) {
	before := noop.NewMeterProvider()
	otel.SetMeterProvider(before)
	t.Cleanup(func() { otel.SetMeterProvider(noop.NewMeterProvider()) })

	shutdown, err := Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := otel.GetMeterProvider(); got != before {
		t.Fatalf("noop mode replaced the global MeterProvider: %T", got)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("noop shutdown: %v", err)
	}
}

// BIG_BRAIN_TELEMETRY=stdout installs a real SDK MeterProvider.
func TestStartStdoutInstallsProvider(t *testing.T) {
	t.Setenv("BIG_BRAIN_TELEMETRY", "stdout")
	t.Cleanup(func() { otel.SetMeterProvider(noop.NewMeterProvider()) })

	shutdown, err := Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, ok := otel.GetMeterProvider().(*sdkmetric.MeterProvider); !ok {
		t.Fatalf("stdout mode did not install an SDK MeterProvider: %T", otel.GetMeterProvider())
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("stdout shutdown: %v", err)
	}
}

// BIG_BRAIN_TELEMETRY=otlp builds and installs a provider without making any
// live network call — otlpmetricgrpc.New dials lazily (construction never
// touches the network), so a fake endpoint is enough to prove the wiring, not
// a real collector. Shutdown is given a short deadline and its error ignored:
// a real flush attempt against 127.0.0.1:0 would otherwise block for the
// exporter's full 10s default export timeout trying to actually dial out.
func TestStartOTLPInstallsProviderNoNetworkCall(t *testing.T) {
	t.Setenv("BIG_BRAIN_TELEMETRY", "otlp")
	t.Setenv("BIG_BRAIN_OTLP_ENDPOINT", "127.0.0.1:0")
	t.Cleanup(func() { otel.SetMeterProvider(noop.NewMeterProvider()) })

	shutdown, err := Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, ok := otel.GetMeterProvider().(*sdkmetric.MeterProvider); !ok {
		t.Fatalf("otlp mode did not install an SDK MeterProvider: %T", otel.GetMeterProvider())
	}
	sctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = shutdown(sctx) // no assertion: a fake endpoint legitimately fails to flush
}
