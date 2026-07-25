package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

// A live reply lets Stream (incremental) and ReadAll (whole) coexist, and a
// second subscriber gets the whole history even if it subscribes late.
func TestStreamBufCoexistAndReplay(t *testing.T) {
	b := newStreamBuf()
	ch := make(chan model.Chunk)
	go b.pump(ch)

	r := Reply{buf: b}
	got := []string{}
	done := make(chan struct{})
	go func() {
		for tok := range r.Stream() {
			got = append(got, tok)
		}
		close(done)
	}()

	ch <- model.Chunk{Content: "hel"}
	ch <- model.Chunk{Content: "lo"}
	close(ch)
	<-done

	if len(got) != 2 || got[0] != "hel" || got[1] != "lo" {
		t.Fatalf("streamed chunks = %v", got)
	}
	// ReadAll works alongside Stream, and a late Stream replays from the start.
	if r.ReadAll() != "hello" {
		t.Fatalf("ReadAll = %q", r.ReadAll())
	}
	var late []string
	for tok := range r.Stream() {
		late = append(late, tok)
	}
	if len(late) != 2 {
		t.Fatalf("late subscriber missed history: %v", late)
	}
	if r.Err() != nil {
		t.Fatalf("Err = %v", r.Err())
	}
}

// A terminal stream error surfaces via reply.Err(), not the (already-returned)
// Ask, and the partial content before it is still readable.
func TestStreamBufError(t *testing.T) {
	b := newStreamBuf()
	ch := make(chan model.Chunk, 2)
	ch <- model.Chunk{Content: "par"}
	ch <- model.Chunk{Err: errors.New("boom")}
	close(ch)
	b.pump(ch)

	r := Reply{buf: b}
	if r.ReadAll() != "par" {
		t.Fatalf("partial = %q", r.ReadAll())
	}
	if r.Err() == nil || r.Err().Error() != "boom" {
		t.Fatalf("Err = %v", r.Err())
	}
}

// Turn.Stream is claim-once: the first caller wins, the second gets ok=false;
// what is written is captured as one reply message. Without a sink, ok=false.
func TestTurnStreamClaimOnce(t *testing.T) {
	if _, ok := (&Turn{ctx: context.Background()}).Stream(); ok {
		t.Fatal("no sink should mean ok=false")
	}

	var mu sync.Mutex
	var sent []string
	sink := &Sink{Write: func(_ context.Context, c string) error {
		mu.Lock()
		sent = append(sent, c)
		mu.Unlock()
		return nil
	}}
	ctx := WithSink(context.Background(), sink)
	turn := &Turn{ctx: ctx}

	out, ok := turn.Stream()
	if !ok {
		t.Fatal("first claim should win")
	}
	if _, ok2 := turn.Stream(); ok2 {
		t.Fatal("second claim should lose")
	}
	out <- "a"
	out <- "b"
	close(out)

	rs := turn.Replies() // waits for the stream goroutine
	if len(rs) != 1 || rs[0].Content != "ab" || rs[0].Role != "assistant" {
		t.Fatalf("captured reply = %+v", rs)
	}
	if len(sent) != 2 {
		t.Fatalf("client got %v", sent)
	}
}

// A Group member (shared turn) never streams — ok=false so it Reply-s normally.
func TestTurnStreamSharedFalse(t *testing.T) {
	sink := &Sink{Write: func(context.Context, string) error { return nil }}
	turn := &Turn{ctx: WithSink(context.Background(), sink), shared: NewSharedChat(nil)}
	if _, ok := turn.Stream(); ok {
		t.Fatal("shared turn should not stream")
	}
}
