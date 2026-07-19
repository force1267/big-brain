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

	"github.com/force1267/big-brain/internal/config"
	"github.com/force1267/big-brain/internal/logging"
	openaiwire "github.com/force1267/big-brain/internal/openai"
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
)

// Deps are the engine-owned ambient dependencies a run sees. Run builds
// them from configuration; tests inject mocks.
type Deps struct {
	Memory   memory.Memory
	Notify   notify.Channel
	Speakers map[string]string // API key → speaker name
	Enqueue  func(context.Context, job.Job) error
}

// Run loads deployment configuration, binds the brain's model roles,
// opens the durable stores, recovers pending background jobs, and serves
// the brain over the OpenAI-compatible API until ctx is cancelled. It is
// the one call a brain author's main makes.
func Run(ctx context.Context, b *brain.Brain) error {
	cfg, err := config.New().Load()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}
	if err := logging.New().Init(cfg); err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}

	if b.Models == nil {
		b.Models = model.Models{}
	}
	for role, name := range cfg.Models {
		b.Models[model.Role(role)] = model.OpenAI(cfg.Upstream.BaseURL, cfg.Upstream.APIKey, name)
	}

	mem, err := memory.OpenFile(cfg.Memory.Path)
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

	deps := Deps{Memory: mem, Notify: channel, Speakers: cfg.Speakers}
	deps.Enqueue = startJobs(ctx, b, store, &deps)

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
// arrive. The returned enqueue persists intent before waking the runner.
func startJobs(ctx context.Context, b *brain.Brain, store job.Store, deps *Deps) func(context.Context, job.Job) error {
	wake := make(chan struct{}, 1)
	sweep := func() {
		err := store.Sweep(ctx, func(ctx context.Context, j job.Job) error {
			return runJob(ctx, b, deps, j)
		})
		if err != nil {
			logrus.WithError(err).Error("job sweep failed")
		}
	}
	go func() {
		sweep() // crash recovery: whatever was pending re-runs from the start
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				sweep()
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
		Speaker: j.Speaker,
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
		Speaker: deps.Speakers[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")],
	}
	for _, m := range req.Messages {
		run.Messages = append(run.Messages, model.Message{Role: m.Role, Content: m.Content})
	}

	id := "chatcmpl-" + uuid.NewString()
	var finish func() // completes the HTTP response once the reply is out
	if req.Stream {
		flusher, ok := w.(http.Flusher)
		if !ok {
			openaiwire.WriteError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		started := false
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

	// Execute node by node so the response can close the moment the reply
	// has streamed; nodes after Reply continue detached from the request —
	// that is what "background" means in this engine.
	for i, n := range b.Chat {
		if err := n.Run(r.Context(), run); err != nil {
			logrus.WithError(fmt.Errorf("%w: node %d: %w", brain.ErrNode, i, err)).Error("chat run failed")
			if !run.Replied {
				openaiwire.WriteError(w, http.StatusInternalServerError, "brain run failed")
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
	openaiwire.WriteError(w, http.StatusInternalServerError, "brain produced no reply")
}
