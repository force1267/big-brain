package brain

import (
	"context"
	"errors"
	"testing"

	"github.com/force1267/big-brain/pkg/job"
	"github.com/force1267/big-brain/pkg/notify"
)

func TestGoEnqueuesDurableIntent(t *testing.T) {
	var got job.Job
	r := &Run{
		Enqueue: func(_ context.Context, j job.Job) error { got = j; return nil },
	}
	r.SetVar("guest", "John")
	node := Go("register-guest", func(r *Run) map[string]any {
		g, _ := Var[string](r, "guest")
		return map[string]any{"guest": g}
	})
	if err := node.Run(context.Background(), r); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got.ID == "" || got.Pipeline != "register-guest" || got.Payload["guest"] != "John" {
		t.Fatalf("job = %+v", got)
	}
}

func TestGoNilPayloadAndErrors(t *testing.T) {
	if err := Go("p", nil).Run(context.Background(), &Run{}); !errors.Is(err, ErrNoEnqueue) {
		t.Fatalf("err = %v; want ErrNoEnqueue", err)
	}
	boom := errors.New("boom")
	r := &Run{Enqueue: func(context.Context, job.Job) error { return boom }}
	if err := Go("p", nil).Run(context.Background(), r); !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}

func TestNotifyRendersAndSends(t *testing.T) {
	ch := &notify.Mock{}
	r := &Run{Notify: ch}
	r.SetVar("result", "Done — John is on the list.")
	if err := Notify(`{{index .Vars "result"}}`).Run(context.Background(), r); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(ch.Sent) != 1 || ch.Sent[0].Text != "Done — John is on the list." {
		t.Fatalf("sent %+v", ch.Sent)
	}
}

func TestNotifyErrors(t *testing.T) {
	if err := Notify("x").Run(context.Background(), &Run{}); !errors.Is(err, ErrNoNotify) {
		t.Fatalf("err = %v; want ErrNoNotify", err)
	}
	if err := Notify("{{.Broken").Run(context.Background(), &Run{Notify: &notify.Mock{}}); !errors.Is(err, ErrNotify) {
		t.Fatalf("err = %v; want ErrNotify", err)
	}
	boom := errors.New("boom")
	err := Notify("x").Run(context.Background(), &Run{Notify: &notify.Mock{Err: boom}})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}
