package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/pkg/engine"
)

// A webhook whose body reaches Respond runs synchronously and replies with the
// resulting chat's content.
func TestWebhookWithReplyRespondsSync(t *testing.T) {
	flow.ResetTriggers()
	t.Cleanup(flow.ResetTriggers)

	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Reply("got: " + string(turn.Payload()))
		return nil
	}))
	flow.Trigger().Next(flow.Webhook("with-reply")).Next(body).Next(flow.Respond)

	_, mux, err := build(talkFlow("x"), Store(engine.NewMemStore()))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/hooks/with-reply", strings.NewReader(`hello`))
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Body.String(); got != "got: hello" {
		t.Fatalf("expected reply content, got %q", got)
	}
}

// A webhook whose body has no top-level Respond is acknowledged immediately
// (202) and runs in the background — the caller is not blocked on it.
func TestWebhookNoReplyAcknowledgesAsync(t *testing.T) {
	flow.ResetTriggers()
	t.Cleanup(flow.ResetTriggers)

	ran := make(chan string, 1)
	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		ran <- string(turn.Payload())
		return nil
	}))
	flow.Trigger().Next(flow.Webhook("no-reply")).Next(body)

	_, mux, err := build(talkFlow("x"), Store(engine.NewMemStore()))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/hooks/no-reply", strings.NewReader(`world`))
	mux.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	select {
	case got := <-ran:
		if got != "world" {
			t.Fatalf("expected payload %q, got %q", "world", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("background webhook body did not run")
	}
}

// A webhook's request headers reach the body via bb.Metadata[T]
// (turn.Metadata, agent.MetadataFrom) — Payload's sibling channel, populated
// from headers rather than the POST body.
func TestWebhookHeadersReachMetadata(t *testing.T) {
	flow.ResetTriggers()
	t.Cleanup(flow.ResetTriggers)

	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Reply("meta: " + string(turn.Metadata()))
		return nil
	}))
	flow.Trigger().Next(flow.Webhook("with-meta")).Next(body).Next(flow.Respond)

	_, mux, err := build(talkFlow("x"), Store(engine.NewMemStore()))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/hooks/with-meta", strings.NewReader(`{}`))
	req.Header.Set("X-Signature", "sig-123")
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Body.String(); got != `meta: {"X-Signature":"sig-123"}` {
		t.Fatalf("expected header surfaced as metadata, got %q", got)
	}
}

// flattenHeaders canonicalizes header keys and keeps only the first value of
// a repeated header (http.Header.Get's own convention) — the wire shape
// bb.Metadata[T] decodes, deliberately map[string]string rather than
// http.Header (Metadata is not HTTP-specific).
func TestFlattenHeaders(t *testing.T) {
	h := http.Header{}
	h.Add("x-signature", "first")
	h.Add("X-Signature", "second")
	h.Set("Content-Type", "application/json")

	raw := flattenHeaders(h)
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["X-Signature"] != "first" {
		t.Fatalf("expected canonical key with first value, got %+v", got)
	}
	if got["Content-Type"] != "application/json" {
		t.Fatalf("expected Content-Type preserved, got %+v", got)
	}
	if len(flattenHeaders(http.Header{})) != 0 {
		t.Fatal("expected empty headers to flatten to nothing")
	}
}

// An unregistered endpoint id 404s rather than silently no-oping.
func TestWebhookUnknownEndpoint404s(t *testing.T) {
	_, mux, err := build(talkFlow("x"), Store(engine.NewMemStore()))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/hooks/nope", strings.NewReader(``))
	mux.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body)
	}
}
