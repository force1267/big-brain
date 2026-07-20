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
		c.Log.Format != "text" || c.Telemetry.Enabled || c.Telemetry.ServiceName != "big-brain" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("BIG_BRAIN_ENV", EnvProduction)
	t.Setenv("BIG_BRAIN_HTTP_ADDR", ":9999")
	t.Setenv("BIG_BRAIN_TELEMETRY_ENABLED", "true")

	c, err := New().Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Env != EnvProduction || c.HTTP.Addr != ":9999" || !c.Telemetry.Enabled {
		t.Fatalf("env overrides not applied: %+v", c)
	}
}

func TestLoad_InvalidEnv(t *testing.T) {
	t.Setenv("BIG_BRAIN_ENV", "staging")

	_, err := New().Load()
	if !errors.Is(err, ErrLoad) || !errors.Is(err, ErrInvalidEnv) {
		t.Fatalf("expected ErrLoad wrapping ErrInvalidEnv, got: %v", err)
	}
}

func TestLoad_Models(t *testing.T) {
	t.Setenv("BIG_BRAIN_MODELS", "fast=gpt-4o-mini, smart = gpt-4o")
	t.Setenv("BIG_BRAIN_UPSTREAM_BASE_URL", "http://up")
	t.Setenv("BIG_BRAIN_UPSTREAM_API_KEY", "k")

	c, err := New().Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Models["fast"] != "gpt-4o-mini" || c.Models["smart"] != "gpt-4o" {
		t.Fatalf("models = %+v", c.Models)
	}
	if c.Upstream.BaseURL != "http://up" || c.Upstream.APIKey != "k" {
		t.Fatalf("upstream = %+v", c.Upstream)
	}
}

func TestLoad_ModelsEmpty(t *testing.T) {
	c, err := New().Load()
	if err != nil || len(c.Models) != 0 {
		t.Fatalf("want empty models, got %+v, %v", c.Models, err)
	}
}

func TestLoad_ModelsInvalid(t *testing.T) {
	t.Setenv("BIG_BRAIN_MODELS", "fast")

	_, err := New().Load()
	if !errors.Is(err, ErrLoad) || !errors.Is(err, ErrInvalidModels) {
		t.Fatalf("expected ErrLoad wrapping ErrInvalidModels, got: %v", err)
	}
}

func TestLoad_SpeakersAndMemory(t *testing.T) {
	t.Setenv("BIG_BRAIN_SPEAKERS", "key-dad=dad,key-kid=kid")
	t.Setenv("BIG_BRAIN_MEMORY_PATH", "/data/m.jsonl")

	c, err := New().Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Speakers["key-dad"] != "dad" || c.Speakers["key-kid"] != "kid" {
		t.Fatalf("speakers = %+v", c.Speakers)
	}
	if c.Memory.Path != "/data/m.jsonl" {
		t.Fatalf("memory path = %q", c.Memory.Path)
	}
}

func TestLoad_SpeakersInvalid(t *testing.T) {
	t.Setenv("BIG_BRAIN_SPEAKERS", "just-a-key")

	_, err := New().Load()
	if !errors.Is(err, ErrLoad) || !errors.Is(err, ErrInvalidSpeakers) {
		t.Fatalf("expected ErrLoad wrapping ErrInvalidSpeakers, got: %v", err)
	}
}

func TestMockLoader(t *testing.T) {
	m := &MockLoader{Cfg: Config{Env: EnvLocal}, Err: nil}
	c, err := m.Load()
	if err != nil || c.Env != EnvLocal || m.Calls != 1 {
		t.Fatalf("mock misbehaved: %+v %v calls=%d", c, err, m.Calls)
	}
}
