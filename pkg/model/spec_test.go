package model

import (
	"errors"
	"strings"
	"testing"
)

// With* set fields and return independent copies (value semantics).
func TestSpecBuildersAndImmutability(t *testing.T) {
	base := Spec{}.WithName("m").WithThink(true).WithTemprature(0.5)
	if base.Name() != "m" {
		t.Fatalf("Name = %q", base.Name())
	}
	if on, set := base.Think(); !on || !set {
		t.Fatalf("Think = %v,%v", on, set)
	}

	// Overriding a copy must not touch the original.
	derived := base.WithTemprature(0.9).WithName("n")
	if base.Name() != "m" {
		t.Errorf("original mutated by derived: Name = %q", base.Name())
	}
	if derived.Name() != "n" {
		t.Errorf("derived Name = %q", derived.Name())
	}
	if p := base.Params(); p.Temperature == nil || *p.Temperature != 0.5 {
		t.Errorf("base temp not preserved: %+v", p)
	}
	if p := base.Params(); p.Think == nil || !*p.Think {
		t.Errorf("base think not preserved: %+v", p)
	}
	if p := derived.Params(); *p.Temperature != 0.9 {
		t.Errorf("derived temp = %v", *p.Temperature)
	}
}

// Params leaves unset fields nil.
func TestSpecParamsUnset(t *testing.T) {
	p := (Spec{}).Params()
	if p.Temperature != nil {
		t.Fatalf("unset temperature should be nil, got %v", *p.Temperature)
	}
	if p.Think != nil {
		t.Fatalf("unset think should be nil, got %v", *p.Think)
	}
}

// Build: happy path, missing name, and a recorded error each take their branch.
func TestSpecBuild(t *testing.T) {
	m, err := Spec{}.WithName("gpt-4o-mini").Build()
	if err != nil || m == nil {
		t.Fatalf("build with name: model=%v err=%v", m, err)
	}

	_, err = Spec{}.Build()
	if !errors.Is(err, ErrNoModelName) {
		t.Fatalf("build without name: want ErrNoModelName, got %v", err)
	}

	sentinel := errors.New("recorded")
	_, err = Spec{}.withErr(sentinel).WithName("x").Build()
	if !errors.Is(err, sentinel) {
		t.Fatalf("recorded error should win over name check, got %v", err)
	}
}

// Every sentinel in this package follows the same "model: ..." prefix
// (ErrNoModelName, ErrUnknownModelTags, ErrToolInput, ErrToolSchema,
// ErrToolArgs) so grepping/reading a bare error string is unambiguous about
// which package it came from. ErrUpstream was the one outlier.
func TestSentinelMessagesSharePackagePrefix(t *testing.T) {
	for _, err := range []error{ErrNoModelName, ErrUnknownModelTags, ErrToolInput, ErrToolSchema, ErrToolArgs, ErrUpstream} {
		if !strings.HasPrefix(err.Error(), "model: ") {
			t.Errorf("%q does not start with the package's %q prefix", err.Error(), "model: ")
		}
	}
}

func TestNewMessageAndAs(t *testing.T) {
	m := NewMessage("hi")
	if m.Role != "user" || m.Content != "hi" {
		t.Fatalf("NewMessage = %+v", m)
	}
	sys := m.As("system")
	if sys.Role != "system" {
		t.Fatalf("As role = %q", sys.Role)
	}
	if m.Role != "user" {
		t.Errorf("As mutated original: %q", m.Role)
	}
}
