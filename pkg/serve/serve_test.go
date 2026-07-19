package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/force1267/big-brain/pkg/brain"
	"github.com/force1267/big-brain/pkg/job"
	"github.com/force1267/big-brain/pkg/memory"
	"github.com/force1267/big-brain/pkg/model"
	"github.com/force1267/big-brain/pkg/notify"
)

func jarvis(m model.Model) *brain.Brain {
	return &brain.Brain{
		Name:   "jarvis",
		Models: model.Models{"fast": m},
		Chat:   []brain.Node{brain.Prompt("persona"), brain.Call("fast"), brain.Reply()},
	}
}

// handler builds a test Handler with mock dependencies.
func handler(b *brain.Brain) http.Handler {
	return Handler(b, Deps{Memory: &memory.Mock{}, Notify: &notify.Mock{}})
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCompletionsNonStreaming(t *testing.T) {
	mock := &model.Mock{Chunks: []string{"hello", " there"}}
	rec := post(t, handler(jarvis(mock)),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}],"temperature":0.2}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var resp struct {
		Object  string
		Choices []struct {
			Message      model.Message
			FinishReason *string `json:"finish_reason"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Object != "chat.completion" || resp.Choices[0].Message.Content != "hello there" {
		t.Fatalf("resp = %+v", resp)
	}
	// persona prepended, caller params passed through
	if mock.Got.Msgs[0].Role != "system" || mock.Got.Msgs[0].Content != "persona" {
		t.Fatalf("model got %+v", mock.Got.Msgs)
	}
	if mock.Got.Params.Temperature == nil || *mock.Got.Params.Temperature != 0.2 {
		t.Fatalf("params = %+v", mock.Got.Params)
	}
}

func TestCompletionsStreaming(t *testing.T) {
	rec := post(t, handler(jarvis(&model.Mock{Chunks: []string{"hel", "lo"}})),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`"hel"`, `"lo"`, `"finish_reason":"stop"`, "data: [DONE]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream missing %q:\n%s", want, body)
		}
	}
}

func TestCompletionsBadJSON(t *testing.T) {
	rec := post(t, handler(jarvis(&model.Mock{})), `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCompletionsBrainFailure(t *testing.T) {
	rec := post(t, handler(jarvis(&model.Mock{Reject: errors.New("boom")})),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCompletionsStreamFailureBeforeOutput(t *testing.T) {
	rec := post(t, handler(jarvis(&model.Mock{Reject: errors.New("boom")})),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSpeakerFromBearerKey(t *testing.T) {
	// the brain sees the speaker resolved from the API credential
	var seen string
	b := &brain.Brain{Name: "jarvis", Chat: []brain.Node{
		brain.Func(func(_ context.Context, r *brain.Run) error {
			seen = r.Speaker
			r.Replied = true
			return r.Emit(model.Chunk{Content: "ok"})
		}),
	}}
	h := Handler(b, Deps{Memory: &memory.Mock{}, Speakers: map[string]string{"key-dad": "dad"}})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"jarvis","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer key-dad")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "dad" {
		t.Fatalf("speaker = %q; want dad", seen)
	}

	// unknown or missing key → anonymous, never an error
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"jarvis","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || seen != "" {
		t.Fatalf("status %d speaker %q; want 200 and anonymous", rec.Code, seen)
	}
}

func TestPostReplyNodesRunDetached(t *testing.T) {
	// nodes after Reply run after the response is complete
	ran := make(chan struct{})
	b := jarvis(&model.Mock{Chunks: []string{"ok"}})
	b.Chat = append(b.Chat, brain.Func(func(context.Context, *brain.Run) error {
		close(ran)
		return nil
	}))
	rec := post(t, handler(b), `{"model":"jarvis","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("post-reply node never ran")
	}
}

func TestRunJobExecutesNamedPipeline(t *testing.T) {
	ch := &notify.Mock{}
	var got string
	b := &brain.Brain{Name: "jarvis", Pipelines: map[string][]brain.Node{
		"register-guest": {
			brain.Func(func(_ context.Context, r *brain.Run) error {
				g, _ := brain.Var[string](r, "guest")
				got = g
				r.SetVar("result", "done: "+g)
				return nil
			}),
			brain.Notify(`{{index .Vars "result"}}`),
		},
	}}
	deps := &Deps{Memory: &memory.Mock{}, Notify: ch}
	j := job.Job{ID: "1", Pipeline: "register-guest", Speaker: "dad", Payload: map[string]any{"guest": "John"}}
	if err := runJob(context.Background(), b, deps, j); err != nil {
		t.Fatalf("runJob: %v", err)
	}
	if got != "John" || len(ch.Sent) != 1 || ch.Sent[0].Text != "done: John" || ch.Sent[0].Speaker != "dad" {
		t.Fatalf("got %q, sent %+v", got, ch.Sent)
	}
}

func TestRunJobUnknownPipeline(t *testing.T) {
	err := runJob(context.Background(), &brain.Brain{}, &Deps{}, job.Job{Pipeline: "nope"})
	if !errors.Is(err, ErrNoPipeline) {
		t.Fatalf("err = %v; want ErrNoPipeline", err)
	}
}

func TestStartJobsRecoversAndRunsEnqueued(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 2)
	b := &brain.Brain{Pipelines: map[string][]brain.Node{
		"p": {brain.Func(func(_ context.Context, r *brain.Run) error {
			id, _ := brain.Var[string](r, "id")
			done <- id
			return nil
		})},
	}}
	store := &job.Mock{Pending: []job.Job{{ID: "old", Pipeline: "p", Payload: map[string]any{"id": "old"}}}}
	deps := &Deps{Memory: &memory.Mock{}, Notify: &notify.Mock{}}
	enqueue := startJobs(ctx, b, store, deps)

	// pending job from "before the crash" runs on startup
	if id := <-done; id != "old" {
		t.Fatalf("recovered %q; want old", id)
	}
	// a newly enqueued job runs without restart
	if err := enqueue(ctx, job.Job{ID: "new", Pipeline: "p", Payload: map[string]any{"id": "new"}}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case id := <-done:
		if id != "new" {
			t.Fatalf("ran %q; want new", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("enqueued job never ran")
	}
}

func TestMessagesNonStreaming(t *testing.T) {
	mock := &model.Mock{Chunks: []string{"hello", " there"}}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(
		`{"model":"jarvis","max_tokens":100,"system":"be brief","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	handler(jarvis(mock)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
	var resp struct {
		Type    string
		Content []struct{ Text string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "message" || resp.Content[0].Text != "hello there" {
		t.Fatalf("resp = %+v", resp)
	}
	// caller system message and max_tokens reached the model
	if mock.Got.Msgs[1].Role != "system" || mock.Got.Msgs[1].Content != "be brief" {
		t.Fatalf("model got %+v", mock.Got.Msgs)
	}
	if mock.Got.Params.MaxTokens == nil || *mock.Got.Params.MaxTokens != 100 {
		t.Fatalf("params = %+v", mock.Got.Params)
	}
}

func TestMessagesStreaming(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(
		`{"model":"jarvis","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	handler(jarvis(&model.Mock{Chunks: []string{"hel", "lo"}})).ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"event: message_start", `"text":"hel"`, `"text":"lo"`, "event: message_stop"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream missing %q:\n%s", want, body)
		}
	}
}

func TestMessagesSpeakerFromAPIKeyHeader(t *testing.T) {
	var seen string
	b := &brain.Brain{Name: "jarvis", Chat: []brain.Node{
		brain.Func(func(_ context.Context, r *brain.Run) error {
			seen = r.Speaker
			r.Replied = true
			return r.Emit(model.Chunk{Content: "ok"})
		}),
	}}
	h := Handler(b, Deps{Speakers: map[string]string{"key-kid": "kid"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"jarvis","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", "key-kid")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "kid" {
		t.Fatalf("speaker = %q; want kid", seen)
	}
}

func TestWebhookEnqueuesDurableJob(t *testing.T) {
	var got job.Job
	b := &brain.Brain{Webhooks: map[string]string{"door": "unknown-face"}}
	deps := Deps{Enqueue: func(_ context.Context, j job.Job) error { got = j; return nil }}
	h := Handler(b, deps)

	req := httptest.NewRequest(http.MethodPost, "/triggers/door",
		strings.NewReader(`{"event":"unrecognized_face","confidence":0.92}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
	if got.Pipeline != "unknown-face" {
		t.Fatalf("job = %+v", got)
	}
	payload := got.Payload["payload"].(map[string]any)
	if payload["event"] != "unrecognized_face" {
		t.Fatalf("payload = %+v", payload)
	}

	// unknown trigger and bad body
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/triggers/nope", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown trigger status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/triggers/door", strings.NewReader(`{bad`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", rec.Code)
	}
}

func TestNextCron(t *testing.T) {
	now := time.Date(2026, 7, 19, 20, 0, 0, 0, time.UTC)

	if got := nextCron(brain.Cron{Every: 5 * time.Minute}, now); !got.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("every: %v", got)
	}
	// daily later today
	if got := nextCron(brain.Cron{Daily: "21:00"}, now); !got.Equal(time.Date(2026, 7, 19, 21, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily today: %v", got)
	}
	// daily already passed → tomorrow
	if got := nextCron(brain.Cron{Daily: "19:00"}, now); !got.Equal(time.Date(2026, 7, 20, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily tomorrow: %v", got)
	}
	// invalid spec falls back to +24h instead of spinning
	if got := nextCron(brain.Cron{Daily: "not-a-time"}, now); !got.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("invalid: %v", got)
	}
}

func TestStartJobsRunsDeferredJobWhenDue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	b := &brain.Brain{Pipelines: map[string][]brain.Node{
		"p": {brain.Func(func(context.Context, *brain.Run) error { close(done); return nil })},
	}}
	store := &job.Mock{}
	enqueue := startJobs(ctx, b, store, &Deps{})

	// due in 150ms — the runner must wake itself, no external nudge
	err := enqueue(ctx, job.Job{ID: "later", Pipeline: "p", RunAt: time.Now().Add(150 * time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("deferred job never ran")
	}
}

func TestModelsListsTheBrain(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler(jarvis(&model.Mock{})).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"jarvis"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}
