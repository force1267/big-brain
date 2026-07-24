package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/force1267/big-brain/internal/anthropic"
	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/internal/openai"
	"github.com/force1267/big-brain/pkg/model"
	"github.com/google/uuid"
)

// Option configures a served brain.
type Option func(*config)

type config struct {
	addr    string
	name    string
	workers int
	tracer  flow.Tracer
	store   flow.Store
}

func defaults() config {
	return config{addr: ":8080", name: "brain", workers: 4}
}

// Addr sets the listen address (default ":8080").
func Addr(a string) Option { return func(c *config) { c.addr = a } }

// Name sets the model id reported to clients and /models (default "brain").
func Name(n string) Option { return func(c *config) { c.name = n } }

// Workers sets the number of concurrent request workers (reserved; requests are
// currently served per-connection by net/http). Kept for API stability.
func Workers(n int) Option { return func(c *config) { c.workers = n } }

// Trace installs a flow tracer; events are also kept in a diagnostics ring
// regardless, so /v1/diagnostics/trace always works.
func Trace(t flow.Tracer) Option { return func(c *config) { c.tracer = t } }

// Store enables durable flow checkpointing. A request carries a run id via the
// X-Run-Id header; on a crash, the client retries with the same id and the
// flows that already completed are skipped (resumed). Without a header a random
// id is used (correct, but no cross-request resume).
func Store(s flow.Store) Option { return func(c *config) { c.store = s } }

// server holds the running brain.
type server struct {
	flow   flow.Flow
	name   string
	tracer flow.Tracer
	ring   *ring
	store  flow.Store
}

// Handler validates the flow and returns its http.Handler for embedding. All
// wiring/config errors surface here (the single startup surface).
func Handler(f flow.Flow, opts ...Option) (http.Handler, error) {
	if err := flow.Validate(f); err != nil {
		return nil, err
	}
	c := defaults()
	for _, o := range opts {
		o(&c)
	}
	r := &ring{max: 500}
	s := &server{flow: f, name: c.name, tracer: tee(r, c.tracer), ring: r, store: c.store}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.openai)
	mux.HandleFunc("POST /v1/messages", s.anthropic)
	mux.HandleFunc("GET /v1/models", s.models)
	mux.HandleFunc("GET /v1/diagnostics/trace", s.diagnostics)
	return mux, nil
}

// Serve runs the brain on the configured address until ctx is cancelled, then
// shuts down gracefully.
func Serve(ctx context.Context, f flow.Flow, opts ...Option) error {
	h, err := Handler(f, opts...)
	if err != nil {
		return err
	}
	c := defaults()
	for _, o := range opts {
		o(&c)
	}
	srv := &http.Server{Addr: c.addr, Handler: h}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *server) run(ctx context.Context, runID string, msgs []model.Message) (string, error) {
	if s.store != nil {
		if runID == "" {
			runID = uuid.NewString()
		}
		ctx = flow.WithCheckpoint(ctx, s.store, runID)
	}
	out, err := flow.Run(ctx, s.flow, flow.State{Chat: msgs}, s.tracer)
	if err != nil {
		return "", err
	}
	return lastContent(out.Chat), nil
}

func (s *server) openai(w http.ResponseWriter, r *http.Request) {
	var req openai.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		openai.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	msgs := make([]model.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = model.Message{Role: m.Role, Content: m.Content}
	}
	reply, err := s.run(r.Context(), r.Header.Get("X-Run-Id"), msgs)
	if err != nil {
		openai.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := "chatcmpl-" + uuid.NewString()
	if req.Stream {
		writeStreamOpenAI(w, id, s.name, reply)
		return
	}
	openai.WriteResponse(w, id, s.name, reply)
}

func (s *server) anthropic(w http.ResponseWriter, r *http.Request) {
	var req anthropic.MessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		anthropic.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var msgs []model.Message
	if s := string(req.System); s != "" {
		msgs = append(msgs, model.Message{Role: "system", Content: s})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, model.Message{Role: m.Role, Content: string(m.Content)})
	}
	reply, err := s.run(r.Context(), r.Header.Get("X-Run-Id"), msgs)
	if err != nil {
		anthropic.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := "msg_" + uuid.NewString()
	if req.Stream {
		writeStreamAnthropic(w, id, s.name, reply)
		return
	}
	anthropic.WriteResponse(w, id, s.name, reply)
}

func (s *server) models(w http.ResponseWriter, _ *http.Request) {
	openai.WriteModels(w, s.name)
}

func (s *server) diagnostics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.ring.snapshot())
}

// writeStreamOpenAI emits the (buffered) reply as one SSE delta then DONE. True
// token streaming through flows arrives with the streaming-chat work later.
func writeStreamOpenAI(w http.ResponseWriter, id, name, reply string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl := http.NewResponseController(w)
	openai.WriteChunk(w, id, name, reply)
	fl.Flush()
	openai.WriteDone(w, id, name)
	fl.Flush()
}

func writeStreamAnthropic(w http.ResponseWriter, id, name, reply string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl := http.NewResponseController(w)
	anthropic.WriteStart(w, id, name)
	anthropic.WriteDelta(w, reply)
	fl.Flush()
	anthropic.WriteStop(w)
	fl.Flush()
}

func lastContent(chat []model.Message) string {
	if len(chat) == 0 {
		return ""
	}
	return chat[len(chat)-1].Content
}

// ring is a bounded buffer of recent trace events for the diagnostics endpoint.
type ring struct {
	mu  sync.Mutex
	buf []flow.Event
	max int
}

func (r *ring) Event(_ context.Context, e flow.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, e)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
}

func (r *ring) snapshot() []flow.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]flow.Event(nil), r.buf...)
}

// tee sends events to the ring and, if set, an author-supplied tracer.
func tee(r *ring, user flow.Tracer) flow.Tracer {
	if user == nil {
		return r
	}
	return teeTracer{r, user}
}

type teeTracer struct{ a, b flow.Tracer }

func (t teeTracer) Event(ctx context.Context, e flow.Event) {
	t.a.Event(ctx, e)
	t.b.Event(ctx, e)
}
