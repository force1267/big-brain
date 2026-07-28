package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/internal/anthropic"
	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/internal/openai"
	"github.com/force1267/big-brain/internal/telemetry"
	"github.com/force1267/big-brain/pkg/engine"
	"github.com/force1267/big-brain/pkg/model"
	"github.com/google/uuid"
)

// ErrNoFlow is returned by Serve/Handler when neither an explicit default flow
// nor any registered flow is available to serve.
var ErrNoFlow = errors.New("serve: no flow to serve")

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
	return config{addr: ":8080", name: "brain", workers: 4, store: engine.NewMemStore()}
}

// Addr sets the listen address (default ":8080").
func Addr(a string) Option { return func(c *config) { c.addr = a } }

// Name sets the model id reported to clients and /models (default "brain").
func Name(n string) Option { return func(c *config) { c.name = n } }

// Workers sets how many triggered/scheduled flow bodies (Trigger/Every/Once)
// run concurrently in the durable job worker (pkg/engine). It does not affect
// HTTP request concurrency — net/http already serves those per-connection.
func Workers(n int) Option { return func(c *config) { c.workers = n } }

// Trace installs a flow tracer; events are also kept in a diagnostics ring
// regardless, so /v1/diagnostics/trace always works.
func Trace(t flow.Tracer) Option { return func(c *config) { c.tracer = t } }

// Store sets the durability backend flows checkpoint to and triggers schedule
// against. A request carries a run id via the X-Run-Id header; on a crash, the
// client retries with the same id and the flows that already completed are
// skipped (resumed). Without a header a random id is used (correct, but no
// cross-request resume). Without Store, an in-memory backend is used (see
// defaults()) — triggers/checkpoints work, but nothing survives a process
// restart; pass engine.NewFileStore or another persistent Store for that.
func Store(s flow.Store) Option { return func(c *config) { c.store = s } }

// server holds the running brain: a default flow (used when a request names no,
// or an unknown, model) and any named flows keyed by model id.
type server struct {
	named  map[string]flow.Flow
	def    flow.Flow
	name   string // reported id of the default flow
	tracer flow.Tracer
	ring   *ring
	store  flow.Store
	sched  *engineScheduler // nil when no store: triggers/initiative disabled
	hooks  *webhookRegistry
}

// Handler validates the flow and returns its http.Handler for embedding. All
// wiring/config errors surface here (the single startup surface). The flow f is
// the explicit default (may be nil to serve only registered flows). Triggers are
// scheduled, but their worker only runs under Serve (an embedder gets the routes,
// not the background loop).
func Handler(f flow.Flow, opts ...Option) (http.Handler, error) {
	_, h, err := build(f, opts...)
	return h, err
}

// build assembles the server and its mux, returning both so Serve can run the
// scheduler worker while Handler exposes only the routes.
func build(f flow.Flow, opts ...Option) (*server, http.Handler, error) {
	named, def := resolveRegistry(f)
	if def == nil && len(named) == 0 {
		return nil, nil, ErrNoFlow
	}
	// Validate every served flow. No dedup: a Flow (seq) isn't hashable, and
	// Validate is idempotent, so re-checking a shared default/named flow is fine.
	for _, fl := range append(flowsOf(named), def) {
		if fl == nil {
			continue
		}
		if err := flow.Validate(fl); err != nil {
			return nil, nil, err
		}
	}
	c := defaults()
	for _, o := range opts {
		o(&c)
	}
	r := &ring{max: 500}
	s := &server{named: named, def: def, name: c.name, tracer: tee(r, c.tracer), ring: r, store: c.store}

	// Triggers/initiative need a store to schedule against. c.store defaults to
	// an in-memory one (defaults()), so this always runs unless wiring itself
	// fails.
	sched, hooks, err := wireScheduler(c.store)
	if err != nil {
		return nil, nil, err
	}
	s.sched = sched
	s.hooks = hooks

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.openai)
	mux.HandleFunc("POST /v1/messages", s.anthropic)
	mux.HandleFunc("POST /v1/hooks/{id}", s.webhook)
	mux.HandleFunc("GET /v1/models", s.models)
	mux.HandleFunc("GET /v1/diagnostics/trace", s.diagnostics)
	return s, mux, nil
}

// resolve picks the flow for a request's model id: a named match, else the
// default. The second return is the id echoed back to the client.
func (s *server) resolve(reqModel string) (flow.Flow, string) {
	if f, ok := s.named[reqModel]; ok {
		return f, reqModel
	}
	return s.def, s.name
}

func flowsOf(m map[string]flow.Flow) []flow.Flow {
	out := make([]flow.Flow, 0, len(m))
	for _, f := range m {
		out = append(out, f)
	}
	return out
}

// Serve runs the brain on the configured address until ctx is cancelled, then
// shuts down gracefully.
func Serve(ctx context.Context, f flow.Flow, opts ...Option) error {
	s, h, err := build(f, opts...)
	if err != nil {
		return err
	}
	c := defaults()
	for _, o := range opts {
		o(&c)
	}
	// Telemetry lifecycle: noop unless BIG_BRAIN_TELEMETRY selects stdout/otlp
	// (internal/telemetry.Start). Every Monitored wrapper in the codebase
	// already targets the global MeterProvider, so nothing else needs to know
	// this ran.
	shutdownTelemetry, err := telemetry.Start(ctx)
	if err != nil {
		return err
	}
	defer shutdownTelemetry(context.Background())
	// Run the durable job worker alongside HTTP: it fires the scheduled/deferred
	// flows (crons, one-shots) registered via triggers. Stops when ctx is done.
	// The worker ctx carries the scheduler too, so a fired body that itself
	// contains a trigger (a self-rescheduling job) can defer further work
	// instead of silently no-oping — the trigger cycle guard in
	// internal/flow/trigger.go only engages on this path.
	if s.sched != nil {
		go s.sched.run(flow.WithScheduler(ctx, s.sched), c.workers)
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

// reply is what one served request produced: the text to send back, plus the
// tool calls nobody answered. The keystone rule lives here — an unanswered call
// is what the brain owes its client, and it is the only tool state that leaves
// the process, because the client owns the transcript.
type reply struct {
	text  string
	calls []model.ToolCall
	// delivered is true once at least one Respond has run — its stage(s)
	// already reached the sink (if any) during the run itself, so the
	// streaming handlers' own "nobody streamed" fallback must not fire again
	// and re-send the same text.
	delivered bool
	// usage is the sum of every model call the run made, across every flow,
	// agent, and tool round — see internal/serve.run and docs/design-metrics.md.
	usage model.Usage
}

// triggerCtx wires Store/Scheduler/Webhooks onto ctx — everything a flow needs
// to reach a Durable checkpoint or a mid-flow Once/Every/Webhook, regardless
// of what kind of request is running it. runID empty gets a fresh one.
func (s *server) triggerCtx(ctx context.Context, runID string) context.Context {
	if s.store != nil {
		if runID == "" {
			runID = uuid.NewString()
		}
		// Make the store available; only Durable flows in the tree activate a
		// checkpoint (opt-in). A brain with no Durable flow persists nothing.
		ctx = flow.WithStore(ctx, s.store, runID)
	}
	if s.sched != nil {
		// A mid-request Once/Every can defer work to run after the reply.
		ctx = flow.WithScheduler(ctx, s.sched)
	}
	if s.hooks != nil {
		// A mid-request Webhook registers a new endpoint on the fly.
		ctx = flow.WithWebhooks(ctx, s.hooks)
	}
	return ctx
}

func (s *server) run(ctx context.Context, f flow.Flow, runID string, msgs []model.Message, req agent.Request) (reply, error) {
	ctx = s.triggerCtx(ctx, runID)
	tally := &model.Tally{}
	ctx = agent.WithTally(ctx, tally)
	out, err := flow.Run(ctx, f, flow.State{Chat: msgs, Req: req}, s.tracer)
	if err != nil {
		return reply{usage: tally.Total()}, err
	}
	// The keystone rule over the whole transcript: a call with no matching result
	// anywhere in the chat is owed to the client; one the client already answered,
	// or a brain resolved internally, is settled history and stays in.
	return reply{
		text: out.Answer(), calls: model.Unresolved(out.Chat), delivered: out.Sent() >= 0,
		usage: tally.Total(),
	}, nil
}

func (s *server) openai(w http.ResponseWriter, r *http.Request) {
	reqStart := time.Now()
	var req openai.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		openai.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	msgs := openai.Messages(req.Messages)
	f, name := s.resolve(req.Model)
	rp := agent.NewRequest(agent.Request{
		Model: req.Model, Temperature: req.Temperature, TopP: req.TopP,
		MaxTokens: req.MaxOutputTokens(), Stop: []string(req.Stop), Think: req.Think(),
	}, openai.Tools(req.Tools), string(req.ToolChoice))
	id := "chatcmpl-" + uuid.NewString()
	runID := r.Header.Get("X-Run-Id")

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := http.NewResponseController(w)
		var ttftOnce sync.Once
		sink := &agent.Sink{Write: func(_ context.Context, chunk string) error {
			ttftOnce.Do(func() { recordRequestTTFT(r.Context(), name, time.Since(reqStart)) })
			if err := openai.WriteChunk(w, id, name, chunk); err != nil {
				return err
			}
			return fl.Flush()
		}}
		out, err := s.run(agent.WithSink(r.Context(), sink), f, runID, msgs, rp)
		if err != nil {
			logrus.WithField("model", name).Error(fmt.Errorf("serve: chat completion: %w", err))
			openai.WriteStreamError(w, err.Error())
			fl.Flush()
			recordRequestSeconds(r.Context(), name, "error", true, time.Since(reqStart))
			return
		}
		if !out.delivered && !sink.Claimed() { // nobody streamed or flushed: emit the buffered reply as one delta
			openai.WriteChunk(w, id, name, out.text)
		}
		calls := openai.Calls(out.calls)
		openai.WriteToolCalls(w, id, name, calls)
		openai.WriteDone(w, id, name, calls, out.usage, req.IncludeUsage())
		fl.Flush()
		recordRequestSeconds(r.Context(), name, "ok", true, time.Since(reqStart))
		return
	}

	out, err := s.run(r.Context(), f, runID, msgs, rp)
	if err != nil {
		logrus.WithField("model", name).Error(fmt.Errorf("serve: chat completion: %w", err))
		openai.WriteError(w, http.StatusInternalServerError, err.Error())
		recordRequestSeconds(r.Context(), name, "error", false, time.Since(reqStart))
		return
	}
	openai.WriteResponse(w, id, name, out.text, openai.Calls(out.calls), out.usage)
	recordRequestSeconds(r.Context(), name, "ok", false, time.Since(reqStart))
}

func (s *server) anthropic(w http.ResponseWriter, r *http.Request) {
	reqStart := time.Now()
	var req anthropic.MessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		anthropic.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var msgs []model.Message
	if s := string(req.System); s != "" {
		msgs = append(msgs, model.Message{Role: "system", Content: s})
	}
	msgs = append(msgs, anthropic.Messages(req.Messages)...)
	f, name := s.resolve(req.Model)
	rp := agent.NewRequest(agent.Request{
		Model: req.Model, Temperature: req.Temperature, TopP: req.TopP, TopK: req.TopK,
		MaxTokens: req.MaxTokens, Stop: req.StopSequences, Think: req.Think(),
	}, anthropic.Tools(req.Tools), string(req.ToolChoice))
	id := "msg_" + uuid.NewString()
	runID := r.Header.Get("X-Run-Id")

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := http.NewResponseController(w)
		anthropic.WriteStart(w, id, name)
		fl.Flush()
		var ttftOnce sync.Once
		sink := &agent.Sink{Write: func(_ context.Context, chunk string) error {
			ttftOnce.Do(func() { recordRequestTTFT(r.Context(), name, time.Since(reqStart)) })
			if err := anthropic.WriteDelta(w, chunk); err != nil {
				return err
			}
			return fl.Flush()
		}}
		out, err := s.run(agent.WithSink(r.Context(), sink), f, runID, msgs, rp)
		if err != nil {
			logrus.WithField("model", name).Error(fmt.Errorf("serve: messages: %w", err))
			// WriteStart already opened block 0 above; close it before the
			// error frame so the stream isn't left with a dangling block. The
			// run errored, so out.usage isn't available — report the zero
			// Usage rather than guess.
			anthropic.WriteStop(w, nil, model.Usage{})
			anthropic.WriteStreamError(w, err.Error())
			fl.Flush()
			recordRequestSeconds(r.Context(), name, "error", true, time.Since(reqStart))
			return
		}
		if !out.delivered && !sink.Claimed() {
			anthropic.WriteDelta(w, out.text)
		}
		anthropic.WriteStop(w, anthropic.Calls(out.calls), out.usage)
		fl.Flush()
		recordRequestSeconds(r.Context(), name, "ok", true, time.Since(reqStart))
		return
	}

	out, err := s.run(r.Context(), f, runID, msgs, rp)
	if err != nil {
		logrus.WithField("model", name).Error(fmt.Errorf("serve: messages: %w", err))
		anthropic.WriteError(w, http.StatusInternalServerError, err.Error())
		recordRequestSeconds(r.Context(), name, "error", false, time.Since(reqStart))
		return
	}
	anthropic.WriteResponse(w, id, name, out.text, anthropic.Calls(out.calls), out.usage)
	recordRequestSeconds(r.Context(), name, "ok", false, time.Since(reqStart))
}

func (s *server) models(w http.ResponseWriter, _ *http.Request) {
	names := make([]string, 0, len(s.named)+1)
	if s.def != nil {
		names = append(names, s.name)
	}
	for n := range s.named {
		names = append(names, n)
	}
	openai.WriteModels(w, names...)
}

func (s *server) diagnostics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.ring.snapshot())
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
