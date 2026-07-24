package agent

// Reply is the result of a Turn's Ask: the model's completed answer. Text is
// available whole (ReadAll), incrementally (Read/Stream), and — via the free
// function bb.Extract[T] — decoded into a schema type. Media accessors are
// present for the multimodal future; today they return empty.
//
// This is a buffered reply: Ask drains the model stream and validates it before
// returning, so the whole text is already here. Read/Stream replay that buffer.
type Reply struct {
	content string
	read    bool
}

// ReadAll returns the full reply text.
func (r Reply) ReadAll() string { return r.content }

// Read returns the not-yet-read remainder of the reply, then nothing. For a
// buffered reply that is the whole text on the first call, "" after.
func (r *Reply) Read() string {
	if r.read {
		return ""
	}
	r.read = true
	return r.content
}

// Stream returns a channel that yields the reply text and closes. It exists so
// callers can treat buffered and (future) live replies the same way.
func (r Reply) Stream() <-chan string {
	ch := make(chan string, 1)
	if r.content != "" {
		ch <- r.content
	}
	close(ch)
	return ch
}

// Media returns the bytes of a named media attachment, or nil. (Multimodal is
// not wired yet; always nil today.)
func (r Reply) Media(string) []byte { return nil }

// ListMedia returns the names of media attachments, or nil.
func (r Reply) ListMedia() []string { return nil }
