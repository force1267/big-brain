package notify

import (
	"context"
	"testing"
)

func TestLogNeverFails(t *testing.T) {
	if err := Log().Notify(context.Background(), Message{Text: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestMockRecords(t *testing.T) {
	m := &Mock{}
	if err := m.Notify(context.Background(), Message{Text: "x"}); err != nil || len(m.Sent) != 1 {
		t.Fatalf("sent %+v, %v", m.Sent, err)
	}
}
