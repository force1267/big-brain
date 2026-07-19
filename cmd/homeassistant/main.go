// Command homeassistant is the reference brain. It deliberately uses only
// pkg/ — it is executable documentation for external brain authors, so it
// must not reach anything they cannot (see IMPLEMENTATION.md).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/force1267/big-brain/pkg/brain"
	"github.com/force1267/big-brain/pkg/serve"
)

const persona = `You are Jarvis, the household assistant of a busy family.
You are warm, brief, and lightly witty. You answer as a helpful member of
the household, never as a generic AI.`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	jarvis := &brain.Brain{
		Name: "jarvis",
		Chat: []brain.Node{
			brain.Prompt(persona),
			brain.Call("fast"),
			brain.Reply(),
		},
	}

	if err := serve.Run(ctx, jarvis); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
