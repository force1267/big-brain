package flow

import (
	"context"
	"errors"
	"testing"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// A sound flow validates clean.
func TestValidateOK(t *testing.T) {
	router := New().WithAgent(
		agent.New().WithModel(model.Bound(&model.Mock{})).Selects("talk"),
	)
	group := Select(New().WithId("talk").WithAgent(mockAgent("x")))
	if err := Validate(router.Next(group)); err != nil {
		t.Fatalf("expected clean, got %v", err)
	}
}

// A declared select with no matching group member fails at startup.
func TestValidateBadSelect(t *testing.T) {
	router := New().WithAgent(
		agent.New().WithModel(model.Bound(&model.Mock{})).Selects("ghost"),
	)
	group := Select(New().WithId("talk").WithAgent(mockAgent("x")))
	err := Validate(router.Next(group))
	if !errors.Is(err, ErrUnknownSelect) {
		t.Fatalf("want ErrUnknownSelect, got %v", err)
	}
}

// A default agent with no model fails validation.
func TestValidateDefaultAgentNoModel(t *testing.T) {
	f := New().WithId("x").WithAgent(agent.New()) // no model, no handler
	if err := Validate(f); err == nil {
		t.Fatal("expected error for modelless default agent")
	}
}

// A handler agent with no model is fine (it may never ask).
func TestValidateHandlerAgentNoModelOK(t *testing.T) {
	f := New().WithAgent(agent.New().OnMessage(func(context.Context, *agent.Turn) error { return nil }))
	if err := Validate(f); err != nil {
		t.Fatalf("handler agent without model should validate: %v", err)
	}
}

// A configured-but-unbuildable model (unknown registry tag) fails validation.
func TestValidateBadModel(t *testing.T) {
	model.ResetRegistry()
	t.Cleanup(model.ResetRegistry)
	bad := agent.New().WithModel(model.Resolve("ghost")) // records ErrUnknownModelTags
	if err := Validate(New().WithId("x").WithAgent(bad)); !errors.Is(err, model.ErrUnknownModelTags) {
		t.Fatalf("want ErrUnknownModelTags, got %v", err)
	}
}
