package logging

import (
	"errors"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/force1267/big-brain/internal/config"
)

func cfgWith(level, format string) config.Config {
	var c config.Config
	c.Log.Level = level
	c.Log.Format = format
	return c
}

func TestInit_TextLevel(t *testing.T) {
	if err := New().Init(cfgWith("debug", "text")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logrus.GetLevel() != logrus.DebugLevel {
		t.Fatalf("level not applied: %v", logrus.GetLevel())
	}
	if _, ok := logrus.StandardLogger().Formatter.(*logrus.TextFormatter); !ok {
		t.Fatal("expected text formatter")
	}
}

func TestInit_JSONFormat(t *testing.T) {
	if err := New().Init(cfgWith("info", "json")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := logrus.StandardLogger().Formatter.(*logrus.JSONFormatter); !ok {
		t.Fatal("expected json formatter")
	}
}

func TestInit_BadLevel(t *testing.T) {
	err := New().Init(cfgWith("loud", "text"))
	if !errors.Is(err, ErrInit) || !errors.Is(err, ErrBadLevel) {
		t.Fatalf("expected ErrInit wrapping ErrBadLevel, got: %v", err)
	}
}

func TestMockInitializer(t *testing.T) {
	m := &MockInitializer{Err: ErrBadLevel}
	if err := m.Init(cfgWith("x", "y")); !errors.Is(err, ErrBadLevel) || m.Calls != 1 || m.Got.Log.Level != "x" {
		t.Fatalf("mock misbehaved: %v %+v", err, m)
	}
}
