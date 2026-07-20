package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	anthropicwire "github.com/force1267/big-brain/internal/anthropic"
	"github.com/force1267/big-brain/internal/config"
	"github.com/force1267/big-brain/internal/logging"
	openaiwire "github.com/force1267/big-brain/internal/openai"
	"github.com/force1267/big-brain/internal/telemetry"
	"github.com/force1267/big-brain/pkg/brain"
	"github.com/force1267/big-brain/pkg/job"
	"github.com/force1267/big-brain/pkg/memory"
	"github.com/force1267/big-brain/pkg/model"
	"github.com/force1267/big-brain/pkg/notify"
)

var (
	// ErrConfig wraps configuration failures during startup.
	ErrConfig = errors.New("serve config stage failed")
	// ErrServer wraps HTTP server failures.
	ErrServer = errors.New("serve http server failed")
	// ErrNoPipeline is returned when a job names a pipeline the brain
	// does not register.
	ErrNoPipeline = errors.New("no such pipeline")
	// ErrNoMemoryModel is returned when BIG_BRAIN_MEMORY_BACKEND=llm names
	// a role (BIG_BRAIN_MEMORY_LLM_ROLE) the brain has no model bound to.
	ErrNoMemoryModel = errors.New("no model bound to memory llm role")
)

// Enqueue persists durable intent to run a named pipeline — the one
// primitive every trigger, of any kind, ultimately calls. Handed to
// WithBackground and WithEndpoint once it exists, so brain author code
// can compose its own trigger sources from it without pkg/serve needing
// to know what kinds of triggers exist.
type Enqueue func(context.Context, job.Job) error

// Deps are the engine-owned ambient dependencies a run sees. Run builds
// them from configuration; tests inject mocks.
type Deps struct {
	Memory  memory.Memory
	Notify  notify.Channel
	Enqueue Enqueue
	// Prepare, if set, runs once per incoming chat/messages request, right
	// after the Run is built and before the pipeline executes, with the
	// raw HTTP request. It lets the brain author inject whatever
	// per-request context their brain needs — identity, locale, tracing,
	// anything — via run.SetVar. The engine has no opinion on what
	// belongs there.
	Prepare func(*http.Request, *brain.Run)
	// Background holds functions registered via WithBackground, each
	// started once at startup, after Enqueue exists.
	Background []func(context.Context, Enqueue)
	// Endpoints holds routes registered via WithEndpoint, mounted onto
	// the same shared server the chat/messages endpoints use.
	Endpoints []endpoint
}

type endpoint struct {
	pattern string
	build   func(Enqueue) http.HandlerFunc
}

// Option configures Deps beyond what deployment config supplies. Pass one
// to Run for behavior only the brain author's code can provide.
type Option func(*Deps)

// WithPrepare sets Deps.Prepare.
func WithPrepare(fn func(*http.Request, *brain.Run)) Option {
	return func(d *Deps) { d.Prepare = fn }
}

// WithBackground registers fn to run once at startup, after Enqueue
// exists, so it can drive any trigger source it likes — a schedule, a
// poller, a queue consumer — using nothing but fn's own goroutines, the
// stdlib, and enqueue. The engine has no concept of "trigger kinds"; this
// is the whole extension point.
func WithBackground(fn func(ctx context.Context, enqueue Enqueue)) Option {
	return func(d *Deps) { d.Background = append(d.Background, fn) }
}

// WithEndpoint registers pattern on the engine's own shared HTTP server.
// build runs once, after Enqueue exists, and returns the actual
// per-request handler — so an HTTP-driven trigger always has a working
// Enqueue to call, expressed entirely in the brain author's own code: the
// route is one line, what it does to the graph is another, and neither
// needs pkg/serve to know anything about "webhook triggers" as a concept.
func WithEndpoint(pattern string, build func(enqueue Enqueue) http.HandlerFunc) Option {
	return func(d *Deps) { d.Endpoints = append(d.Endpoints, endpoint{pattern: pattern, build: build}) }
}

// Run loads deployment configuration, binds the brain's model roles,
// opens the durable stores, recovers pending background jobs, and serves
// the brain over the OpenAI-compatible API until ctx is cancelled. It is
// the one call a brain author's main makes.
func Run(ctx context.Context, b *brain.Brain, opts ...Option) error {
	cfg, err := config.New().Load()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}
	if err := logging.New().Init(cfg); err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}
	tel := telemetry.New(cfg)
	if err := tel.Start(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}
	defer func() {
		if err := tel.Shutdown(context.WithoutCancel(ctx)); err != nil {
			logrus.WithError(err).Error("telemetry shutdown")
		}
	}()

	if b.Models == nil {
		b.Models = model.Models{}
	}
	for role, name := range cfg.Models {
		b.Models[model.Role(role)] = model.OpenAI(cfg.Upstream.BaseURL, cfg.Upstream.APIKey, name)
	}

	var mem memory.Memory
	switch cfg.Memory.Backend {
	case config.MemoryBackendLLM:
		mm, ok := b.Models[model.Role(cfg.Memory.LLMRole)]
		if !ok {
			return fmt.Errorf("%w: %w: %q", ErrConfig, ErrNoMemoryModel, cfg.Memory.LLMRole)
		}
		mem, err = memory.OpenLLM(cfg.Memory.Path, mm, cfg.Memory.LLMLimit)
	default:
		mem, err = memory.OpenFile(cfg.Memory.Path, cfg.Memory.FileLimit)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}
	store, err := job.OpenFile(cfg.Jobs.Path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}
	channel := notify.Log()
	if cfg.Notify.URL != "" {
		channel = notify.Webhook(cfg.Notify.URL)
	}

	deps := Deps{Memory: mem, Notify: channel}
	for _, opt := range opts {
		opt(&deps)
	}
	deps.Enqueue = startJobs(ctx, b, store, &deps)
	for _, fn := range deps.Background {
		go fn(ctx, deps.Enqueue)
	}

	srv := &http.Server{Addr: cfg.HTTP.Addr, Handler: Handler(b, deps)}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logrus.WithFields(logrus.Fields{"brain": b.Name, "addr": cfg.HTTP.Addr}).Info("brain serving")

	select {
	case err := <-errc:
		return fmt.Errorf("%w: %w", ErrServer, err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("%w: %w", ErrServer, err)
		}
		logrus.Info("brain stopped")
		return nil
	}
}

// startJobs recovers pending jobs, then runs newly enqueued ones as they
// arrive; deferred jobs (self-installed triggers) fire when due. The
// returned enqueue persists intent before waking the runner.
func startJobs(ctx context.Context, b *brain.Brain, store job.Store, deps *Deps) Enqueue {
	wake := make(chan struct{}, 1)
	sweep := func() time.Time {
		next, err := store.Sweep(ctx, func(ctx context.Context, j job.Job) error {
			return runJob(ctx, b, deps, j)
		})
		if err != nil {
			logrus.WithError(err).Error("job sweep failed")
		}
		return next
	}
	go func() {
		timer := time.NewTimer(time.Hour)
		defer timer.Stop()
		for {
			next := sweep() // first pass = crash recovery
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if !next.IsZero() {
				timer.Reset(time.Until(next))
			}
			select {
			case <-ctx.Done():
				return
			case <-wake:
			case <-timer.C:
			}
		}
	}()
	return func(ctx context.Context, j job.Job) error {
		if err := store.Enqueue(ctx, j); err != nil {
			return err
		}
		select {
		case wake <- struct{}{}:
		default:
		}
		return nil
	}
}

// runJob executes one background job against its named pipeline.
func runJob(ctx context.Context, b *brain.Brain, deps *Deps, j job.Job) error {
	log := logrus.WithFields(logrus.Fields{"job": j.ID, "pipeline": j.Pipeline})
	nodes, ok := b.Pipelines[j.Pipeline]
	if !ok {
		log.Error("job names unregistered pipeline")
		return fmt.Errorf("%w: %q", ErrNoPipeline, j.Pipeline)
	}
	run := &brain.Run{
		Models:  b.Models,
		Memory:  deps.Memory,
		Notify:  deps.Notify,
		Enqueue: deps.Enqueue,
		Vars:    j.Payload,
	}
	if err := brain.Execute(ctx, nodes, run); err != nil {
		// deliberately not re-notified by the engine: whether a failed
		// background job tells the user is the brain's choice (PRODUCT.md)
		log.WithError(err).Error("background job failed")
		return err
	}
	log.Info("background job done")
	return nil
}

// Handler returns the OpenAI-compatible http.Handler for one brain.
// Exported separately from Run so it is testable and embeddable.
func Handler(b *brain.Brain, deps Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		completions(b, deps, w, r)
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		openaiwire.WriteModels(w, b.Name)
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		messages(b, deps, w, r)
	})
	for _, ep := range deps.Endpoints {
		mux.HandleFunc(ep.pattern, ep.build(deps.Enqueue))
	}
	return mux
}

func completions(b *brain.Brain, deps Deps, w http.ResponseWriter, r *http.Request) {
	var req openaiwire.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		openaiwire.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	run := &brain.Run{
		Params:  model.Params{Temperature: req.Temperature, MaxTokens: req.MaxTokens},
		Models:  b.Models,
		Memory:  deps.Memory,
		Notify:  deps.Notify,
		Enqueue: deps.Enqueue,
	}
	for _, m := range req.Messages {
		run.Messages = append(run.Messages, model.Message{Role: m.Role, Content: m.Content})
	}
	if deps.Prepare != nil {
		deps.Prepare(r, run)
	}

	id := "chatcmpl-" + uuid.NewString()
	var finish func() // completes the HTTP response once the reply is out
	started := false  // once streaming has begun, error bodies must not be written
	writeErr := func(w http.ResponseWriter, status int, msg string) {
		if !started {
			openaiwire.WriteError(w, status, msg)
		}
	}
	if req.Stream {
		flusher, ok := w.(http.Flusher)
		if !ok {
			openaiwire.WriteError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		run.Emit = func(c model.Chunk) error {
			if !started {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				started = true
			}
			if err := openaiwire.WriteChunk(w, id, b.Name, c.Content); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}
		finish = func() {
			if started {
				_ = openaiwire.WriteDone(w, id, b.Name)
				flusher.Flush()
			}
		}
	} else {
		var full strings.Builder
		run.Emit = func(c model.Chunk) error {
			full.WriteString(c.Content)
			return nil
		}
		finish = func() { openaiwire.WriteResponse(w, id, b.Name, full.String()) }
	}

	executeChat(b, w, r, run, finish, writeErr)
}

// executeChat runs the chat pipeline node by node so the response can
// close the moment the reply has streamed; nodes after Reply continue
// detached from the request — that is what "background" means in this
// engine. writeErr is the caller's protocol-shaped error writer; it must
// be safe to skip once the response has started streaming.
func executeChat(b *brain.Brain, w http.ResponseWriter, r *http.Request, run *brain.Run,
	finish func(), writeErr func(http.ResponseWriter, int, string)) {
	for i, n := range b.Chat {
		if err := n.Run(r.Context(), run); err != nil {
			logrus.WithError(fmt.Errorf("%w: node %d: %w", brain.ErrNode, i, err)).Error("chat run failed")
			if !run.Replied {
				// mid-stream failures just truncate the stream; writeErr
				// callers guard against writing onto started responses
				writeErr(w, http.StatusInternalServerError, "brain run failed")
			}
			return
		}
		if run.Replied {
			finish()
			if rest := b.Chat[i+1:]; len(rest) > 0 {
				go func(ctx context.Context) {
					if err := brain.Execute(ctx, rest, run); err != nil {
						logrus.WithError(err).Error("post-reply pipeline failed")
					}
				}(context.WithoutCancel(r.Context()))
			}
			return
		}
	}
	// the pipeline never replied — that is a brain bug, not a caller error
	logrus.Error("chat pipeline finished without replying")
	writeErr(w, http.StatusInternalServerError, "brain produced no reply")
}

// messages serves the Anthropic-compatible endpoint over the same brain.
func messages(b *brain.Brain, deps Deps, w http.ResponseWriter, r *http.Request) {
	var req anthropicwire.MessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		anthropicwire.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	run := &brain.Run{
		Params:  model.Params{Temperature: req.Temperature, MaxTokens: req.MaxTokens},
		Models:  b.Models,
		Memory:  deps.Memory,
		Notify:  deps.Notify,
		Enqueue: deps.Enqueue,
	}
	if req.System != "" {
		run.Messages = append(run.Messages, model.Message{Role: "system", Content: string(req.System)})
	}
	for _, m := range req.Messages {
		run.Messages = append(run.Messages, model.Message{Role: m.Role, Content: string(m.Content)})
	}
	if deps.Prepare != nil {
		deps.Prepare(r, run)
	}

	id := "msg_" + uuid.NewString()
	var finish func()
	started := false
	writeErr := func(w http.ResponseWriter, status int, msg string) {
		if !started {
			anthropicwire.WriteError(w, status, msg)
		}
	}
	if req.Stream {
		flusher, ok := w.(http.Flusher)
		if !ok {
			anthropicwire.WriteError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		run.Emit = func(c model.Chunk) error {
			if !started {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				if err := anthropicwire.WriteStart(w, id, b.Name); err != nil {
					return err
				}
				started = true
			}
			if err := anthropicwire.WriteDelta(w, c.Content); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}
		finish = func() {
			if started {
				_ = anthropicwire.WriteStop(w)
				flusher.Flush()
			}
		}
	} else {
		var full strings.Builder
		run.Emit = func(c model.Chunk) error {
			full.WriteString(c.Content)
			return nil
		}
		finish = func() { anthropicwire.WriteResponse(w, id, b.Name, full.String()) }
	}

	executeChat(b, w, r, run, finish, writeErr)
}
