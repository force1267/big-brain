# big-brain

**An agent that disguises itself as a model.** big-brain wraps LLMs (and,
on the roadmap, voice/vision) behind standard OpenAI- and
Anthropic-compatible APIs — from the outside it's just another model
endpoint, so every existing chat UI, IDE plugin, and SDK is a free client.
Inside, a request runs through a **brain**: a tree of flows and agents —
model calls, memory, tools, routing — that makes it far more capable than
one model call.

It's a **library**, not a service you configure — you write a small Go
program that imports `pkg/bb`, assembles your brain as a tree of flows, and
calls one function to serve it.

## 60-second demo

A complete, working brain — a persona assistant that routes by intent and
acts on a tool. No config files, no YAML graph, no plugin system: it's Go.

```go
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/force1267/big-brain/pkg/bb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// A model backs the "chat" role — a real provider if BIG_BRAIN_API_KEY is
	// set, else a canned reply so this runs with no credentials.
	if os.Getenv("BIG_BRAIN_API_KEY") != "" {
		bb.RegisterModel(bb.NewModel().WithName("gpt-4o-mini"), "chat")
	} else {
		bb.RegisterModel(bb.FixedModel("At your service."), "chat")
	}

	// One agent, one flow: ask the model and reply.
	assistant := bb.NewAgent().
		WithModel(bb.NewModel("chat")).
		WithRole(bb.Role("You are Jarvis: warm, brief, lightly witty."))
	brain := bb.NewFlow().WithAgent(assistant)

	bb.Serve(ctx, brain) // OpenAI + Anthropic at :8080
}
```

Point *any* OpenAI-compatible client at it:

```sh
curl localhost:8080/v1/chat/completions -H 'content-type: application/json' -d '{
  "messages": [{"role": "user", "content": "hello there"}]
}'
```

From the client's side it's an OpenAI model. From the inside it's a flow you
grow — add a router that `Select`s capabilities, agents that call tools,
memory the brain keeps across turns — none of which the client has to know.

## What makes it not-just-a-model

- **Memory** — the brain remembers across turns and decides what to keep;
  memory is the brain's own state, woven into the persona.
- **Initiative** — a brain keeps acting past the reply: flows chained after
  `bb.Respond` (e.g. a `Notify` that reaches out). Durable execution means a
  crashed run resumes from where it stopped, not from the start.
- **Hands** — tools are just Go: an agent's `OnMessage` handler calls
  whatever you like and shapes the reply.
- **Character** — persona, tone, routing logic — whatever your agents decide.
- **Composition & concurrency** — `Select` routes to one capability;
  `All`/`One`/`Group` run several concurrently; `Checkpoint`/`Wait`
  coordinate agents; typed `Schema`/`Extract` give structured output.

## Reference brains

- **`cmd/jarvis-demo`** — a runnable smart-home assistant over a
  self-contained dummy world (sensors, devices, a notification sink): a
  keyword router into talk / remember / recall / house / briefing, memory
  across turns, concurrent sensor reads, a Notify flow after the reply, and
  durable execution. Runs with no API key.
- **`cmd/marvis-demo`** — the API "goal post": an intent router that
  classifies with a model + typed schema, then `Select`s a capability.

## Build & run

```sh
go build ./...
go test ./...
go run ./cmd/jarvis-demo    # smart-home brain: world on :8090, brain on :8080
```

Provider credentials come from the environment (12-factor), prefix
`BIG_BRAIN_` — e.g. `BIG_BRAIN_API_KEY=sk-...`,
`BIG_BRAIN_BASE_URL=https://api.openai.com/v1`, `BIG_BRAIN_MODEL=gpt-4o-mini`.
`BIG_BRAIN_DATA=<dir>` makes a brain's durability survive restarts.

## Documentation

- **[docs/authoring-guide.md](docs/authoring-guide.md)** — the developer
  guide: the flow/agent/turn model, models, routing, concurrency, memory,
  serving, durability, testing. Start here to build a brain.
- **[PRODUCT.md](PRODUCT.md)** — what big-brain is and why.
- **[IMPLEMENTATION.md](IMPLEMENTATION.md)** — the package architecture behind
  `pkg/bb`.
- **[LOG.md](LOG.md)** — project history, session by session.
- **[CLAUDE.md](CLAUDE.md)** — rules AI agents follow in this repo.

## Status

The `pkg/bb` framework is complete and green (much of it `-race` tested):
model roles, agents/turns, structured extraction, flows, `Select` routing,
`All`/`One`/`Group` concurrency, checkpoints, startup validation,
OpenAI/Anthropic serving with diagnostics, and durable checkpoint/resume.
Both reference brains run end to end. Deferred: true token streaming through
flows, live cross-agent chat visibility, and scheduled triggers (the durable
scheduler lives in `pkg/engine`).
