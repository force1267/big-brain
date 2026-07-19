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
	"github.com/force1267/big-brain/pkg/memory"
	"github.com/force1267/big-brain/pkg/model"
)

var (
	// ErrConfig wraps configuration failures during startup.
	ErrConfig = errors.New("serve config stage failed")
	// ErrServer wraps HTTP server failures.
	ErrServer = errors.New("serve http server failed")
)

// Run loads deployment configuration, binds the brain's model roles, and
// serves the brain over the OpenAI-compatible API until ctx is cancelled.
// It is the one call a brain author's main makes.
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

	srv := &http.Server{Addr: cfg.HTTP.Addr, Handler: Handler(b, mem, cfg.Speakers)}
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

// Handler returns the OpenAI-compatible http.Handler for one brain, with
// its memory and the API-key → speaker map. Exported separately from Run
// so it is testable and embeddable.
func Handler(b *brain.Brain, mem memory.Memory, speakers map[string]string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		completions(b, mem, speakers, w, r)
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		openaiwire.WriteModels(w, b.Name)
	})
	return mux
}

func completions(b *brain.Brain, mem memory.Memory, speakers map[string]string, w http.ResponseWriter, r *http.Request) {
	var req openaiwire.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		openaiwire.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	run := &brain.Run{
		Params:  model.Params{Temperature: req.Temperature, MaxTokens: req.MaxTokens},
		Models:  b.Models,
		Memory:  mem,
		Speaker: speakers[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")],
	}
	for _, m := range req.Messages {
		run.Messages = append(run.Messages, model.Message{Role: m.Role, Content: m.Content})
	}

	id := "chatcmpl-" + uuid.NewString()
	if req.Stream {
		streamCompletion(b, run, id, w, r)
		return
	}

	var full strings.Builder
	run.Emit = func(c model.Chunk) error {
		full.WriteString(c.Content)
		return nil
	}
	if err := brain.Execute(r.Context(), b.Chat, run); err != nil {
		logrus.WithError(err).Error("chat run failed")
		openaiwire.WriteError(w, http.StatusInternalServerError, "brain run failed")
		return
	}
	openaiwire.WriteResponse(w, id, b.Name, full.String())
}

func streamCompletion(b *brain.Brain, run *brain.Run, id string, w http.ResponseWriter, r *http.Request) {
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
	if err := brain.Execute(r.Context(), b.Chat, run); err != nil {
		logrus.WithError(err).Error("chat run failed")
		if !started {
			openaiwire.WriteError(w, http.StatusInternalServerError, "brain run failed")
		}
		// ponytail: mid-stream failure just truncates the SSE stream; an
		// error event convention can come with the authoring guide.
		return
	}
	if started {
		_ = openaiwire.WriteDone(w, id, b.Name)
		flusher.Flush()
	}
}
