package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/force1267/big-brain/internal/flow"
	"github.com/google/uuid"
)

// ErrDupWebhook means two Webhook triggers registered the same endpoint id —
// an author mistake (see flow.Webhook's doc: the id is the public route
// slug), caught loudly at registration time rather than one silently
// shadowing the other.
var ErrDupWebhook = errors.New("serve: duplicate webhook endpoint id")

// webhookRegistry implements flow.Webhooks: an in-process map from endpoint
// id to the handler a Webhook trigger registered. Unlike engineScheduler,
// this needs no Store or durable engine — firing a webhook is a normal
// synchronous flow.Run, not a durable schedule, so a plain map is the whole
// mechanism.
type webhookRegistry struct {
	mu    sync.RWMutex
	hooks map[string]flow.WebhookHandler
}

func newWebhookRegistry() *webhookRegistry {
	return &webhookRegistry{hooks: map[string]flow.WebhookHandler{}}
}

// Register implements flow.Webhooks.
func (r *webhookRegistry) Register(endpointID string, h flow.WebhookHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.hooks[endpointID]; dup {
		return fmt.Errorf("%w: %q", ErrDupWebhook, endpointID)
	}
	r.hooks[endpointID] = h
	return nil
}

func (r *webhookRegistry) lookup(endpointID string) (flow.WebhookHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.hooks[endpointID]
	return h, ok
}

// webhook handles POST /v1/hooks/{id}. No auth, no rate limit, no body-size
// cap here — net/http has no stdlib default to expose (checked: only
// MaxHeaderBytes exists, which is headers-only), and auth/rate-limiting is a
// reverse-proxy/gateway concern, same as every other route this package
// serves. The endpoint id is not a secret; don't rely on it as one.
func (s *server) webhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h, ok := s.hooks.lookup(id)
	if !ok {
		http.Error(w, "unknown webhook endpoint", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	meta := flattenHeaders(r.Header)
	runID := r.Header.Get("X-Run-Id")
	if runID == "" {
		runID = uuid.NewString()
	}

	if h.HasReply {
		out, err := h.Run(s.triggerCtx(r.Context(), runID), body, meta)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(lastContent(out.Chat)))
		return
	}

	// No top-level Respond to wait for — likely a long-running job. Acknowledge
	// now rather than block the caller on it. The background run needs a ctx
	// detached from this request: r.Context() is cancelled the instant this
	// handler returns (right after WriteHeader), which would kill the run
	// almost immediately if reused directly.
	ctx := s.triggerCtx(context.WithoutCancel(r.Context()), runID)
	w.WriteHeader(http.StatusAccepted)
	go func() {
		if _, err := h.Run(ctx, body, meta); err != nil {
			logrus.WithField("endpoint", id).Error(fmt.Errorf("serve: webhook run: %w", err))
		}
	}()
}

// flattenHeaders turns a request's headers into bb.Metadata[T]'s wire shape:
// map[string]string, not http.Header — Metadata is not HTTP-specific (a
// non-HTTP trigger seeds the same shape via WithSeedMetadata), so the
// HTTP-only, multi-value shape stops here. Keys are canonicalized (same
// casing net/http already normalizes to) and only the first value of a
// repeated header is kept, matching http.Header.Get's own convention.
// Marshalled now so it travels through scheduling/replay same as payload.
func flattenHeaders(h http.Header) []byte {
	if len(h) == 0 {
		return nil
	}
	flat := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			flat[http.CanonicalHeaderKey(k)] = v[0]
		}
	}
	raw, _ := json.Marshal(flat)
	return raw
}
