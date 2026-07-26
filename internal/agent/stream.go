package agent

import (
	"strings"
	"sync"

	"github.com/force1267/big-brain/pkg/model"
)

// streamBuf is a record-and-replay buffer behind a live Reply: one producer
// (Ask's pump) appends chunks; any number of consumers read the whole history
// plus the live tail. It is what lets reply.Stream() (live) and reply.ReadAll()/
// Extract (whole) coexist on one reply — a raw channel is single-consumer, so
// the buffer, not the channel, is the source of truth. Consumers never block the
// producer: the pump always fills the buffer regardless of who is reading.
type streamBuf struct {
	mu      sync.Mutex
	chunks  []string
	calls   []model.ToolCall // tool calls the model asked for, collected whole
	err     error
	closed  bool
	waiters []chan struct{} // woken on every append and on close
}

func newStreamBuf() *streamBuf { return &streamBuf{} }

// pump drains a model stream into the buffer until it ends, recording a terminal
// error if the stream failed. Run it in its own goroutine.
func (b *streamBuf) pump(stream <-chan model.Chunk) {
	for c := range stream {
		if c.Err != nil {
			b.close(c.Err)
			return
		}
		if c.Call != nil {
			b.pushCall(*c.Call)
			continue
		}
		b.push(c.Content)
	}
	b.close(nil)
}

func (b *streamBuf) push(c string) {
	b.mu.Lock()
	b.chunks = append(b.chunks, c)
	b.wake()
	b.mu.Unlock()
}

func (b *streamBuf) pushCall(c model.ToolCall) {
	b.mu.Lock()
	b.calls = append(b.calls, c)
	b.wake()
	b.mu.Unlock()
}

// toolCalls blocks until the reply is complete, then returns the calls. Tool
// arguments are not streamed in v1, so waiting is what makes a call whole.
func (b *streamBuf) toolCalls() []model.ToolCall {
	b.mu.Lock()
	for !b.closed {
		w := b.wait()
		b.mu.Unlock()
		<-w
		b.mu.Lock()
	}
	calls := append([]model.ToolCall(nil), b.calls...)
	b.mu.Unlock()
	return calls
}

func (b *streamBuf) close(err error) {
	b.mu.Lock()
	if !b.closed {
		b.err, b.closed = err, true
	}
	b.wake()
	b.mu.Unlock()
}

// wake releases everyone waiting for progress; callers hold b.mu.
func (b *streamBuf) wake() {
	for _, w := range b.waiters {
		close(w)
	}
	b.waiters = nil
}

// wait registers for the next progress signal; caller holds b.mu, and the
// returned channel must be awaited with b.mu released.
func (b *streamBuf) wait() chan struct{} {
	w := make(chan struct{})
	b.waiters = append(b.waiters, w)
	return w
}

// stream returns a channel yielding the whole reply from the start (history plus
// live tail) and closing when the reply is complete.
func (b *streamBuf) stream() <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for i := 0; ; {
			b.mu.Lock()
			for i >= len(b.chunks) && !b.closed {
				w := b.wait()
				b.mu.Unlock()
				<-w
				b.mu.Lock()
			}
			if i < len(b.chunks) {
				c := b.chunks[i]
				i++
				b.mu.Unlock()
				out <- c
				continue
			}
			b.mu.Unlock()
			return // closed and fully drained
		}
	}()
	return out
}

// readAll blocks until the reply is complete, then returns the whole text and
// the terminal error (if the stream ended badly).
func (b *streamBuf) readAll() (string, error) {
	b.mu.Lock()
	for !b.closed {
		w := b.wait()
		b.mu.Unlock()
		<-w
		b.mu.Lock()
	}
	text := strings.Join(b.chunks, "")
	err := b.err
	b.mu.Unlock()
	return text, err
}
