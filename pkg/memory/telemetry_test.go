package memory

import (
	"context"
	"errors"
	"testing"
)

func TestMonitoredDelegates(t *testing.T) {
	mock := &Mock{}
	m := Monitored(mock)
	if err := m.Remember(context.Background(), Fact{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	facts, err := m.Recall(context.Background(), 10)
	if err != nil || len(facts) != 1 || facts[0].Content != "x" {
		t.Fatalf("delegation broken: %+v, %v", facts, err)
	}
}

func TestMonitoredPropagatesErrors(t *testing.T) {
	boom := errors.New("boom")
	m := Monitored(&Mock{RememberErr: boom, RecallErr: boom})
	if err := m.Remember(context.Background(), Fact{}); !errors.Is(err, boom) {
		t.Fatalf("remember err = %v", err)
	}
	if _, err := m.Recall(context.Background(), 1); !errors.Is(err, boom) {
		t.Fatalf("recall err = %v", err)
	}
}
