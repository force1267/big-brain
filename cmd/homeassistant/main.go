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

// addGuest is a tool: it registers the guest with the door-camera endpoint
// and tells the model what happened so the reply can confirm it. A tool is
// just a node body — plain Go, no framework.
func addGuest(ctx context.Context, r *brain.Run) error {
	it, _ := brain.Var[intent](r, "intent")
	body, err := json.Marshal(map[string]string{"name": it.Guest})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		os.Getenv("JARVIS_DOOR_URL"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	result := "the door camera accepted the guest"
	if err != nil {
		result = "the door camera could not be reached: " + err.Error()
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			result = fmt.Sprintf("the door camera refused (status %d)", resp.StatusCode)
		}
	}
	r.Messages = append(r.Messages, model.Message{Role: "system",
		Content: fmt.Sprintf("Tool result: tried to add %q to the guest list; %s. Confirm this to the user in one short sentence, in persona.", it.Guest, result)})
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
			brain.If(isAddGuest, brain.Func(addGuest), nil),
			brain.Call("fast"),
			brain.Reply(),
			// after Reply: the caller already has the answer; ambient
			// memory happens behind their back.
			brain.Memorize("fast"),
		},
	}

	if err := serve.Run(ctx, jarvis); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
