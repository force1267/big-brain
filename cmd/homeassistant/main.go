// Command homeassistant is the reference brain. It deliberately uses only
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
	"syscall"

	"github.com/force1267/big-brain/pkg/brain"
	"github.com/force1267/big-brain/pkg/model"
	"github.com/force1267/big-brain/pkg/serve"
)

const persona = `You are Jarvis, the household assistant of a busy family.
You are warm, brief, and lightly witty. You answer as a helpful member of
the household, never as a generic AI.`

const classify = `Classify the user's latest message.
Actions: "add_guest" (they want someone added to the door guest list) or
"chat" (anything else). For add_guest, "guest" is the person's name;
otherwise leave it empty.`

// intent is the structured output of the classification stage (story 4).
type intent struct {
	Action string `json:"action"`
	Guest  string `json:"guest"`
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
	}
	return nil
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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	jarvis := &brain.Brain{
		Name: "jarvis",
		Chat: []brain.Node{
			brain.Prompt(persona),
			brain.RecallFacts(50),
			brain.Extract[intent]("fast", classify, "intent"),
			brain.If(isAddGuest, brain.Seq(
				brain.Go("register-guest", func(r *brain.Run) map[string]any {
					it, _ := brain.Var[intent](r, "intent")
					return map[string]any{"guest": it.Guest}
				}),
				brain.Func(queueGuest),
			), nil),
			brain.Call("fast"),
			brain.Reply(),
			// after Reply: the caller already has the answer; ambient
			// memory happens behind their back.
			brain.Memorize("fast"),
		},
		Pipelines: map[string][]brain.Node{
			// story 5: finish later, then reach out
			"register-guest": {
				brain.Func(registerGuest),
				brain.Notify(`{{index .Vars "result"}}`),
			},
		},
	}

	if err := serve.Run(ctx, jarvis); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
