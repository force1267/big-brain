# Authoring guide

How to write a brain with `pkg/bb`. A brain is a Go program that assembles a
tree of **flows** and serves it. This guide moves with the code; if it disagrees
with `pkg/bb`, the code wins. Two complete examples live in `cmd/marvis-demo`
(intent routing with a model + schema) and `cmd/jarvis-demo` (a runnable
smart-home brain).

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

    bb.RegisterModel(bb.NewModel().WithName("gpt-4o-mini"), "chat")

    agent := bb.NewAgent().
        WithModel(bb.NewModel("chat")).
        WithRole(bb.Role("You are a terse assistant."))
    flow := bb.NewFlow().WithAgent(agent)

    bb.Serve(ctx, flow) // OpenAI + Anthropic at :8080
}
```

A default (no-`OnMessage`) agent just asks the model with the incoming chat and
replies. That is the whole walking skeleton.

## Models

```go
bb.RegisterModel(bb.NewModel().WithName("gpt-4o-mini").WithTemprature(0.5), "fast", "cheap")
m := bb.NewModel("fast")                 // seeded from the registered model
m2 := bb.NewModel("fast").WithTemprature(0.9) // overrides just this use
inline := bb.NewModel().WithName("gpt-4o") // no registry, built inline
demo := bb.FixedModel("canned reply")    // no provider — for demos/tests
```

`bb.NewModel(tags…)` is always a builder: with no tags it starts blank, with
tags it is seeded from the registered model and stays overridable. Provider
credentials come from the environment (`BIG_BRAIN_API_KEY`, `BIG_BRAIN_BASE_URL`).
Flow code names a *role*; deployment decides which provider backs it.

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

## Durability

With `bb.Store(...)`, each flow's result is checkpointed per run. A request
carries its run id in the `X-Run-Id` header; if the process crashes and the
client retries with the same id, the flows that already completed replay from
their savepoint (a `flow.cached` trace event) instead of re-asking the model.
Use `bb.FileStore(dir)` to survive process restarts.

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
