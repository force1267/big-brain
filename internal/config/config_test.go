package config

import (
	"errors"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	c, err := New().Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Env != EnvLocal || c.HTTP.Addr != ":8080" || c.Log.Level != "info" ||
		c.Log.Format != "text" || c.Telemetry.Enabled || c.Telemetry.ServiceName != "wrapper" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("WRAPPER_ENV", EnvProduction)
	t.Setenv("WRAPPER_HTTP_ADDR", ":9999")
	t.Setenv("WRAPPER_TELEMETRY_ENABLED", "true")

	c, err := New().Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Env != EnvProduction || c.HTTP.Addr != ":9999" || !c.Telemetry.Enabled {
		t.Fatalf("env overrides not applied: %+v", c)
	}
}

func TestLoad_InvalidEnv(t *testing.T) {
	t.Setenv("WRAPPER_ENV", "staging")

	_, err := New().Load()
	if !errors.Is(err, ErrLoad) || !errors.Is(err, ErrInvalidEnv) {
		t.Fatalf("expected ErrLoad wrapping ErrInvalidEnv, got: %v", err)
	}
}

func TestMockLoader(t *testing.T) {
	m := &MockLoader{Cfg: Config{Env: EnvLocal}, Err: nil}
	c, err := m.Load()
	if err != nil || c.Env != EnvLocal || m.Calls != 1 {
		t.Fatalf("mock misbehaved: %+v %v calls=%d", c, err, m.Calls)
	}
}
