package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/force1267/big-brain/pkg/bb"
)

// The demo runs with no provider, so keyword understanding is a load-bearing
// path, not a fallback nobody takes. These are its checks.

func TestGuessRoutes(t *testing.T) {
	for msg, want := range map[string]string{
		"remind me to call mum in 20 minutes": idRemind,
		"remember the wifi code is swordfish": idRemember,
		"forget the wifi code":                idForget,
		"what did i tell you about the cat":   idRecall,
		"add milk to the shopping list":       idList,
		"turn off the porch light":            idHouse,
		"give me a briefing":                  idBriefing,
		"how are you today":                   idTalk,
	} {
		if got := guess(msg); got != want {
			t.Errorf("guess(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestValidFallsBackOnJunk(t *testing.T) {
	if got := valid("  HOUSE ", "whatever"); got != idHouse {
		t.Errorf("valid normalises: got %q", got)
	}
	if got := valid("nonsense", "turn off the fan"); got != idHouse {
		t.Errorf("valid falls back to keywords: got %q", got)
	}
}

func TestGuessCommand(t *testing.T) {
	cases := []struct {
		msg  string
		want command
	}{
		{"turn off the porch light", command{Device: "porch light", State: "off"}},
		{"turn on the heater", command{Device: "heater", State: "on"}},
		{"lock the front door", command{Device: "front lock", State: "locked"}},
		{"set the thermostat to 23", command{Device: "thermostat", State: "23"}},
		{"how humid is it", command{Sensor: "humidity"}},
		{"any motion?", command{Sensor: "motion"}},
	}
	for _, c := range cases {
		if got := guessCommand(c.msg); got != c.want {
			t.Errorf("guessCommand(%q) = %+v, want %+v", c.msg, got, c.want)
		}
	}
}

func TestGuessListOp(t *testing.T) {
	if got := guessListOp("add milk to the shopping list"); got != (listOp{List: "shopping", Op: "add", Item: "milk"}) {
		t.Errorf("add: %+v", got)
	}
	if got := guessListOp("remove milk from the shopping list"); got.Op != "remove" || got.Item == "" {
		t.Errorf("remove: %+v", got)
	}
	if got := guessListOp("what's on my todo list"); got != (listOp{List: "todo", Op: "show"}) {
		t.Errorf("show: %+v", got)
	}
}

func TestGuessReminderTimes(t *testing.T) {
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)

	text, due := guessReminder("remind me to take the bread out in 25 minutes", now)
	if text != "take the bread out" || !due.Equal(now.Add(25*time.Minute)) {
		t.Errorf("relative: %q at %v", text, due)
	}

	_, due = guessReminder("remind me to call the dentist at 14:30", now)
	if due.Hour() != 14 || due.Minute() != 30 || due.Day() != 25 {
		t.Errorf("clock: %v", due)
	}

	// A clock time already past today means tomorrow, not the past.
	_, due = guessReminder("remind me at 8:00 to leave", now)
	if due.Day() != 26 {
		t.Errorf("past clock time should roll over: %v", due)
	}
}

func TestResolveSpec(t *testing.T) {
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	if when, ok := resolve(reminderSpec{Minutes: 5}, now); !ok || !when.Equal(now.Add(5*time.Minute)) {
		t.Errorf("minutes: %v %v", when, ok)
	}
	if when, ok := resolve(reminderSpec{At: "07:15"}, now); !ok || when.Hour() != 7 || when.Day() != 26 {
		t.Errorf("at: %v %v", when, ok)
	}
	if _, ok := resolve(reminderSpec{}, now); ok {
		t.Error("an empty spec must not resolve")
	}
}

func TestMemoryPersistsAndFiresOnce(t *testing.T) {
	ctx := context.Background()
	store := bb.MemStore()

	m := openMemory(ctx, store)
	m.remember(ctx, "the wifi code is swordfish")
	m.remember(ctx, "the wifi code is swordfish") // duplicate
	m.listAdd(ctx, "shopping", "milk")
	due := time.Now().Add(-time.Minute)
	m.schedule(ctx, "call mum", due)

	// A second reader over the same store sees all of it: that is the restart.
	m2 := openMemory(ctx, store)
	if got := m2.facts(); len(got) != 1 {
		t.Fatalf("facts after reload: %v", got)
	}
	if got := m2.list("shopping"); len(got) != 1 || got[0] != "milk" {
		t.Fatalf("list after reload: %v", got)
	}
	if fired := m2.due(ctx, time.Now()); len(fired) != 1 || fired[0].Text != "call mum" {
		t.Fatalf("first sweep: %v", fired)
	}
	if fired := m2.due(ctx, time.Now()); len(fired) != 0 {
		t.Fatalf("a fired reminder must not fire twice: %v", fired)
	}
	if n := m2.forget(ctx, "wifi"); n != 1 || len(m2.facts()) != 0 {
		t.Fatalf("forget: %d %v", n, m2.facts())
	}
}

// TestBrainEndToEnd drives a real request through the assembled brain against a
// real dummy house: the reply must be the house's actual state, and the house
// must have been changed.
func TestBrainEndToEnd(t *testing.T) {
	bb.WithModel(bb.FixedModel("At your service.")).WithTag(mSmart, mFast)

	w := startWorld("127.0.0.1:18091")
	defer w.shutdown()

	j := &jarvis{
		house: &client{base: "http://127.0.0.1:18091", http: &http.Client{Timeout: 2 * time.Second}},
		mem:   openMemory(context.Background(), bb.MemStore()),
	}
	brain := j.route().
		Next(bb.Select(j.talk(), j.remember(), j.forget(), j.recall(), j.lists(), j.control(), j.briefing(), j.remind())).
		Next(bb.Respond)

	h, err := bb.Handler(brain)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	if got := ask(t, srv.URL, "turn on the heater"); !strings.Contains(got, "heater is on") {
		t.Errorf("control reply = %q", got)
	}
	if state := w.snapshot().Devices["heater"]; state != "on" {
		t.Errorf("the house was not actually changed: heater = %q", state)
	}
	if got := ask(t, srv.URL, "remember the spare key is under the pot"); !strings.Contains(got, "spare key") {
		t.Errorf("remember reply = %q", got)
	}
	if facts := j.mem.facts(); len(facts) != 1 {
		t.Errorf("fact not kept: %v", facts)
	}
}

func ask(t *testing.T, base, msg string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"model":    "jarvis",
		"messages": []map[string]string{{"role": "user", "content": msg}},
	})
	resp, err := http.Post(base+"/v1/chat/completions", "application/json", strings.NewReader(string(body)))
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
