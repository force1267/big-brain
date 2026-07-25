package agent

import (
	"context"
	"testing"
)

// Request is the zero value outside a served request, and reads back what
// WithRequest set otherwise.
func TestTurnRequest(t *testing.T) {
	if r := (&Turn{ctx: context.Background()}).Request(); r.Model != "" || r.Temperature != nil {
		t.Fatalf("zero request expected, got %+v", r)
	}
	temp := 0.5
	ctx := WithRequest(context.Background(), Request{Model: "m", Temperature: &temp})
	r := (&Turn{ctx: ctx}).Request()
	if r.Model != "m" || r.Temperature == nil || *r.Temperature != 0.5 {
		t.Fatalf("request not carried: %+v", r)
	}
}
