package notify

import (
	"context"
	"errors"
	"testing"
)

func TestMonitoredDelegates(t *testing.T) {
	mock := &Mock{}
	c := Monitored(mock)
	if err := c.Notify(context.Background(), Message{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(mock.Sent) != 1 || mock.Sent[0].Text != "x" {
		t.Fatalf("delegation broken: %+v", mock.Sent)
	}
}

func TestMonitoredPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	if err := Monitored(&Mock{Err: boom}).Notify(context.Background(), Message{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
