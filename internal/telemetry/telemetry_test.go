package telemetry

import (
	"context"
	"errors"
	"testing"

	"github.com/force1267/big-brain/internal/config"
)

func TestNew_DisabledReturnsNoop(t *testing.T) {
	var cfg config.Config // Telemetry.Enabled = false zero value
	p := New(cfg)
	if _, ok := p.(noop); !ok {
		t.Fatalf("expected noop, got %T", p)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("noop start errored: %v", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("noop shutdown errored: %v", err)
	}
}

func TestNew_EnabledReturnsOTLP(t *testing.T) {
	var cfg config.Config
	cfg.Telemetry.Enabled = true
	if _, ok := New(cfg).(*otlp); !ok {
		t.Fatal("expected otlp provider")
	}
}

func TestOTLP_ShutdownBeforeStartIsNil(t *testing.T) {
	if err := (&otlp{}).Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOTLP_StartAndShutdown(t *testing.T) {
	// The gRPC exporter connects lazily, so Start succeeds without a
	// collector; Shutdown then flushes and reports the connection failure
	// wrapped in ErrShutdown. Both branches get exercised.
	var cfg config.Config
	cfg.Telemetry.Enabled = true
	cfg.Telemetry.Endpoint = "localhost:1" // nothing listens here
	cfg.Telemetry.ServiceName = "test"

	o := &otlp{cfg: cfg}
	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := o.Shutdown(ctx); !errors.Is(err, ErrShutdown) {
		t.Fatalf("expected ErrShutdown, got: %v", err)
	}
}

func TestMockProvider(t *testing.T) {
	m := &MockProvider{StartErr: ErrStart}
	if err := m.Start(context.Background()); !errors.Is(err, ErrStart) || m.Started != 1 {
		t.Fatalf("mock start misbehaved: %v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil || m.Stopped != 1 {
		t.Fatalf("mock shutdown misbehaved: %v", err)
	}
}
