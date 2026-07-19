package job

import (
	"context"
	"errors"
	"testing"
)

func TestMonitoredDelegates(t *testing.T) {
	mock := &Mock{}
	s := Monitored(mock)
	if err := s.Enqueue(context.Background(), Job{ID: "a", Pipeline: "p"}); err != nil {
		t.Fatal(err)
	}
	var ran []string
	_, err := s.Sweep(context.Background(), func(_ context.Context, j Job) error {
		ran = append(ran, j.ID)
		return errors.New("boom") // outcome counting must not change semantics
	})
	if err != nil || len(ran) != 1 || ran[0] != "a" {
		t.Fatalf("delegation broken: %v, %v", ran, err)
	}
}

func TestMonitoredPropagatesEnqueueError(t *testing.T) {
	boom := errors.New("boom")
	s := Monitored(&Mock{EnqueueErr: boom})
	if err := s.Enqueue(context.Background(), Job{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
