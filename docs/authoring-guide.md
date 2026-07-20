# Authoring Guide

How to build a brain on top of `pkg/`. This is the developer-facing manual:
what the pieces are, how they fit, and worked examples. For *why* the API is
shaped this way, see `PRODUCT.md` and `IMPLEMENTATION.md` — this doc only
covers *how to use it*.

## Table of contents

1. [Mental model](#mental-model)
2. [Quickstart: hello brain](#quickstart-hello-brain)
3. [Concepts](#concepts)
4. [Node reference](#node-reference)
5. [Recipes](#recipes)
6. [Configuration](#configuration)
7. [Testing your brain](#testing-your-brain)
8. [Reference: the jarvis-demo brain](#reference-the-jarvis-demo-brain)

## Mental model

A **brain** is a Go program. There is no engine binary that loads a brain
from a file — your `main.go` imports `pkg/...`, builds a `brain.Brain` as a
plain Go value, and calls `serve.Run`. That call *is* the deployable server:
it serves an OpenAI-compatible and Anthropic-compatible API, so any existing
chat client, SDK, or IDE plugin can talk to your brain without knowing it's
not a real model.

```
your main.go
  └── imports pkg/brain, pkg/model, pkg/memory, pkg/serve, ...
        └── builds a brain.Brain{...} (a graph of Nodes, as data)
              └── serve.Run(ctx, brain)   ← this is your whole main()
```

Internally, a request is not "call one model and stream the answer" — it
runs a **pipeline**: a sequence of steps (**nodes**) that can prompt, call a
model, extract structured data, branch, run tools, recall/store memory,
fan out in parallel, and reply. The caller only ever sees one streamed
answer; the pipeline behind it can be arbitrarily elaborate.

## Quickstart: hello brain

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

	b := &brain.Brain{
		Name: "hello",
		Chat: []brain.Node{
			brain.Prompt("You are a friendly assistant. Be brief."),
			brain.Call("fast"),
			brain.Reply(),
		},
	}

	if err := serve.Run(ctx, b); err != nil {
		os.Exit(1)
	}
}
```

Run it:

```sh
BIG_BRAIN_MODELS="fast=gpt-4o-mini" \
BIG_BRAIN_UPSTREAM_BASE_URL="https://api.openai.com/v1" \
BIG_BRAIN_UPSTREAM_API_KEY="sk-..." \
go run .
```

Talk to it with any OpenAI client:

```sh
curl localhost:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"hello","messages":[{"role":"user","content":"hi"}]}'
```

That's a complete, servable brain in nine lines of pipeline. Everything past
this point is about the building blocks you can add to `Chat` — and about
`Pipelines`, the second kind of graph a brain can have, for work that
happens off the request path.

## Concepts

### Brain

```go
type Brain struct {
	Name      string
	Models    model.Models        // usually left nil; serve.Run binds it from config
	Chat      []Node              // runs once per incoming chat/messages request
	Pipelines map[string][]Node   // named graphs background jobs and triggers run by name
	Webhooks  map[string]string   // trigger name → pipeline, exposed at POST /triggers/{name}
	Crons     []cron.Cron         // schedules your brain runs on its own
}
```

`Chat` is the only pipeline the HTTP request itself runs. Everything else —
webhooks, crons, self-scheduled follow-ups — runs a *named* pipeline from
`Pipelines`, looked up by string. This is the one indirection that makes
initiative possible: a pipeline doesn't care whether it was triggered by a
person typing or a job firing at 3am.

`cron.Cron` (package `pkg/cron`) lives outside `pkg/brain` on purpose:
computing when a schedule next fires (interval math, daily-of-day
wraparound) is infrastructure, the same kind of thing as `pkg/model` or
`pkg/memory` — `Brain` declares crons the way it declares `Webhooks`, it
doesn't own the scheduling type or algorithm.

### Triggers

A trigger is whatever starts a pipeline run. There are three:

- **chat** — the API request itself (`Chat`). Not a special case internally;
  just the most common way a pipeline starts.
- **webhook** — an external system POSTs to `/triggers/{name}`; you declare
  the mapping in `Brain.Webhooks`.
- **cron** — a schedule you declare in `Brain.Crons` (`cron.Cron{Every: ...}`
  or `cron.Cron{Daily: "21:00", ...}`).

A fourth "trigger" is really a *capability*, not a wiring you declare
upfront: **self-installed**. Any node, mid-run, can call `brain.Go` or
`brain.GoAt` to schedule a pipeline to run later, on its own initiative.
That's how "I'll text you when it's done" becomes real instead of a lie the
model tells.

### Nodes and Run

A `Node` is one pipeline step:

```go
type Node interface {
	Run(ctx context.Context, r *Run) error
}
```

`*Run` is the shared, per-request state nodes read and write, in order:

```go
type Run struct {
	Messages []model.Message   // conversation so far — nodes may prepend/append
	Params   model.Params      // caller's sampling params (temperature, max tokens), read-only context
	Models   model.Models      // role → bound model
	Stream   <-chan model.Chunk
	Emit     func(model.Chunk) error // wired by serve; Reply() uses it
	Vars     map[string]any    // per-run scratch space, node-to-node
	Memory   memory.Memory
	Notify   notify.Channel
	Enqueue  func(context.Context, job.Job) error
	Replied  bool              // set once Reply() has streamed the answer
}
```

**Rule of thumb: never close over mutable state in a node function.** Nodes
are shared values — the same `brain.Func` closure runs for every concurrent
request. Anything specific to one run belongs on `*Run`, via `r.SetVar` /
`brain.Var[T]`, which are mutex-guarded (safe to call from `Parallel`
branches).

```go
r.SetVar("guest", "John")
guest, ok := brain.Var[string](r, "guest")
```

### Ad-hoc nodes: brain.Func

Most brain-specific logic (a tool call, a small transform) doesn't need its
own package — write it as a plain function and wrap it:

```go
func lookupWeather(ctx context.Context, r *brain.Run) error {
	r.SetVar("weather", "sunny, 24°C")
	return nil
}

// in the pipeline:
brain.Func(lookupWeather)
```

`brain.Func` is `http.HandlerFunc`-style: any `func(context.Context, *Run)
error` is a `Node`.

### Context & effects (ambient, not steps)

`Memory`, `Notify`, `Enqueue`, and `Models` are not things you wire into
each node — they're already sitting on `*Run`, injected by `serve.Run`
from configuration (or by your tests, if you build a `*Run` directly).
Nodes that need them just read `r.Memory`, `r.Notify`, etc.

Notice `Run` has no `Speaker`, no identity field of any kind, no notion of
"who is talking." The engine deliberately carries no such concept — it's a
pipeline runtime, not a household assistant. If your brain needs to know
who's calling, that's context *you* inject, the same way any node injects
anything: into `Vars`, via `Run.SetVar`/`brain.Var[T]`. See [Speaker
identity](#speaker-identity-story-3) for how `cmd/jarvis-demo` does this
with zero engine involvement beyond the generic `serve.WithPrepare` hook.

### Model roles

Brain code never names a provider or a specific model — it names a **role**:

```go
brain.Call("fast")
brain.Extract[intent]("smart", instruction, "intent")
```

Which model backs `"fast"` or `"smart"` is deployment config
(`BIG_BRAIN_MODELS=fast=gpt-4o-mini,smart=gpt-4o`), bound once at startup by
`serve.Run`. This is what makes the same brain code portable across
providers and across "swap the cheap model for a better one" without a
redeploy of logic.

### Dynamism — how much structure changes at runtime

The graph your brain runs is built once, in Go, at startup — but "the graph
never changes" is not true in practice. There's a ladder:

1. **Fixed graph, dynamic data.** The pipeline is constant; `Memory` (and
   any other state) changes what it does. This covers most "the brain
   learned something" cases — see [Memory](#memory) below.
2. **Dynamic construction.** Your Go code assembles or parameterizes
   subgraphs at runtime — e.g. building a `[]Node` slice conditionally
   before assigning it to `Brain.Chat`. Free: it's just Go.
3. **Self-installed triggers.** A node calls `brain.Go` / `brain.GoAt` to
   schedule a pipeline for later — the brain scheduling its own future.

There's no dynamism level where the brain rewrites its own pipeline code at
runtime — that's out of scope for this engine. If you find yourself wanting
that, prefer level 1: put the "if" in data (memory), not in a self-modifying
graph.

## Node reference

Grouped by what they do. All are in `pkg/brain` unless noted.

### Conversation shaping

| Node | Signature | What it does |
|---|---|---|
| `Prompt` | `Prompt(tmpl string) Node` | Renders `tmpl` (`text/template`, executed against `*Run`) and prepends it as a system message. |
| `RecallFacts` | `RecallFacts(notes ...string) Node` | Recalls facts via `r.Memory.Recall(ctx, query)` (`query` is the latest message's content) and injects them, tagged by when, as a system message, plus any `notes` you pass — domain guidance on how to weigh them. How many facts come back is the bound `Memory`'s own config, not a node parameter. Requires `r.Memory`. |

There is no `Situation` node — time is already ambient via the stdlib
(`time.Now()`), so the engine has nothing to inject; see [Time and
situation awareness](#time-and-situation-awareness-story-8) below.

### Model calls

| Node | Signature | What it does |
|---|---|---|
| `Call` | `Call(role model.Role) Node` | Streams a completion from the model bound to `role`, using `r.Messages` and the caller's `r.Params`. Sets `r.Stream`. |
| `Extract[T]` | `Extract[T any](role model.Role, instruction, key string) Node` | Asks the model for JSON matching `T`, strictly decodes (unknown fields rejected), makes exactly one repair round-trip on mismatch, stores the decoded `T` in `r.Vars[key]`. Caller sampling params are *not* passed — extraction is machinery, not the caller's answer. |

### Control flow

| Node | Signature | What it does |
|---|---|---|
| `Seq` | `Seq(nodes ...Node) Node` | Runs nodes in order as one node — use inside `If` branches. |
| `If` | `If(cond func(*Run) bool, then, els Node) Node` | Runs `then` if `cond(r)`, else `els`. Either may be `nil` (no-op). |
| `Parallel` | `Parallel(nodes ...Node) Node` | Fans children out concurrently, joins before continuing, joins their errors. Branches must write results via `r.SetVar` under distinct keys — never mutate `r.Messages` or `r.Stream` from inside a branch. |

### Reply

| Node | Signature | What it does |
|---|---|---|
| `Reply` | `Reply() Node` | Streams `r.Stream` to the caller via `r.Emit`, sets `r.Replied = true`. The HTTP response closes right after this fires — nodes placed *after* `Reply()` keep running detached from the request. This is the entire mechanism behind "on it, I'll text you when it's done." |

### Memory

| Node | Signature | What it does |
|---|---|---|
| `RecallFacts` | see above | Reads memory into context. |
| `Memorize` | `Memorize(role model.Role, instruction string) Node` | Asks the model, following `instruction`, whether the latest exchange contains durable facts worth keeping; if so, stores each via `r.Memory.Remember`. Ambient — the caller never says "remember this." Place after `Reply()` so it doesn't add latency to the answer. |

### Background work & notification

| Node | Signature | What it does |
|---|---|---|
| `Go` | `Go(pipeline string, payload func(*Run) map[string]any) Node` | Persists durable intent to run `pipeline` (from `Brain.Pipelines`) with a payload built from the current run, then returns immediately. Survives a crash — re-runs from the start (at-least-once), never silently lost. |
| `GoAt` | `GoAt(when func(*Run) time.Time, pipeline string, payload func(*Run) map[string]any) Node` | `Go`, deferred until `when(r)` — a trigger the brain installs for itself. Durable like any other job. |
| `Notify` | `Notify(tmpl string) Node` | Renders `tmpl` (`text/template` against `*Run`) and sends it out `r.Notify`. This is the brain *initiating* contact — no request is waiting for the answer. Address it to whoever it concerns from inside `tmpl` itself, e.g. `{{index .Vars "speaker"}}` if that's a key your brain sets. |

### Writing your own

Anything not covered above — an HTTP tool call, a database lookup, a custom
transform — is just `brain.Func(yourFunction)`. There is no tool framework
or plugin registry; a tool is Go code that reads/writes `*Run`.

## Recipes

### Persona chat (story 1)

```go
Chat: []brain.Node{
	brain.Prompt("You are Jarvis, warm and brief."),
	brain.Call("fast"),
	brain.Reply(),
},
```

### Ambient memory (story 2)

`RecallFacts` and `Memorize` are domain-neutral primitives. `memory.Fact`
holds only `Content` and `At` — no attribution field, on purpose: whose
fact it is, if that matters to your brain, is something you encode into
`Content` yourself. `Memory.Recall` takes a `query` (`RecallFacts` passes
the latest message's content) but no limit — how many facts a backing
keeps or returns is that backing's own config, set when it's constructed,
not a call-time parameter every implementation is forced to support. The
zero-setup `OpenFile(path, limit)` ignores query and returns the most
recent `limit` facts; `OpenLLM(path, model, limit)` (also in
`pkg/memory`) uses query, handing the whole log plus the query to a model
in one call to decide what's relevant, capped at `limit`. A second real
implementation with a genuinely different notion of "relevant" is what
keeps `Memory`'s interface honest about not mandating "most recent" — or
a fixed per-call cap. `serve.Run` builds the zero-setup `OpenFile` from
`BIG_BRAIN_MEMORY_LIMIT` (default 50); swap in `OpenLLM` yourself if you
build your own `serve.Handler` wiring instead of calling `serve.Run`. The
simplest version needs nothing beyond the wiring:

```go
const memorizeInstruction = `Does the user's latest message state durable
facts worth remembering long-term? List them, each self-contained, in third
person. Leave the list empty for small talk, questions, or one-off requests.`

Chat: []brain.Node{
	brain.Prompt(persona),
	brain.RecallFacts(),
	brain.Call("fast"),
	brain.Reply(),
	brain.Memorize("fast", memorizeInstruction),
},
```

If facts need per-speaker attribution (see [Speaker
identity](#speaker-identity-story-3) below), `Memorize`'s generic version
can't do that — it has no idea what a "speaker" is. Write your own small
node instead, reusing the generic pieces (`Extract`, `r.Memory`) directly.
This is what `cmd/jarvis-demo` does: its own `memorize` function extracts
facts exactly like `brain.Memorize` does internally, then tags each one
with `fmt.Sprintf("[%s] %s", speaker, content)` before calling
`r.Memory.Remember`. `RecallFacts` needs no change at all — it just prints
`Content` back out, tag and all.

### Speaker identity (story 3)

`pkg/` has no concept of a speaker, a caller, or identity of any kind —
`*Run` doesn't carry one. If your brain needs to know who's talking, that
is entirely your own mechanism, built from two fully generic pieces the
engine does provide:

- `serve.WithPrepare(fn func(*http.Request, *brain.Run))`, an option to
  `serve.Run` that calls `fn` once per incoming chat/messages request,
  before the pipeline runs, with the raw request. The engine has no
  opinion on what `fn` does with it.
- `Run.Vars` (via `run.SetVar` / `brain.Var[T]`), the same per-run scratch
  space every node already uses.

`cmd/jarvis-demo` combines them: `resolveSpeaker()` parses its own
`JARVIS_DEMO_SPEAKERS` env var (`key-dad=dad,key-kid=kid`) into a map once
at startup, and returns a `func(*http.Request, *brain.Run)` that looks up
the bearer token or `x-api-key` header and calls `run.SetVar("speaker",
name)`. That function is passed to `serve.Run(ctx, jarvis,
serve.WithPrepare(resolveSpeaker()))`. Every credential scheme choice
(bearer vs. `x-api-key`, a flat map vs. a database, the very key name
`"speaker"`) lives in `cmd/jarvis-demo/main.go` only — swap any of it, or
drop the concept entirely, without touching `pkg/`.

Downstream, the demo reads it back with a small `speakerOf(r *brain.Run)
string` helper wherever it needs it: to tell the model who it's talking to
(inside its own `situation` node — see [Time and situation
awareness](#time-and-situation-awareness-story-8)), to tag memorized
facts, and to carry identity into background jobs by adding `"speaker":
speakerOf(r)` to the payload map
`Go`/`GoAt` already take — `Run.Vars` is seeded from that payload when the
background pipeline runs, so no extra plumbing is needed there either.

### Intent → structured tool call (story 4)

```go
type intent struct {
	Action string `json:"action"`
	Guest  string `json:"guest"`
}

func isAddGuest(r *brain.Run) bool {
	it, ok := brain.Var[intent](r, "intent")
	return ok && it.Action == "add_guest" && it.Guest != ""
}

func addGuest(ctx context.Context, r *brain.Run) error {
	it, _ := brain.Var[intent](r, "intent")
	// call your real endpoint here
	r.SetVar("result", "added "+it.Guest)
	return nil
}

Chat: []brain.Node{
	brain.Prompt(persona),
	brain.Extract[intent]("fast", classifyInstruction, "intent"),
	brain.If(isAddGuest, brain.Func(addGuest), nil),
	brain.Call("fast"),
	brain.Reply(),
},
```

### Finish later, then reach out (story 5)

The reply promises work is happening; the actual work — and the
notification — run after the HTTP response has already closed:

```go
Chat: []brain.Node{
	brain.Prompt(persona),
	brain.Extract[intent]("fast", classifyInstruction, "intent"),
	brain.If(isAddGuest, brain.Seq(
		brain.Go("register-guest", func(r *brain.Run) map[string]any {
			it, _ := brain.Var[intent](r, "intent")
			return map[string]any{"guest": it.Guest}
		}),
		brain.Func(func(_ context.Context, r *brain.Run) error {
			r.Messages = append(r.Messages, model.Message{Role: "system",
				Content: "Tell the user you're on it and will text them when done."})
			return nil
		}),
	), nil),
	brain.Call("fast"),
	brain.Reply(),
},
Pipelines: map[string][]brain.Node{
	"register-guest": {
		brain.Func(registerGuestTool), // does the real work, r.SetVar("result", ...)
		brain.Notify(`{{index .Vars "result"}}`),
	},
},
```

`Go`'s payload becomes `r.Vars` when the background pipeline runs (see
`serve.runJob`), so the tool reads it with `brain.Var[string](r, "guest")`.

### Reacting to the world (story 6)

```go
Webhooks: map[string]string{"door": "unknown-face"},
Pipelines: map[string][]brain.Node{
	"unknown-face": {
		brain.RecallFacts(),
		brain.Func(describeEvent), // turns the webhook payload into a message
		brain.Extract[verdict]("fast", verdictInstruction, "verdict"),
		brain.If(openedOK,
			brain.Notify("Door opened: {{(index .Vars \"verdict\").Reason}}"),
			brain.Notify("Alert: unrecognized face.")),
	},
},
```

An external system does `POST /triggers/door` with a JSON body; the body
lands in `r.Vars["payload"]` when the pipeline runs. No person prompted this
run.

### Acting on schedule (story 7)

Config-defined, fires forever without any durability of its own:

```go
Crons: []cron.Cron{{Daily: "21:00", Pipeline: "nightly-review"}},
```

Self-installed, one-shot, decided by the brain mid-conversation:

```go
brain.GoAt(func(r *brain.Run) time.Time {
	return time.Now().Add(24 * time.Hour)
}, "party-prep", nil),
```

### Time and situation awareness (story 8)

No engine node for this — `time.Now()` is already ambient via the stdlib,
so there's nothing for `pkg/brain` to inject. Write a plain `brain.Func`:

```go
func situation(_ context.Context, r *brain.Run) error {
	now := time.Now()
	msg := fmt.Sprintf("Current situation: it is %s, %s (%s).\n"+
		"House quiet hours are 22:00 to 07:00; avoid noisy appliances then.\n",
		now.Format("Monday, 2 January 2006"), now.Format("15:04"), now.Format("MST"))
	r.Messages = append([]model.Message{{Role: "system", Content: msg}}, r.Messages...)
	return nil
}
```

Put `brain.Func(situation)` early in `Chat`, right after `Prompt`, so every
downstream node and the final reply sees it. This is exactly what
`cmd/jarvis-demo`'s `situation` function does — and since it's a plain
`Func`, it can freely read anything else on `Run` (speaker identity from
`Vars`, whatever else the brain tracks) in the same message, which the old
engine-owned `Situation` node never could.

### Choosing the right model for the job (story 9)

Not a node — a config decision. Reference cheap tasks and important ones
with different roles in your pipeline:

```go
brain.Extract[intent]("fast", classify, "intent"), // small-talk classification: cheap model
brain.Call("smart"),                                // the actual answer: better model
```

then bind them differently per deployment:
`BIG_BRAIN_MODELS=fast=gpt-4o-mini,smart=gpt-4o`.

### Multi-stage reasoning behind one reply (story 10)

```go
brain.Parallel(
	brain.Func(checkWeather),
	brain.Func(checkRSVPs),
),
brain.Func(func(_ context.Context, r *brain.Run) error {
	weather, _ := brain.Var[string](r, "weather")
	rsvps, _ := brain.Var[string](r, "rsvps")
	r.Messages = append(r.Messages, model.Message{Role: "system",
		Content: fmt.Sprintf("Weather: %s. RSVPs: %s. Weave both into one short reply.", weather, rsvps)})
	return nil
}),
brain.Call("fast"),
brain.Reply(),
```

The caller only ever sees the one streamed reply that follows.

## Configuration

Everything environment-dependent is 12-factor env vars, prefix `BIG_BRAIN_`.
None of this is brain code — it's how you deploy a given brain binary.

| Variable | Default | Meaning |
|---|---|---|
| `BIG_BRAIN_ENV` | `local` | `local` or `production`. |
| `BIG_BRAIN_HTTP_ADDR` | `:8080` | Listen address. |
| `BIG_BRAIN_LOG_LEVEL` | `info` | logrus level. |
| `BIG_BRAIN_LOG_FORMAT` | `text` | `text` or `json`. |
| `BIG_BRAIN_TELEMETRY_ENABLED` | `false` | Turns on OTLP metrics export. |
| `BIG_BRAIN_TELEMETRY_ENDPOINT` | `localhost:4317` | OTLP gRPC endpoint. |
| `BIG_BRAIN_UPSTREAM_BASE_URL` | provider default | Base URL of the OpenAI-compatible upstream backing your model roles. |
| `BIG_BRAIN_UPSTREAM_API_KEY` | — | Upstream API key. |
| `BIG_BRAIN_MODELS` | — | Role bindings, e.g. `fast=gpt-4o-mini,smart=gpt-4o`. |
| `BIG_BRAIN_MEMORY_PATH` | `memory.jsonl` | Zero-setup memory store file. |
| `BIG_BRAIN_MEMORY_LIMIT` | `50` | Facts `OpenFile` keeps in a single recall; 0 = no cap. Implementation-level config, not a `RecallFacts` parameter. |
| `BIG_BRAIN_JOBS_PATH` | `jobs.jsonl` | Zero-setup durable job log. |
| `BIG_BRAIN_NOTIFY_URL` | — | Outgoing webhook URL; empty logs notifications instead of sending them. |

Speaker identity has no engine config at all — `pkg/` has no concept of a
speaker. `cmd/jarvis-demo` reads its own `JARVIS_DEMO_SPEAKERS` with
`os.Getenv`, entirely outside the engine's config package; see [Speaker
identity](#speaker-identity-story-3) above.

`memory`, `job`, and `notify` are all interfaces (`pkg/memory.Memory`,
`pkg/job.Store`, `pkg/notify.Channel`); the JSONL files above are the
zero-setup default implementations `serve.Run` wires in. If you need a
different backing store, implement the interface and call `serve.Handler`
directly instead of `serve.Run` (see below).

## Testing your brain

You don't need real model calls or a running HTTP server to test pipeline
logic — build a `*brain.Run` directly and call nodes:

```go
func TestAddGuestBranch(t *testing.T) {
	r := &brain.Run{Vars: map[string]any{"intent": intent{Action: "add_guest", Guest: "John"}}}
	if !isAddGuest(r) {
		t.Fatal("expected add_guest branch to trigger")
	}
}
```

Every `pkg/` package that exports an interface ships a `mock.go`
(`model.Mock`, `memory.Mock`, `job.Mock`, `notify.Mock`) for exactly this —
inject them on `*Run` (`r.Models`, `r.Memory`, `r.Enqueue`, `r.Notify`)
instead of hitting a real provider or file. `model.Mock` supports scripted
multi-call sequences via `Script`/`Calls` for pipelines with more than one
model call.

For an end-to-end test without a real upstream, use `serve.Handler(brain,
deps)` directly — it's an `http.Handler`, testable with `httptest` the same
way you'd test any Go HTTP service, no `serve.Run` (and no real network)
required.

## Reference: the jarvis-demo brain

`cmd/jarvis-demo/main.go` is a complete, ~200-line brain exercising all ten
reference stories at once — persona, memory, speaker identity, tool calls,
background jobs with notification, webhook reactions, cron + self-installed
schedules, situational awareness, model roles, and parallel fan-out. It
imports nothing but `pkg/`, exactly like an external brain author would;
read it end to end as a worked example once you're past the recipes above.
