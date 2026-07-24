package bb

import (
	"errors"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

func TestNewModelNoTagsIsBlankBuilder(t *testing.T) {
	m := NewModel()
	if m.Name() != "" || m.Err() != nil {
		t.Fatalf("blank builder = name %q err %v", m.Name(), m.Err())
	}
	// still a builder: overridable.
	if got := m.WithName("x").Name(); got != "x" {
		t.Fatalf("override = %q", got)
	}
}

func TestNewModelSeededFromRegistryAndOverridable(t *testing.T) {
	model.ResetRegistry()
	t.Cleanup(model.ResetRegistry)

	RegisterModel(NewModel().WithName("gemma").WithTemprature(0.3), "cheap", "fast")

	// seeded from the registered model
	seeded := NewModel("cheap")
	if seeded.Name() != "gemma" || seeded.Err() != nil {
		t.Fatalf("seeded = name %q err %v", seeded.Name(), seeded.Err())
	}
	// override does not mutate the registered model
	overridden := NewModel("cheap").WithTemprature(0.9)
	if p := overridden.Params(); p.Temperature == nil || *p.Temperature != 0.9 {
		t.Fatalf("override temp = %+v", p)
	}
	if p := NewModel("cheap").Params(); *p.Temperature != 0.3 {
		t.Fatalf("registered model was mutated: temp %v", *p.Temperature)
	}
	// found by both tags
	if NewModel("fast", "cheap").Name() != "gemma" {
		t.Fatal("lookup by all tags failed")
	}
}

func TestNewModelUnknownTagRecordsError(t *testing.T) {
	model.ResetRegistry()
	t.Cleanup(model.ResetRegistry)

	m := NewModel("ghost")
	if !errors.Is(m.Err(), model.ErrUnknownModelTags) {
		t.Fatalf("unknown tag err = %v", m.Err())
	}
}

func TestMessageAndRoleFacade(t *testing.T) {
	if m := NewMessage("hi"); m.Role != "user" || m.Content != "hi" {
		t.Fatalf("NewMessage = %+v", m)
	}
	if r := Role("be nice"); r.Role != "system" || r.Content != "be nice" {
		t.Fatalf("Role = %+v", r)
	}
	if m := NewMessage("x").As("assistant"); m.Role != "assistant" {
		t.Fatalf("As = %+v", m)
	}
}
