package agent

// Reply is the result of a Turn's Ask: the model's answer. Text is available
// whole (ReadAll), incrementally (Read/Stream), and — via the free function
// bb.Extract[T] — decoded into a schema type. Media accessors are present for
// the multimodal future; today they return empty.
//
// A Reply is either buffered (content set — the schema path, a FixedModel, or a
// non-streaming ask) or live (buf set — Ask returned before the model finished,
// a goroutine pumping tokens in). Both back the same methods: ReadAll/Read block
// for the whole text, Stream yields it from the start, Err reports a terminal
// stream error. The methods keep value semantics; the shared *streamBuf makes a
// copy of a live Reply observe the same stream.
type Reply struct {
	content string
	buf     *streamBuf
	read    bool
}

// ReadAll returns the full reply text (blocking until a live reply completes).
func (r Reply) ReadAll() string {
	if r.buf != nil {
		s, _ := r.buf.readAll()
		return s
	}
	return r.content
}

// Read returns the not-yet-read remainder of the reply, then nothing. For a
// buffered reply that is the whole text on the first call, "" after. For a live
// reply it blocks for completion and returns the whole text once (Stream is the
// incremental reader).
func (r *Reply) Read() string {
	if r.read {
		return ""
	}
	r.read = true
	return r.ReadAll()
}

// Stream returns a channel that yields the reply text from the start and closes
// when it is complete. For a live reply it is token-by-token; for a buffered one
// it yields the whole text once. Callers can Stream and ReadAll the same reply.
func (r Reply) Stream() <-chan string {
	if r.buf != nil {
		return r.buf.stream()
	}
	ch := make(chan string, 1)
	if r.content != "" {
		ch <- r.content
	}
	close(ch)
	return ch
}

// Err reports a terminal error from a live reply's stream (nil for a buffered
// reply, or once a live reply completed cleanly). It blocks until the reply is
// complete, so check it after draining Stream/ReadAll.
func (r Reply) Err() error {
	if r.buf != nil {
		_, err := r.buf.readAll()
		return err
	}
	return nil
}

// Media returns the bytes of a named media attachment, or nil. (Multimodal is
// not wired yet; always nil today.)
func (r Reply) Media(string) []byte { return nil }

// ListMedia returns the names of media attachments, or nil.
func (r Reply) ListMedia() []string { return nil }
