# Authoring guide

How to write a brain with `pkg/bb`. A brain is a Go program that assembles a
tree of **flows** and serves it. This guide moves with the code; if it disagrees
with `pkg/bb`, the code wins. Two complete examples live in `cmd/marvis-demo`
(intent routing with a model + schema, annotated line by line) and
`cmd/jarvis-demo` (a runnable smart-home brain: schema routing with keyword
fallbacks, durable memory and lists, a `Group`-based briefing, reminders, and
cron routines — it runs with no API key).

## The mental model

- A **Flow** runs one or more **agents** over an incoming chat, collects their
  replies, and hands the result to the next flow.
- Flows **compose**: a group of flows is itself a flow (`Select`, `All`, `One`,
  `Group`), and `Next` chains them.
- An **Agent** is *build-time* configuration (model, role, schema, handler).
- A **Turn** is the agent *live* on one message — what an `OnMessage` handler
  receives. It can `Add`/`Ask`/`Reply`/`Select`; it cannot reconfigure the
  agent. (The compiler enforces this split.)

The smallest brain:

```go
func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    bb.WithModel(bb.NewModel().WithName("gpt-4o-mini")).WithTag("chat")

    agent := bb.NewAgent(). // no model: inherits the flow's / the default
        WithRole(bb.Role("You are a terse assistant."))
    flow := bb.NewFlow().WithModel(bb.NewModel("chat")).WithAgent(agent)

    bb.Serve(ctx, flow) // OpenAI + Anthropic at :8080
}
```

A default (no-`OnMessage`) agent just asks the model with the incoming chat and
replies. That is the whole walking skeleton.

## Models

```go
bb.WithModel(bb.NewModel().WithName("gpt-4o-mini").WithTemprature(0.5)).WithTag("fast", "cheap")
m := bb.NewModel("fast")                 // seeded from the registered model
m2 := bb.NewModel("fast").WithTemprature(0.9) // overrides just this use
inline := bb.NewModel().WithName("gpt-4o") // no registry, built inline
demo := bb.FixedModel("canned reply")    // no provider — for demos/tests
```

`bb.WithModel(m)` registers `m` and returns a handle; `.WithTag(…)` binds it to
lookup tags. `bb.NewModel(tags…)` is always a builder: with no tags it starts
blank, with tags it is seeded from the registered model and stays overridable.
Provider credentials come from the environment (`BIG_BRAIN_API_KEY`,
`BIG_BRAIN_BASE_URL`). Flow code names a *role*; deployment decides which
provider backs it.

**Which model an agent asks** is resolved along a ladder, first match wins:

1. `agent.WithModel(m)` — the agent's own model.
2. `flow.WithModel(m)` — the model set on the flow the agent runs in.
3. `bb.WithDefaultModel(m)` — an explicit process default.
4. the first `bb.WithModel(…)` registered — the implicit default.

So no agent is ever truly model-less; leaving `WithModel` off just means "use
whatever the flow, then the default, provides". A default (no-`OnMessage`) agent
that resolves to no model at any rung is a startup error from `bb.Serve`.

## Agents and turns

```go
agent := bb.NewAgent().
    WithModel(bb.NewModel("fast")).
    WithRole(bb.Role("You are Jarvis.")).
    WithSchema(bb.Schema[intent]()).      // optional: expect structured output
    Selects(idTalk, idHouse).             // optional: declare Select exits
    OnMessage(func(ctx context.Context, turn bb.Turn) error {
        turn.Add(turn.Last())             // add the latest incoming message
        reply, err := turn.Ask()          // send role + added chat to the model
        if err != nil {
            return err                    // schema mismatch + transport surface here
        }
        turn.Reply(reply.ReadAll())       // add an assistant message to the flow
        return nil
    })
```

- `turn.Messages` is the incoming conversation; `turn.Last()` is the latest.
- `turn.Add(msgs…)` chooses what the next `Ask` sends; `turn.AskWith(msgs…)` is
  `Add`+`Ask`.
- `turn.Reply(text)` appends output to the flow's chat (zero or many times); it
  does **not** go to the model.
- `reply.ReadAll()` / `Read()` / `Stream()` read the answer; `bb.Extract[T](reply)`
  decodes it into a schema type.
- `ctx` is this turn's context; it is done when the handler returns. Pass it to
  any I/O you do so cancellation is respected.
- `turn.Request()` is the client's request as context: the sampling parameters
  it sent (`model`, `temperature`, `max_tokens`, …). They are **not** applied to
  your agent automatically — they are an input for the handler to read and act on.

**Request parameters.** The engine never silently honors a client's sampling
knobs; it hands them to the flow so *you* decide. A handler can honor, clamp,
ignore, or branch on them:

```go
OnMessage(func(ctx context.Context, turn bb.Turn) error {
    req := turn.Request()
    if n := req.MaxTokens; n != nil && *n < 64 {
        turn.Add(bb.NewMessage("Answer in one short sentence.").As("system"))
    }
    // req.Model is the model id the client asked for (also what selects a
    // named flow at the serving layer); req.Temperature is theirs to weigh in.
    turn.Add(turn.Last())
    reply, err := turn.Ask() // asks with the agent's own model config, not req's
    // ...
})
```

The agent's own `WithModel` config is what `Ask` uses; the request params are
context, never an override — so a brain stays a brain, not a raw model whose
behavior the caller dictates.

**Structured output.** `WithSchema(bb.Schema[T]())` tells the agent to expect
JSON matching `T`; `Ask` validates the reply against it (a mismatch is the error
from `Ask`), and `bb.Extract[T](reply)` returns a typed `T`:

```go
type intent struct {
    Intent string `json:"intent" enum:"talk,house,remember" doc:"the chosen capability"`
    Reason string `json:"reason"`
}
// ...
reply, err := turn.Ask()
if err != nil { return err }
it := bb.Extract[intent](reply)
turn.Select(it.Intent)
```

Struct tags shape the schema sent to the model: `doc:"…"` becomes a field
description, and `enum:"a,b,c"` constrains a field to a fixed set — handy for a
router that must pick one of a known list of ids.

`bb.Extract` is a free function (not `reply.Extract[T]()`) because Go forbids
type parameters on methods — the same shape as `bb.Schema[T]()`.

## Routing with Select

`Select` groups flows so an upstream agent picks one by id:

```go
brain := router.Next(bb.Select(talk, remember, house)).Next(bb.Respond)
```

- Each Select member must set an id with `WithId`; a member without one is
  ignored (with a warning).
- An agent picks a member with `turn.Select(id)`. An unknown id is a **loud
  error** at request time, never a silent misroute.
- Declaring an agent's exits with `Selects(id…)` adds a **startup** check
  (`bb.Serve`/`bb.Handler` verifies every declared exit is a group member)
  before any request runs. It is optional — declare it when you want the
  boot-time guarantee.
- Within one agent, the last `Select` wins (program order). Across *concurrent*
  agents, two different selects is a loud `error`, not a race; the same id is
  fine.

## Chaining and continuing past the reply

`a.Next(b).Next(c)` runs a→b→c, threading the chat. `bb.Respond` is the prebuilt
flow that marks the last message as the user's reply; you can chain flows after
it to keep acting:

```go
brain := router.Next(bb.Select(caps...)).Next(bb.Respond).Next(notify)
```

`bb.Notify(send)` is a prebuilt outgoing flow — it sends the chat's last message
to `send` and passes the chat through:

```go
notify := bb.Notify(func(ctx context.Context, text string) error {
    return postToWebhook(ctx, text)
})
```

## Concurrency

A flow with several agents runs them concurrently; they can coordinate with a
checkpoint:

```go
cp := bb.NewCheckpoint()
recognizer := bb.NewAgent().OnMessage(func(ctx context.Context, t bb.Turn) error {
    t.Reply(classify(t.Last().Content)); bb.Reached(cp); return nil
})
guard := bb.NewAgent().OnMessage(func(ctx context.Context, t bb.Turn) error {
    if err := bb.Wait(ctx, cp); err != nil { return err } // wait for recognizer
    // ...
    return nil
})
flow := bb.NewFlow().WithAgent(recognizer, guard)
```

Group strategies over member flows:

- `bb.All(a, b, …)` — run all, merge every reply, end when all end.
- `bb.One(a, b, …)` — first to finish wins, the rest are cancelled.
- `bb.Group(a, b, …)` — run all over one **live shared chat**: a member's reply
  is immediately visible to the others (a member's next `Ask`, or `turn.Last()`,
  sees it). Order members with `Checkpoint`/`Wait` when one must see another's
  contribution first.

## Memory

Memory is the brain's own state — bb does not impose a store. Keep facts in a
map, or in a KV via `bb.MemStore()` / `bb.FileStore(dir)` (a `Get`/`Put`
backend), and read/write it inside a handler, weaving recalled facts into the
persona:

```go
if facts := mem.recall(); len(facts) > 0 {
    turn.Add(bb.NewMessage("You remember: " + strings.Join(facts, "; ")).As("system"))
}
```

See `cmd/jarvis-demo` for a complete memory + tools + briefing brain.

## Streaming to the client

The user can see tokens as the model types them — but only at the **terminal**
boundary: the flow whose reply is the answer (the one before `bb.Respond`, or
the last in the chain). Everywhere upstream, flows hand each other *complete*
messages, because that is what durable checkpointing needs. So `State` always
carries whole messages; streaming is a parallel live tee that exists only at the
end.

A **default agent (no `OnMessage`) streams automatically** when it is terminal
and the client asked for it — nothing to write. To stream from a handler, tee
the model's live output into the outgoing channel `turn.Stream()` hands you:

```go
OnMessage(func(ctx context.Context, turn bb.Turn) error {
    turn.Add(turn.Last())
    reply, err := turn.Ask()
    if err != nil {
        return err
    }
    if out, ok := turn.Stream(); ok { // ok only when terminal + client wants SSE
        for tok := range reply.Stream() { // live model tokens
            out <- tok                     // forward (or transform/inject)
        }
        close(out)             // done; the full text is captured into State for you
        return reply.Err()     // a mid-stream model error surfaces here
    }
    turn.Reply(reply.ReadAll()) // buffered fallback (non-terminal, or non-streaming)
    return nil
})
```

Key facts:

- `turn.Stream()` returns `(chan<- string, ok)`. `ok` is **claim-once**: the
  first agent to call it in the terminal flow wins; everyone else (a sibling in a
  concurrent group, a non-terminal agent, a non-streaming request) gets
  `ok=false` and should `turn.Reply` normally.
- `reply.Stream()` and `reply.ReadAll()`/`bb.Extract` **coexist** — read the
  live tokens *and* still get the whole text (e.g. to save to memory after). You
  are never forced to choose.
- Closing `out` is enough: the framework delivers to the client and records the
  complete message into `State`, so `Respond`/`Notify` and durability all see the
  whole reply. Do **not** also `turn.Reply` the same text.
- `reply.Err()` is where a mid-stream model error lands (once tokens are flowing
  there is no HTTP status left to fail with; the server emits an SSE error frame).
- A schema agent never streams live (structured output is validated whole); its
  `reply.Stream()` yields the finished JSON once.

## Serving

```go
h, err := bb.Handler(flow, opts...)        // http.Handler for embedding
err := bb.Serve(ctx, flow,                 // or own the listener + shutdown
    bb.Addr(":8080"),
    bb.Trace(bb.JSONL(os.Stdout)),         // jsonl trace of every flow
    bb.Store(bb.MemStore()),               // durable checkpointing
)
```

`Serve`/`Handler` **validate the whole flow at startup** — modelless default
agents, unbuildable models, and declared Select exits with no matching member
all fail before the port binds. That is the single place wiring errors surface;
the other is `Ask` (schema/transport, at runtime).

Endpoints: `POST /v1/chat/completions` (OpenAI), `POST /v1/messages`
(Anthropic), `GET /v1/models`, `GET /v1/diagnostics/trace`.

### Serving several flows

One brain can serve many flows, chosen by the request's `model`:

```go
bb.WithFlow(chatFlow)                       // unnamed → the default flow
bb.WithFlow(codeFlow).As("acme/coder")      // named → picked by model id
bb.WithFlow(mathFlow).As("acme/math").
    Serve(ctx)                              // chainable; Serve ends the chain
```

A request naming a registered model routes to that flow; a request naming no or
an unknown model gets the default. Which flow is the default is a precedence
(highest wins, last-within-rank wins):

1. `bb.Serve(ctx, f)` — an explicit default passed to `Serve`.
2. `bb.WithDefaultFlow(f)` — an explicit default, no name.
3. `bb.WithFlow(f)` — the last unnamed flow.
4. `bb.WithFlow(f).As(name)` — a named flow, default only if nothing unnamed exists.

`WithFlow(f).As(name)` names a flow (calling `As` twice is a compile error). A
`RegisterFlow` (unnamed) cannot chain another `WithFlow` — a chain holds one
default — but a named flow can. `Serve(ctx)` with no default is valid when at
least one named flow is registered.

## Naming and models on any flow

`WithId` and `WithModel` are methods of **every** flow, not just `NewFlow()` —
a `Select`, an `All`/`One`/`Group`, or a `Next` chain too. Name a composite to
make it one addressable unit (Selectable, triggerable, durable); set a model on a
group to give the agents inside it a default:

```go
capabilities := bb.Select(talk, remember, house).
    WithModel(bb.NewModel("cheap")). // default for member agents that set none
    WithId("capabilities")            // name the whole group
```

Model resolution is lexical scope over the tree: **agent's own → its flow's →
nearest enclosing group's → `bb.WithDefaultModel` → first registered.** Because
these return the `Flow` interface, call them *after* the `Basic`-only `WithAgent`
(`NewFlow().WithAgent(a).WithModel(m).WithId("x")`).

## Durability (opt-in and loud)

Durability is a deliberate, per-flow choice — never a silent effect of
configuring a store. Name a flow, then make it durable:

```go
remember := bb.NewFlow().WithAgent(a).WithId("remember").Durable()
```

`WithId` returns a `NamedFlow`; only a `NamedFlow` has `.Durable()`, so a durable
flow always has the id it resumes against — durable-but-anonymous won't compile.
A flow **without** `.Durable()` never persists, even with `bb.Store(...)`
configured; the store is just the backend for the flows that opted in. A durable
flow checkpoints its sub-flows: on a retry with the same `X-Run-Id`, completed
ones replay from their savepoint (a `flow.cached` trace event) instead of
re-asking. `.Durable()` takes options — `bb.ForwardCompatible()` (resume even if
the graph changed; by default a changed structure is discarded, not resumed
into), `bb.Retries(n)`, `bb.TTL(d)`, `bb.ResumeOnReregister()`. Use
`bb.FileStore(dir)` to survive restarts.

## Initiative — triggers and scheduling

A brain can act on its own, not only per request. **Triggers are flows.** Reaching
one *splits the chain*: the flow after it becomes a deferred, durable body that
runs later, on its own.

```go
// A nightly job, registered outside Serve (runs at startup, then on the cron):
nightly := bb.NewFlow().WithAgent(summarize).WithId("nightly").
    Next(bb.Notify(text))
bb.Trigger().Next(bb.Every("0 21 * * *")).Next(nightly)

// Keep working past the reply ("I'll text you when it's done"):
router.Next(capabilities).Next(bb.Respond).Next(bb.Once(when)).Next(followUp)
```

- `bb.Trigger(opts…)` heads a startup chain; a bare `Trigger().Next(f)` is a boot
  task. `bb.Every(spec)` schedules on a cron; `bb.Once(t)` fires a single time.
- The deferred body **must** be a named flow (`WithId`) so it resolves after a
  restart; an unnamed body is warned and skipped.
- Triggers require `bb.Store(...)` (durable scheduling) and run their worker under
  `bb.Serve` (not a bare `bb.Handler`).
- A scheduled body replays the request context: `turn.Request()` (the protocol
  params) and `bb.Payload[T](turn)` (arbitrary trigger data, seeded with
  `bb.WithSeedPayload(x)` or captured from the originating request) both work in
  the fired body.
- Loops and recursion are re-triggers: a body scheduling its own id again — each
  iteration a fresh, durable run. There are no cycles in the static `Next` graph.

## THE RULES (short list)

1. **Agent configures, Turn acts.** No `Ask` at build time, no `WithModel` at
   runtime — the types won't let you.
2. **Select ids are strings** (they come from a model). Declare exits with
   `Selects` to catch typos at startup.
3. **Errors surface in two places**: `Serve`/`Handler` (wiring, startup) and
   `Ask` (schema + transport, runtime). Builders never error mid-chain.
4. **Pass `ctx` to your I/O** so a cancelled turn cancels your calls.

## Testing a flow

Build a flow, drive a request through the `http.Handler` from `bb.Handler`, and
assert on the reply — or unit-test a handler by constructing an agent with
`bb.FixedModel(...)`. For structured output, `bb.Extract[T]` gives you the typed
value to assert against. See the package tests under `internal/flow` and
`internal/serve` for patterns.
