# big-brain

**An agent that disguises itself as a model.** big-brain wraps LLMs (and,
on the roadmap, voice/vision) behind standard OpenAI- and
Anthropic-compatible APIs — from the outside it's just another model
endpoint, so every existing chat UI, IDE plugin, and SDK is a free client.
Inside, a request runs through a **brain**: a pipeline of model calls,
memory, tools, and background work that makes it far more capable than one
model call.

It's a **library**, not a service you configure — you write a small Go
program that imports `pkg/`, describes your brain as data, and calls one
function to serve it.

## 60-second demo

This is a complete, working brain — a persona-driven assistant with ambient
memory. No config files, no YAML graph, no plugin system: it's a Go value.

```go
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/force1267/big-brain/pkg/brain"
	"github.com/force1267/big-brain/pkg/serve"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	jarvis := &brain.Brain{
		Name: "jarvis",
		Chat: []brain.Node{
			brain.Prompt("You are Jarvis, warm, brief, lightly witty."),
			brain.RecallFacts(50),   // remembers you, ambiently, across sessions
			brain.Call("fast"),      // "fast" is a role — provider/model bound by config
			brain.Reply(),           // streams the answer to the caller
			brain.Memorize("fast"),  // decides on its own what's worth remembering
		},
	}

	if err := serve.Run(ctx, jarvis); err != nil {
		os.Exit(1)
	}
}
```

```sh
BIG_BRAIN_MODELS="fast=gpt-4o-mini" \
BIG_BRAIN_UPSTREAM_BASE_URL="https://api.openai.com/v1" \
BIG_BRAIN_UPSTREAM_API_KEY="sk-..." \
go run .
```

Now point *any* OpenAI-compatible client at it — curl, the OpenAI SDK, an
IDE's "custom model" field, Open WebUI, whatever you already have:

```sh
curl localhost:8080/v1/chat/completions -H 'content-type: application/json' -d '{
  "model": "jarvis",
  "messages": [{"role": "user", "content": "we are vegetarian now, by the way"}]
}'

# weeks later, in a new session:
curl localhost:8080/v1/chat/completions -H 'content-type: application/json' -d '{
  "model": "jarvis",
  "messages": [{"role": "user", "content": "what should we make for dinner tonight?"}]
}'
# → suggests something meat-free, unprompted. It remembered.
```

That's the whole pitch: from the client's side, it's an OpenAI model. From
the inside, it's a pipeline that recalls facts, decides what to keep, and
picks the model that backs "fast" from your deployment config — none of
which the client has to know or configure.

## What makes it not-just-a-model

- **Memory** — the brain remembers across sessions, ambiently; it decides
  what's worth keeping, not the user.
- **Initiative** — a brain can reply "on it, I'll text you when it's done,"
  keep working after the HTTP response has closed, and reach out later via
  a webhook. Work also survives a crash — jobs are durable intent, re-run
  rather than lost.
- **Senses & hands** — vision/voice on the roadmap; tools today are just Go
  functions your pipeline calls.
- **Character** — persona, tone, mood — whatever your prompt and pipeline
  logic decide.
- **Reacting and scheduling** — a brain can wire up webhooks (an external
  system pings it) and cron schedules (it checks in on its own), on top of
  the usual chat trigger.

The reference implementation, `cmd/jarvis-demo`, is a home-assistant brain
that exercises all of the above — persona chat, ambient memory, per-speaker
identity, structured tool calls, background jobs with notification, webhook
reactions, self-scheduled follow-ups, and parallel multi-stage reasoning
behind one streamed reply — in about 200 lines, importing nothing but
`pkg/`.

## Build & run

```sh
go build ./...
go test ./...
go run ./cmd/cli serve          # runs the skeleton entrypoint
go run ./cmd/jarvis-demo        # runs the reference brain
```

Configuration is env-only (12-factor), prefix `BIG_BRAIN_` — e.g.
`BIG_BRAIN_MODELS=fast=gpt-4o-mini`, `BIG_BRAIN_MEMORY_PATH=memory.jsonl`,
`BIG_BRAIN_TELEMETRY_ENABLED=true`. Full variable reference in the guide
below.

## Documentation

- **[docs/authoring-guide.md](docs/authoring-guide.md)** — the developer
  guide for building your own brain: concepts, the full node reference,
  worked recipes for every reference story, configuration, and testing. If
  you're implementing a brain against `pkg/`, start there.
- **[PRODUCT.md](PRODUCT.md)** — what big-brain is and why, product
  decisions only.
- **[IMPLEMENTATION.md](IMPLEMENTATION.md)** — how the product decisions map
  onto the codebase.
- **[LOG.md](LOG.md)** — project history, session by session.
- **[CLAUDE.md](CLAUDE.md)** — rules AI agents follow in this repo.
- **[docs/research.md](docs/research.md)** — technology choices and
  rationale.

## Status

v1 functional scope is complete: all ten reference stories (persona chat,
memory, speaker identity, tool calls, background jobs + notification,
webhook triggers, cron + self-installed schedules, situational awareness,
model roles, parallel fan-out) pass end to end against both the OpenAI
chat-completions and Anthropic messages APIs, streaming included.
