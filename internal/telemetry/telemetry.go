package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	otlpmetricgrpc "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/force1267/big-brain/internal/config"
)

var (
	// ErrStart wraps failures while bringing telemetry up.
	ErrStart = errors.New("telemetry start failed")
	// ErrShutdown wraps failures while tearing telemetry down.
	ErrShutdown = errors.New("telemetry shutdown failed")
)

// Provider brings metrics up and tears them down gracefully.
type Provider interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// New selects the Provider for the environment: noop when telemetry is
// disabled, OTLP gRPC otherwise.
func New(cfg config.Config) Provider {
	if !cfg.Telemetry.Enabled {
		return noop{}
	}
	return &otlp{cfg: cfg}
}

type noop struct{}

var _ Provider = noop{}

func (noop) Start(context.Context) error    { return nil }
func (noop) Shutdown(context.Context) error { return nil }

type otlp struct {
	cfg      config.Config
	provider *sdkmetric.MeterProvider
}

var _ Provider = (*otlp)(nil)

func (o *otlp) Start(ctx context.Context) error {
	exp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(o.cfg.Telemetry.Endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("%w: creating otlp exporter: %w", ErrStart, err)
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(o.cfg.Telemetry.ServiceName),
	))
	if err != nil {
		return fmt.Errorf("%w: building resource: %w", ErrStart, err)
	}

	o.provider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
	)
	otel.SetMeterProvider(o.provider)
	return nil
}

func (o *otlp) Shutdown(ctx context.Context) error {
	if o.provider == nil {
		return nil
	}
	if err := o.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrShutdown, err)
	}
	return nil
}
