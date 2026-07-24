// Command jarvis-demo is a reference brain built entirely on pkg/bb — a home
// assistant for a smart house. It shows the bb surface working end to end:
//
//   - an intent router (a modelless agent that Selects a capability by keyword),
//   - capability flows chosen from a Select group: talk, remember, recall,
//     house (reads sensors / sets devices on the dummy world), briefing
//     (reads several sensors concurrently and summarises),
//   - memory the brain keeps across turns (author state, woven into the persona),
//   - a Notify flow after Respond, so every answer is echoed to the world's
//     notification sink,
//   - durable execution (bb.Store) and a jsonl trace of every flow.
//
// It is self-contained: a dummy "world" HTTP server (sensors, devices, a
// notification sink) runs in-process on :8090, so the demo needs nothing
// external. Run it:
//
//	go run ./cmd/jarvis-demo
//
// then point any OpenAI client at http://localhost:8080/v1. Try messages like
// "remember the wifi code is swordfish", "what did I tell you", "what's the
// temperature", "turn on the porch light", or "give me a briefing". With no
// credentials the persona replies are canned; set BIG_BRAIN_API_KEY (and
// optionally BIG_BRAIN_BASE_URL, BIG_BRAIN_MODEL) to back chat with a real
// model. Set BIG_BRAIN_DATA=<dir> for on-disk durability across restarts.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/force1267/big-brain/pkg/bb"
)

// Capability ids — the Select routing vocabulary. The router declares these as
// its exits; each capability flow claims one with WithId.
const (
	idTalk     = "talk"
	idRemember = "remember"
	idRecall   = "recall"
	idHouse    = "house"
	idBriefing = "briefing"
)

const worldAddr = "127.0.0.1:8090"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The dummy world the brain acts on: sensors, devices, a notify sink.
	world := startWorld(worldAddr)
	defer world.Shutdown(context.Background())

	// The model backing persona chat: a real provider if configured, else a
	// canned reply so the demo runs with no key.
	if key := os.Getenv("BIG_BRAIN_API_KEY"); key != "" {
		bb.RegisterModel(bb.NewModel().WithName(envOr("BIG_BRAIN_MODEL", "gpt-4o-mini")).WithTemprature(0.7), "chat")
	} else {
		bb.RegisterModel(bb.FixedModel("At your service. Anything else?"), "chat")
	}

	j := &jarvis{world: "http://" + worldAddr, mem: &memory{}, http: &http.Client{Timeout: 5 * time.Second}}

	// The brain: route by intent, run the chosen capability, respond, then echo
	// the answer to the world's notification sink. .Next after Respond is how a
	// brain keeps acting past the reply.
	brain := j.router().
		Next(bb.Select(j.talk(), j.remember(), j.recall(), j.house(), j.briefing())).
		Next(bb.Respond).
		Next(j.notify())

	// Durability: in-memory by default, on-disk when BIG_BRAIN_DATA is set.
	store := bb.MemStore()
	if dir := os.Getenv("BIG_BRAIN_DATA"); dir != "" {
		fs, err := bb.FileStore(dir)
		if err != nil {
			fatal(err)
		}
		store = fs
	}

	fmt.Fprintln(os.Stderr, "jarvis-demo: world on :8090, brain on :8080 (OpenAI at /v1/chat/completions); trace on stdout")
	err := bb.Serve(ctx, brain,
		bb.Addr(":8080"),
		bb.Trace(bb.JSONL(os.Stdout)),
		bb.Store(store),
	)
	if err != nil {
		fatal(err)
	}
}

// jarvis holds the brain's dependencies: the world it acts on and the memory it
// keeps. Flow constructors hang off it so they can close over both.
type jarvis struct {
	world string
	mem   *memory
	http  *http.Client
}

// router is a modelless agent: it reads the latest message and Selects a
// capability by keyword. Declaring its exits lets Serve verify the wiring at
// startup.
func (j *jarvis) router() bb.Flow {
	route := bb.NewAgent().
		Selects(idTalk, idRemember, idRecall, idHouse, idBriefing).
		OnMessage(func(_ context.Context, turn bb.Turn) error {
			switch msg := strings.ToLower(turn.Last().Content); {
			case strings.Contains(msg, "remember"):
				turn.Select(idRemember)
			case strings.Contains(msg, "recall"), strings.Contains(msg, "what did"), strings.Contains(msg, "what do you know"):
				turn.Select(idRecall)
			case strings.Contains(msg, "briefing"), strings.Contains(msg, "status"):
				turn.Select(idBriefing)
			case containsAny(msg, "temperature", "temp", "door", "humidity", "light", "turn on", "turn off"):
				turn.Select(idHouse)
			default:
				turn.Select(idTalk)
			}
			return nil
		})
	return bb.NewFlow().WithAgent(route)
}

// talk is the persona capability: it weaves what the brain remembers into a
// system note, then answers with the chat model.
func (j *jarvis) talk() bb.Flow {
	a := bb.NewAgent().
		WithModel(bb.NewModel("chat")).
		WithRole(bb.Role("You are Jarvis, a warm but terse home assistant.")).
		OnMessage(func(_ context.Context, turn bb.Turn) error {
			if facts := j.mem.recall(); len(facts) > 0 {
				turn.Add(bb.NewMessage("You remember: " + strings.Join(facts, "; ")).As("system"))
			}
			turn.Add(turn.Last())
			reply, err := turn.Ask()
			if err != nil {
				return err
			}
			turn.Reply(reply.ReadAll())
			return nil
		})
	return bb.NewFlow().WithId(idTalk).WithAgent(a)
}

// remember stores a fact from the user's message. No model needed.
func (j *jarvis) remember() bb.Flow {
	a := bb.NewAgent().OnMessage(func(_ context.Context, turn bb.Turn) error {
		fact := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(turn.Last().Content), "remember"))
		fact = strings.TrimLeft(fact, " :,-")
		if fact == "" {
			turn.Reply("Remember what, exactly?")
			return nil
		}
		j.mem.remember(fact)
		turn.Reply("Got it — I'll remember that " + fact + ".")
		return nil
	})
	return bb.NewFlow().WithId(idRemember).WithAgent(a)
}

// recall reports what the brain has been told.
func (j *jarvis) recall() bb.Flow {
	a := bb.NewAgent().OnMessage(func(_ context.Context, turn bb.Turn) error {
		facts := j.mem.recall()
		if len(facts) == 0 {
			turn.Reply("You haven't told me anything to remember yet.")
			return nil
		}
		turn.Reply("Here's what I know: " + strings.Join(facts, "; ") + ".")
		return nil
	})
	return bb.NewFlow().WithId(idRecall).WithAgent(a)
}

// house acts on the dummy world: set a device, or read a sensor.
func (j *jarvis) house() bb.Flow {
	a := bb.NewAgent().OnMessage(func(ctx context.Context, turn bb.Turn) error {
		msg := strings.ToLower(turn.Last().Content)
		switch {
		case strings.Contains(msg, "turn on"), strings.Contains(msg, "turn off"):
			device := deviceFrom(msg)
			state := "on"
			if strings.Contains(msg, "off") {
				state = "off"
			}
			if err := j.set(ctx, device, state); err != nil {
				return err
			}
			turn.Reply(fmt.Sprintf("Done — %s is now %s.", device, state))
		default:
			sensor := sensorFrom(msg)
			reading, err := j.read(ctx, sensor)
			if err != nil {
				return err
			}
			turn.Reply(fmt.Sprintf("The %s reads %s.", sensor, reading))
		}
		return nil
	})
	return bb.NewFlow().WithId(idHouse).WithAgent(a)
}

// briefing reads several sensors concurrently and summarises them in one reply.
func (j *jarvis) briefing() bb.Flow {
	a := bb.NewAgent().OnMessage(func(ctx context.Context, turn bb.Turn) error {
		sensors := []string{"temperature", "humidity", "door"}
		readings := make([]string, len(sensors))
		var wg sync.WaitGroup
		for i, s := range sensors {
			wg.Add(1)
			go func(i int, s string) {
				defer wg.Done()
				r, err := j.read(ctx, s)
				if err != nil {
					r = "unavailable"
				}
				readings[i] = fmt.Sprintf("%s %s", s, r)
			}(i, s)
		}
		wg.Wait()
		turn.Reply("Briefing — " + strings.Join(readings, ", ") + ".")
		return nil
	})
	return bb.NewFlow().WithId(idBriefing).WithAgent(a)
}

// notify echoes the final reply to the world's notification sink. It sits after
// Respond, so it runs once the user has their answer.
func (j *jarvis) notify() bb.Flow {
	return bb.Notify(func(ctx context.Context, text string) error {
		return j.post(ctx, "/notify", `{"text":`+quote(text)+`}`)
	})
}

// --- dummy world client helpers ---

func (j *jarvis) read(ctx context.Context, sensor string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", j.world+"/sensor/"+sensor, nil)
	resp, err := j.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b)), nil
}

func (j *jarvis) set(ctx context.Context, device, state string) error {
	return j.post(ctx, "/device/"+device, `{"state":`+quote(state)+`}`)
}

func (j *jarvis) post(ctx context.Context, path, body string) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", j.world+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := j.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// --- memory: the brain's own state, a simple concurrent list of facts ---

type memory struct {
	mu    sync.Mutex
	facts []string
}

func (m *memory) remember(fact string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.facts = append(m.facts, fact)
}

func (m *memory) recall() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.facts...)
}

// --- the dummy world server ---

func startWorld(addr string) *http.Server {
	sensors := map[string]string{"temperature": "21°C", "humidity": "45%", "door": "closed"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sensor/{name}", func(w http.ResponseWriter, r *http.Request) {
		v, ok := sensors[r.PathValue("name")]
		if !ok {
			v = "unknown"
		}
		io.WriteString(w, v)
	})
	mux.HandleFunc("POST /device/{name}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(os.Stderr, "🏠 world: device %s <- %s\n", r.PathValue("name"), bytes.TrimSpace(body))
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("POST /notify", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(os.Stderr, "🔔 world: notify %s\n", bytes.TrimSpace(body))
		io.WriteString(w, "ok")
	})
	srv := &http.Server{Addr: addr, Handler: mux}
	go srv.ListenAndServe()
	return srv
}

// --- small helpers ---

func sensorFrom(msg string) string {
	switch {
	case strings.Contains(msg, "humid"):
		return "humidity"
	case strings.Contains(msg, "door"):
		return "door"
	default:
		return "temperature"
	}
}

func deviceFrom(msg string) string {
	for _, d := range []string{"porch light", "light", "heater", "lock", "fan"} {
		if strings.Contains(msg, d) {
			return d
		}
	}
	return "light"
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func quote(s string) string { return `"` + strings.NewReplacer(`"`, `\"`, "\n", `\n`).Replace(s) + `"` }

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "jarvis-demo:", err)
	os.Exit(1)
}
