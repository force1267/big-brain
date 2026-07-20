// Command jarvis-demo is the reference brain. It deliberately uses only
// pkg/ — it is executable documentation for external brain authors, so it
// must not reach anything they cannot (see IMPLEMENTATION.md).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/force1267/big-brain/pkg/brain"
	"github.com/force1267/big-brain/pkg/memory"
	"github.com/force1267/big-brain/pkg/model"
	"github.com/force1267/big-brain/pkg/serve"
)

const persona = `You are Jarvis, the household assistant of a busy family.
You are warm, brief, and lightly witty. You answer as a helpful member of
the household, never as a generic AI.`

const classify = `Classify the user's latest message.
Actions: "add_guest" (they want someone added to the door guest list),
"party" (they announce a party or gathering coming up), or "chat"
(anything else). For add_guest, "guest" is the person's name; otherwise
leave it empty.`

// recallNote is household-specific guidance on how to weigh recalled facts,
// passed to the generic brain.RecallFacts node.
const recallNote = `Facts tagged with a name belong to that person only; prefer the current speaker's and the shared household facts.`

// memorizeInstruction is household-flavored wording for what's worth
// remembering, passed to the generic brain.Memorize node.
const memorizeInstruction = `Does the user's latest message state durable
facts worth remembering long-term (preferences, appointments, dates,
relationships, standing household rules)? List them, each self-contained,
in third person, saying "the speaker" for the person talking (never "the
user"). Leave the list empty for small talk, questions, or one-off
requests.`

// intent is the structured output of the classification stage (story 4).
type intent struct {
	Action string `json:"action"`
	Guest  string `json:"guest"`
}

// face is the door camera's webhook payload (story 6).
type verdict struct {
	Open   bool   `json:"open"`
	Reason string `json:"reason"`
}

// registerGuest is the background tool (story 5): it calls the door-camera
// endpoint with the guest from the job payload and records the outcome for
// the Notify node. It never returns an error — this brain chooses to
// notify on failure rather than fail silently (see PRODUCT.md).
func registerGuest(ctx context.Context, r *brain.Run) error {
	guest, _ := brain.Var[string](r, "guest")
	body, _ := json.Marshal(map[string]string{"name": guest})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		os.Getenv("JARVIS_DOOR_URL"), bytes.NewReader(body))
	if err != nil {
		r.SetVar("result", fmt.Sprintf("I couldn't add %s to the guest list: %v", guest, err))
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	switch {
	case err != nil:
		r.SetVar("result", fmt.Sprintf("I couldn't reach the door camera to add %s — I'll need you to try again.", guest))
	case resp.StatusCode >= 300:
		resp.Body.Close()
		r.SetVar("result", fmt.Sprintf("The door camera refused %s (status %d).", guest, resp.StatusCode))
	default:
		resp.Body.Close()
		r.SetVar("result", fmt.Sprintf("Done — %s is on the guest list and the door camera will recognize them.", guest))
		// the guest list is a durable fact the unknown-face pipeline reads
		_ = r.Memory.Remember(ctx, memory.Fact{Speaker: r.Speaker,
			Content: fmt.Sprintf("%s is on the door guest list.", guest), At: time.Now()})
	}
	return nil
}

// describeFace turns the door camera's webhook payload into a message the
// verdict extraction can reason over, alongside recalled facts.
func describeFace(_ context.Context, r *brain.Run) error {
	payload, _ := brain.Var[map[string]any](r, "payload")
	desc, _ := json.Marshal(payload)
	r.Messages = append(r.Messages, model.Message{Role: "user",
		Content: "Door camera event, someone is at the door: " + string(desc)})
	return nil
}

// checkWeather and checkRSVPs are the story-10 fan-out tools. Each could
// call a real API; what matters is they run concurrently and merge into
// one reply. ponytail: canned results; swap for real endpoints anytime.
func checkWeather(_ context.Context, r *brain.Run) error {
	r.SetVar("weather", "clear skies expected, around 24°C")
	return nil
}

func checkRSVPs(ctx context.Context, r *brain.Run) error {
	facts, err := r.Memory.Recall(ctx, 0)
	if err != nil {
		return err
	}
	guests := 0
	for _, f := range facts {
		if strings.Contains(f.Content, "guest list") {
			guests++
		}
	}
	r.SetVar("rsvps", fmt.Sprintf("%d guests on the door list so far", guests))
	return nil
}

// partyAt schedules the brain's own reminder before the party (story 7).
// ponytail: fixed "tomorrow 09:00"; parsing dates out of chat is a model
// job for a later slice. JARVIS_PARTY_DELAY overrides for demos.
func partyAt(*brain.Run) time.Time {
	if d, err := time.ParseDuration(os.Getenv("JARVIS_PARTY_DELAY")); err == nil {
		return time.Now().Add(d)
	}
	tomorrow := time.Now().AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, tomorrow.Location())
}

// queueGuest persists the intent and primes the reply to promise a text.
func queueGuest(_ context.Context, r *brain.Run) error {
	it, _ := brain.Var[intent](r, "intent")
	r.Messages = append(r.Messages, model.Message{Role: "system",
		Content: fmt.Sprintf("You have queued adding %q to the guest list; it runs in the background. Tell the user, in persona and one short sentence, that you're on it and will text them when it's done.", it.Guest)})
	return nil
}

func isAddGuest(r *brain.Run) bool {
	it, ok := brain.Var[intent](r, "intent")
	return ok && it.Action == "add_guest" && it.Guest != ""
}

// resolveSpeaker parses JARVIS_DEMO_SPEAKERS ("key-dad=dad,key-kid=kid")
// into an API-key → speaker-name map once, then looks up the bearer token
// (OpenAI clients) or x-api-key (Anthropic clients) on each request. This
// whole scheme — env-var config, header choice, flat-map lookup — is a
// demo policy; brain.Brain.ResolveSpeaker lets any brain author plug in
// something else entirely (JWTs, mTLS, a database).
func resolveSpeaker() func(*http.Request) string {
	m := map[string]string{}
	for _, pair := range strings.Split(os.Getenv("JARVIS_DEMO_SPEAKERS"), ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(pair), "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return func(r *http.Request) string {
		if k := r.Header.Get("x-api-key"); k != "" {
			return m[k]
		}
		return m[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	jarvis := &brain.Brain{
		Name:           "jarvis",
		ResolveSpeaker: resolveSpeaker(),
		Chat: []brain.Node{
			brain.Prompt(persona),
			// story 8: time/situation awareness, no per-request plumbing
			brain.Situation("House quiet hours are 22:00 to 07:00; avoid noisy appliances then."),
			brain.RecallFacts(50, recallNote),
			brain.Extract[intent]("fast", classify, "intent"),
			brain.If(isAddGuest, brain.Seq(
				brain.Go("register-guest", func(r *brain.Run) map[string]any {
					it, _ := brain.Var[intent](r, "intent")
					return map[string]any{"guest": it.Guest}
				}),
				brain.Func(queueGuest),
			), nil),
			brain.If(func(r *brain.Run) bool {
				it, ok := brain.Var[intent](r, "intent")
				return ok && it.Action == "party"
			}, brain.Seq(
				// story 7: the brain installs a trigger for itself
				brain.GoAt(partyAt, "party-prep", nil),
				// story 10: fan out checks, join into one reply
				brain.Parallel(brain.Func(checkWeather), brain.Func(checkRSVPs)),
				brain.Func(func(_ context.Context, r *brain.Run) error {
					weather, _ := brain.Var[string](r, "weather")
					rsvps, _ := brain.Var[string](r, "rsvps")
					r.Messages = append(r.Messages, model.Message{Role: "system",
						Content: fmt.Sprintf("You scheduled yourself a party-prep reminder for tomorrow morning. Checks you ran: weather — %s; RSVPs — %s. Acknowledge the party plan in persona, one or two short sentences, weaving these in.", weather, rsvps)})
					return nil
				}),
			), nil),
			brain.Call("fast"),
			brain.Reply(),
			// after Reply: the caller already has the answer; ambient
			// memory happens behind their back.
			brain.Memorize("fast", memorizeInstruction),
		},
		Pipelines: map[string][]brain.Node{
			// story 5: finish later, then reach out
			"register-guest": {
				brain.Func(registerGuest),
				brain.Notify(`{{index .Vars "result"}}`),
			},
			// story 6: reacting to the world, no human prompted this run
			"unknown-face": {
				brain.RecallFacts(50, recallNote),
				brain.Func(describeFace),
				brain.Extract[verdict]("fast",
					`Someone is at the door. Based on the known facts (the guest
list), decide whether to open: open only for people on the guest list.
Give a one-sentence reason.`, "verdict"),
				brain.If(func(r *brain.Run) bool {
					v, ok := brain.Var[verdict](r, "verdict")
					return ok && v.Open
				},
					brain.Notify(`Door opened: {{(index .Vars "verdict").Reason}}`),
					brain.Notify(`Alert — someone unrecognized is at the door. {{(index .Vars "verdict").Reason}}`)),
			},
			// story 7: acting on schedule
			"party-prep": {
				brain.Notify("Reminder: the party is coming up — time to sort the shopping and tidy up."),
			},
			"nightly-review": {
				brain.RecallFacts(50, recallNote),
				brain.Notify("Nightly check-in done — I reviewed the household facts; all quiet."),
			},
		},
		Webhooks: map[string]string{"door": "unknown-face"},
		Crons:    []brain.Cron{{Daily: "21:00", Pipeline: "nightly-review"}},
	}

	if err := serve.Run(ctx, jarvis); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
