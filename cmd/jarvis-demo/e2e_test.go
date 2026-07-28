package main

// jarvis is the repo's end-to-end test: this file assembles the real brain
// from main.go over a dummy house and two scripted models, drives it over
// real HTTP (both wire protocols, streaming and buffered, durable resume,
// crons), and asserts the reply, the trace, and the world's side effects.
// See docs/design-jarvis-e2e.md for the design this file implements.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/force1267/big-brain/pkg/bb"
	"github.com/force1267/big-brain/pkg/model"
)

// --- the mock model ---
//
// pkg/model.Mock (pkg/model/mock.go) already covers scripted replies and
// gibberish, but has no per-call error injection or delay, and this session
// is scoped to cmd/jarvis-demo — so a small local double lives here instead
// of growing the shared one. It only needs to satisfy pkg/model.Model.
type mockModel struct {
	mu     sync.Mutex
	Script []string      // per-call reply text; the last entry repeats
	Chunks []string      // when set, streamed as several deltas instead of Script
	Errs   []error       // Errs[i] ends the i'th call's stream instead of Script/Chunks
	Delay  time.Duration // Stream waits this long before answering, honoring ctx
	calls  int
	Got    struct {
		Msgs   []model.Message
		Params model.Params
	}
}

var _ model.Model = (*mockModel)(nil)

func (m *mockModel) Stream(ctx context.Context, msgs []model.Message, p model.Params) (<-chan model.Chunk, error) {
	m.mu.Lock()
	m.Got.Msgs, m.Got.Params = msgs, p
	i := m.calls
	m.calls++
	m.mu.Unlock()

	if m.Delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.Delay):
		}
	}

	out := make(chan model.Chunk, len(m.Chunks)+1)
	if i < len(m.Errs) && m.Errs[i] != nil {
		out <- model.Chunk{Err: m.Errs[i]}
		close(out)
		return out, nil
	}
	if len(m.Chunks) > 0 {
		for _, c := range m.Chunks {
			out <- model.Chunk{Content: c}
		}
		close(out)
		return out, nil
	}
	if len(m.Script) > 0 {
		out <- model.Chunk{Content: m.Script[min(i, len(m.Script)-1)]}
	}
	close(out)
	return out, nil
}

// reset clears everything but Got, so a fixture can be reused between
// scenarios without one row's script leaking into the next.
func (m *mockModel) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Script, m.Chunks, m.Errs, m.Delay, m.calls = nil, nil, nil, 0, 0
}

// --- the fixture ---

// fixture is the one brain, world and pair of mocks the whole e2e binary
// shares. The model, flow and trigger registries in pkg/model/internal/flow
// are process-global with no public reset (see docs/design-jarvis-e2e.md,
// "Process-global registries"), so building a second fixture in the same
// binary would either double-register triggers or silently keep answering
// from the first registration (model.Lookup returns the first tag match).
// Every scenario instead reconfigures fast/smart.Script between subtests.
type fixture struct {
	j     *jarvis
	w     *world
	fast  *mockModel
	smart *mockModel
	url   string
}

var (
	fx     *fixture
	fxOnce sync.Once
)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fxOnce.Do(func() {
		fast, smart := &mockModel{}, &mockModel{}
		bb.WithModel(model.Bound(fast)).WithTag(mFast)
		bb.WithModel(model.Bound(smart)).WithTag(mSmart)

		w := newWorld()
		// No t.Cleanup: fxOnce.Do only ever runs during the first test that
		// calls newFixture, so a Cleanup registered here would tear the shared
		// server down when *that* test finishes, not when the binary exits —
		// breaking every later test. The process exiting reclaims both servers.
		wsrv := httptest.NewServer(w.handler())

		j := &jarvis{
			house: &client{base: wsrv.URL, http: &http.Client{Timeout: 2 * time.Second}},
			mem:   openMemory(context.Background(), bb.MemStore()),
		}

		brain := j.route().
			Next(bb.Select(
				j.talk(), j.remember(), j.forget(), j.recall(), j.lists(), j.control(), j.briefing(), j.remind(),
			).WithModel(bb.NewModel(mFast)).WithId("capabilities")).
			Next(bb.Respond).
			Next(j.speak())

		// Initiative, registered once for the whole binary — same as main().
		j.routines()

		// Each routine body served under its own model name too, so a test can
		// drive it directly (POST with "model": idBoot/.../idGoodnight) instead
		// of waiting on its cron.
		bb.WithFlow(j.boot().Next(j.speak())).As(idBoot)
		bb.WithFlow(j.sweep()).As(idSweep)
		bb.WithFlow(j.morning().Next(j.speak())).As(idMorning)
		bb.WithFlow(j.goodnight().Next(j.speak())).As(idGoodnight)

		h, err := bb.Handler(brain, bb.Store(bb.MemStore()))
		if err != nil {
			t.Fatal(err) // also the cron-spec validity check: a bad spec fails here
		}
		srv := httptest.NewServer(h)

		fx = &fixture{j: j, w: w, fast: fast, smart: smart, url: srv.URL}
	})
	return fx
}

// --- HTTP helpers ---

func askModel(t *testing.T, base, modelName, msg string, headers map[string]string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"model":    modelName,
		"messages": []map[string]string{{"role": "user", "content": msg}},
	})
	req, err := http.NewRequest(http.MethodPost, base+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ask %q: %s: %s", msg, resp.Status, raw)
	}
	var out struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		t.Fatalf("ask %q: bad reply %s", msg, raw)
	}
	return out.Choices[0].Message.Content
}

func askAnthropic(t *testing.T, base, msg string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"model":      "jarvis",
		"max_tokens": 256,
		"messages":   []map[string]string{{"role": "user", "content": msg}},
	})
	resp, err := http.Post(base+"/v1/messages", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ask anthropic %q: %s: %s", msg, resp.Status, raw)
	}
	var out struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Type != "message" || len(out.Content) == 0 {
		t.Fatalf("ask anthropic %q: bad reply %s", msg, raw)
	}
	return out.Content[0].Text
}

// traceEvent mirrors just the fields of internal/flow.Event the assertions
// below need, read back over the public /v1/diagnostics/trace endpoint.
type traceEvent struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

func trace(t *testing.T, base string) []traceEvent {
	t.Helper()
	resp, err := http.Get(base + "/v1/diagnostics/trace")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var events []traceEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	return events
}

// --- the scenario table ---

type scenario struct {
	name  string
	wire  string // "" = openai, "anthropic"
	say   string
	fast  []string // router/extractor script; a bad-JSON entry forces the keyword path
	smart []string // prose script
	errs  []error  // per-call injection on the fast model, index-aligned with fast
	want  string   // substring of the reply
	route string   // the capability id that must appear in select.enter
	then  func(t *testing.T, f *fixture)
}

func want(t *testing.T, got, sub string) {
	t.Helper()
	if !strings.Contains(got, sub) {
		t.Errorf("got %q, want it to contain %q", got, sub)
	}
}

var scenarios = []scenario{
	{
		name:  "talk answers in the smart voice",
		say:   "how are you doing today",
		fast:  []string{`{"intent":"talk","reason":"chat"}`},
		smart: []string{"At your service, always."},
		want:  "At your service",
		route: idTalk,
	},
	{
		name:  "control sets a device",
		say:   "turn on the heater",
		fast:  []string{`{"intent":"house","reason":"device change"}`, `{"device":"heater","state":"on"}`},
		want:  "heater is on",
		route: idHouse,
		then:  func(t *testing.T, f *fixture) { want(t, f.w.snapshot().Devices["heater"], "on") },
	},
	{
		name:  "gibberish router falls back to keywords",
		say:   "turn off the porch light",
		fast:  []string{"}{ not json at all"},
		want:  "porch light is off",
		route: idHouse,
		then:  func(t *testing.T, f *fixture) { want(t, f.w.snapshot().Devices["porch light"], "off") },
	},
	{
		name:  "remember keeps a fact",
		say:   "remember the spare key is under the pot",
		fast:  []string{`{"intent":"remember","reason":"fact"}`, `{"fact":"the spare key is under the pot"}`},
		want:  "spare key",
		route: idRemember,
		then: func(t *testing.T, f *fixture) {
			facts := f.j.mem.facts()
			if len(facts) == 0 || !strings.Contains(facts[len(facts)-1], "spare key") {
				t.Errorf("fact not kept: %v", facts)
			}
		},
	},
	{
		name:  "recall answers from memory",
		say:   "what did i tell you about the spare key",
		fast:  []string{`{"intent":"recall","reason":"asked"}`},
		smart: []string{"You told me the spare key is under the pot."},
		want:  "spare key",
		route: idRecall,
	},
	{
		name:  "forget drops the fact",
		say:   "forget about the spare key",
		fast:  []string{`{"intent":"forget","reason":"drop"}`},
		want:  "Forgotten",
		route: idForget,
		then: func(t *testing.T, f *fixture) {
			for _, fact := range f.j.mem.facts() {
				if strings.Contains(fact, "spare key") {
					t.Errorf("fact should have been forgotten: %v", f.j.mem.facts())
				}
			}
		},
	},
	{
		name:  "list adds an item",
		say:   "add milk to the shopping list",
		fast:  []string{`{"intent":"list","reason":"grocery"}`, `{"list":"shopping","op":"add","item":"milk"}`},
		want:  "Added milk",
		route: idList,
		then:  func(t *testing.T, f *fixture) { want(t, strings.Join(f.j.mem.list("shopping"), ","), "milk") },
	},
	{
		name:  "model transport error falls back to keywords",
		say:   "add eggs to the shopping list",
		fast:  []string{`{"intent":"list","reason":"grocery"}`},
		errs:  []error{nil, errors.New("dial tcp 127.0.0.1:1: connect: connection refused")},
		want:  "Added eggs",
		route: idList,
		then:  func(t *testing.T, f *fixture) { want(t, strings.Join(f.j.mem.list("shopping"), ","), "eggs") },
	},
	{
		name:  "briefing narrates the house",
		say:   "give me a briefing",
		fast:  []string{`{"intent":"briefing","reason":"summary"}`},
		smart: []string{"Everything in the house is calm right now."},
		want:  "calm",
		route: idBriefing,
	},
	{
		name:  "remind schedules a reminder",
		say:   "remind me to call mum in 20 minutes",
		fast:  []string{`{"intent":"remind","reason":"reminder"}`, `{"text":"call mum","minutes":20,"at":""}`},
		want:  "I'll remind you",
		route: idRemind,
		then: func(t *testing.T, f *fixture) {
			due := f.j.mem.pending()
			if len(due) == 0 || due[len(due)-1].Text != "call mum" {
				t.Errorf("reminder not scheduled: %v", due)
			}
		},
	},
	{
		name:  "the anthropic wire answers too",
		wire:  "anthropic",
		say:   "good evening",
		fast:  []string{`{"intent":"talk","reason":"chat"}`},
		smart: []string{"Good evening. The house is quiet."},
		want:  "Good evening",
		route: idTalk,
	},
}

func TestJarvisScenarios(t *testing.T) {
	f := newFixture(t)

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			f.fast.reset()
			f.smart.reset()
			f.fast.Script, f.fast.Errs = s.fast, s.errs
			f.smart.Script = s.smart

			var got string
			if s.wire == "anthropic" {
				got = askAnthropic(t, f.url, s.say)
			} else {
				got = askModel(t, f.url, "jarvis", s.say, nil)
			}
			want(t, got, s.want)

			// The diagnostics ring is a bounded buffer (500 events), so an
			// absolute index taken before the request can be stale by the time
			// it's read back. Subtests run sequentially, never concurrently, so
			// the most recent select.enter is unambiguously this request's.
			events := trace(t, f.url)
			routed := ""
			for _, e := range events {
				if e.Kind == "select.enter" {
					routed = e.Detail
				}
			}
			if routed != s.route {
				t.Errorf("last select.enter = %q, want %q", routed, s.route)
			}

			if s.then != nil {
				s.then(t, f)
			}
		})
	}
}

// TestEveryCapabilityHasAScenario fails the build the moment a capability is
// added to main.go's router without a matching row above — the mechanism
// docs/design-jarvis-e2e.md asks for instead of a discipline problem.
func TestEveryCapabilityHasAScenario(t *testing.T) {
	covered := map[string]bool{}
	for _, s := range scenarios {
		covered[s.route] = true
	}
	for id := range capabilities {
		if !covered[id] {
			t.Errorf("capability %q has no e2e scenario", id)
		}
	}
}

// TestEveryRoutineHasAScenario is routineIDs' counterpart: TestJarvisRoutines
// drives boot/morning/goodnight as served flows and TestJarvisSweepFiresForReal
// drives sweep through the real engine. If a fifth routine is ever added to
// routines() without updating both routineIDs and this map, this fails.
func TestEveryRoutineHasAScenario(t *testing.T) {
	covered := map[string]bool{idBoot: true, idSweep: true, idMorning: true, idGoodnight: true}
	for _, id := range routineIDs {
		if !covered[id] {
			t.Errorf("routine %q has no e2e scenario", id)
		}
	}
}

// --- streaming, memory, durable resume ---

func TestJarvisStreams(t *testing.T) {
	f := newFixture(t)
	f.fast.reset()
	f.smart.reset()
	f.fast.Script = []string{`{"intent":"talk","reason":"chat"}`}
	f.smart.Chunks = []string{"al", "pha"}

	body := `{"model":"jarvis","stream":true,"messages":[{"role":"user","content":"say something"}]}`
	resp, err := http.Post(f.url+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := string(raw)

	if n := strings.Count(out, "\"content\""); n < 2 {
		t.Fatalf("expected >=2 content deltas, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "[DONE]") {
		t.Fatalf("missing [DONE]:\n%s", out)
	}
}

func TestJarvisMemoryAcrossRequests(t *testing.T) {
	f := newFixture(t)

	f.fast.reset()
	f.smart.reset()
	f.fast.Script = []string{`{"intent":"remember","reason":"fact"}`, `{"fact":"the garage code is 7734"}`}
	got := askModel(t, f.url, "jarvis", "remember the garage code is 7734", nil)
	want(t, got, "garage code")

	f.fast.reset()
	f.smart.reset()
	f.fast.Script = []string{`{"intent":"recall","reason":"asked"}`}
	f.smart.Script = []string{"The garage code is 7734."}
	got = askModel(t, f.url, "jarvis", "what did i tell you about the garage", nil)
	want(t, got, "7734")

	var sent string
	for _, m := range f.smart.Got.Msgs {
		sent += m.Content + "\n"
	}
	want(t, sent, "7734")
}

func TestJarvisDurableResume(t *testing.T) {
	f := newFixture(t)
	before := len(f.j.mem.facts())

	f.fast.reset()
	f.smart.reset()
	f.fast.Script = []string{`{"intent":"remember","reason":"fact"}`, `{"fact":"the spa closes at 9pm"}`}
	headers := map[string]string{"X-Run-Id": "e2e-durable-1"}

	askModel(t, f.url, "jarvis", "remember the spa closes at 9pm", headers)
	if got := len(f.j.mem.facts()); got != before+1 {
		t.Fatalf("first post: facts = %d, want %d", got, before+1)
	}

	tBefore := len(trace(t, f.url))
	askModel(t, f.url, "jarvis", "remember the spa closes at 9pm", headers)
	if got := len(f.j.mem.facts()); got != before+1 {
		t.Fatalf("resumed post added a duplicate: facts = %d, want %d", got, before+1)
	}

	tail := trace(t, f.url)[tBefore:]
	cached := false
	for _, e := range tail {
		if e.Kind == "flow.cached" {
			cached = true
		}
	}
	if !cached {
		t.Errorf("expected flow.cached in trace tail: %+v", tail)
	}
}

// --- routines ---

func TestJarvisRoutines(t *testing.T) {
	f := newFixture(t)

	t.Run("boot", func(t *testing.T) {
		got := askModel(t, f.url, idBoot, "boot", nil)
		want(t, got, "Jarvis online")
		heard := f.w.heard()
		if len(heard) == 0 || !strings.Contains(heard[len(heard)-1], "Jarvis online") {
			t.Errorf("boot did not speak: %v", heard)
		}
	})

	t.Run("morning", func(t *testing.T) {
		f.smart.reset()
		f.smart.Script = []string{"Good morning. The house is calm and nothing is due."}
		got := askModel(t, f.url, idMorning, "morning", nil)
		want(t, got, "Good morning")
	})

	t.Run("goodnight", func(t *testing.T) {
		got := askModel(t, f.url, idGoodnight, "goodnight", nil)
		want(t, got, "Goodnight")
		devices := f.w.snapshot().Devices
		want(t, devices["front lock"], "locked")
		want(t, devices["porch light"], "off")
	})
}

// TestJarvisSweepFiresForReal drives the sweep body through the real engine —
// trigger registration, payload capture, engine enqueue, worker, durable body,
// notify sink — instead of invoking it directly as a served flow. bb.Once with
// a past time makes the dispatcher fire on its first loop iteration, so this
// needs no wall-clock cron wait (internal/serve/engine_test.go:37 relies on the
// same property).
func TestJarvisSweepFiresForReal(t *testing.T) {
	f := newFixture(t)

	due := time.Now().Add(-time.Minute)
	f.j.mem.schedule(context.Background(), "water the plants", due)

	bb.Trigger().Next(bb.Once(time.Now())).Next(f.j.sweep())

	probe := bb.NewFlow().WithAgent(bb.NewAgent().
		OnMessage(func(_ context.Context, turn bb.Turn, _ bb.ModelChat) error {
			turn.Reply("ok")
			return nil
		})).Next(bb.Respond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bb.Serve(ctx, probe, bb.Addr("127.0.0.1:0"), bb.Store(bb.MemStore()))

	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, s := range f.w.heard() {
			if strings.Contains(s, "Reminder: water the plants") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("sweep did not fire in time; heard: %v", f.w.heard())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// --- scope fence ---

// TestMarvisFrozen pins cmd/marvis-demo/main.go's content hash. marvis is the
// annotated goal-post spec pkg/bb was designed to satisfy (docs/PRODUCT.md
// §Reference brains), not runnable product code — work on jarvis, the e2e, or
// pkg/ must never touch it.
//
// ponytail: a content hash, not a semantic guard — marvis is package main with
// nothing exported to compare. It cannot stop an edit, only make one loud: if
// you legitimately changed marvis, update `want` here and say so in docs/LOG.md.
func TestMarvisFrozen(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "marvis-demo", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "d1f569f182d6284956279bb798d01b6a316864f8fbbd76687f958a3b9ca75dbf"
	if got := fmt.Sprintf("%x", sha256.Sum256(b)); got != want {
		t.Fatalf("cmd/marvis-demo/main.go changed (%s).\n"+
			"marvis is the frozen goal-post spec; jarvis/e2e/pkg work must not touch it.\n"+
			"If you meant to change marvis, update `want` in this test and say so in docs/LOG.md.", got)
	}
}
