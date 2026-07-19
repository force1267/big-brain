package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/config"
	"github.com/force1267/big-brain/internal/logging"
	"github.com/force1267/big-brain/internal/telemetry"
)

func testRunner(loader config.Loader, logs logging.Initializer, tel telemetry.Provider) runner {
	return runner{
		loader:      loader,
		logs:        logs,
		telemetryOf: func(config.Config) telemetry.Provider { return tel },
	}
}

func validCfg() config.Config {
	var c config.Config
	c.Env = config.EnvLocal
	c.Log.Level = "info"
	return c
}

func TestRun_HappyPathUntilCancel(t *testing.T) {
	tel := &telemetry.MockProvider{}
	r := testRunner(&config.MockLoader{Cfg: validCfg()}, &logging.MockInitializer{}, tel)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tel.Started != 1 || tel.Stopped != 1 {
		t.Fatalf("telemetry lifecycle wrong: started=%d stopped=%d", tel.Started, tel.Stopped)
	}
}

func TestRun_ConfigError(t *testing.T) {
	r := testRunner(&config.MockLoader{Err: config.ErrLoad}, &logging.MockInitializer{}, &telemetry.MockProvider{})
	err := r.Run(context.Background())
	if !errors.Is(err, ErrConfig) || !errors.Is(err, config.ErrLoad) {
		t.Fatalf("expected ErrConfig wrapping config.ErrLoad, got: %v", err)
	}
}

func TestRun_LoggingError(t *testing.T) {
	r := testRunner(&config.MockLoader{Cfg: validCfg()}, &logging.MockInitializer{Err: logging.ErrBadLevel}, &telemetry.MockProvider{})
	err := r.Run(context.Background())
	if !errors.Is(err, ErrStartup) || !errors.Is(err, logging.ErrBadLevel) {
		t.Fatalf("expected ErrStartup wrapping logging.ErrBadLevel, got: %v", err)
	}
}

func TestRun_TelemetryStartError(t *testing.T) {
	tel := &telemetry.MockProvider{StartErr: telemetry.ErrStart}
	r := testRunner(&config.MockLoader{Cfg: validCfg()}, &logging.MockInitializer{}, tel)
	err := r.Run(context.Background())
	if !errors.Is(err, ErrStartup) || !errors.Is(err, telemetry.ErrStart) {
		t.Fatalf("expected ErrStartup wrapping telemetry.ErrStart, got: %v", err)
	}
}

func TestRun_TelemetryShutdownErrorIsLoggedNotReturned(t *testing.T) {
	tel := &telemetry.MockProvider{ShutdownErr: telemetry.ErrShutdown}
	r := testRunner(&config.MockLoader{Cfg: validCfg()}, &logging.MockInitializer{}, tel)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatalf("shutdown error must not fail Run: %v", err)
	}
	if tel.Stopped != 1 {
		t.Fatal("shutdown was not attempted")
	}
}

func TestNew_WiresRealImplementations(t *testing.T) {
	if _, ok := New().(runner); !ok {
		t.Fatal("New should return the wired runner")
	}
}

func TestMockRunner(t *testing.T) {
	m := &MockRunner{Err: ErrStartup}
	if err := m.Run(context.Background()); !errors.Is(err, ErrStartup) || m.Calls != 1 {
		t.Fatalf("mock misbehaved: %v", err)
	}
}
