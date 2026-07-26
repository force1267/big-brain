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
- An **Agent** is _build-time_ configuration (model, role, schema, handler).
- A **Turn** and a **ModelChat** are the agent _live_ on one message — the two
  handles an `OnMessage` handler receives. `turn` faces the **client**
  (`Reply`/`Stream`/`Call`/`Select`), `chat` faces the **model**
  (`Add`/`Ask`/`Resolve`). Neither can reconfigure the agent. (The compiler
  enforces both splits.)

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
replies — forwarding the caller's tools and relaying the model's tool calls
untouched, so it behaves exactly like the model behind it. That is the whole
walking skeleton.

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
`BIG_BRAIN_BASE_URL`). Flow code names a _role_; deployment decides which
provider backs it.

By default a name resolves through the OpenAI-compatible client. To consume a
model natively through Anthropic's own API instead:

```go
bb.NewModel().WithName("claude-sonnet-5").WithProvider(bb.AnthropicProvider)
```

`WithProvider` is the only thing that changes — the same registry, tags,
`WithTemprature`, and inheritance ladder apply either way. `WithThink(true)`
requests extended reasoning mode; only the Anthropic provider honors it (a
fixed token budget), OpenAI silently ignores it. This is independent of `bb.Serve`, which always speaks both the
OpenAI and Anthropic wire protocols to *callers* regardless of which provider
a brain *consumes*.

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
    OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
        chat.Add(turn.Last())             // add the latest incoming message
        reply, err := chat.Ask()          // send role + added chat to the model
        if err != nil {
            return err                    // schema mismatch + transport surface here
        }
        turn.Reply(reply.ReadAll())       // add an assistant message to the flow
        return nil
    })
```

**A handler gets two handles, and which one you touch says which direction you
mean.** An agent mediates two opposite conversations — the client that called
your brain, and the model your brain calls — and the same nouns (a message, a
tool call, a tool result) flow both ways. So they are separate objects:

|       | `turn bb.Turn` — the CLIENT side                   | `chat bb.ModelChat` — the MODEL side                 |
| ----- | -------------------------------------------------- | ---------------------------------------------------- |
| read  | `Messages`, `Last()`, `Request()`, `ToolResults()` | `Messages()`                                         |
| write | `Reply`, `Stream`, `Call`                          | `Add`, `WithTools`, `ForwardTools`, `WithToolChoice` |
| act   | `Select`                                           | `Ask`, `AskWith`, `Resolve`                          |

`chat.Ask` asks the model; `turn.Reply` answers the client. `chat.Add` builds
the prompt; `turn.Call` asks the client to run something. Neither can
reconfigure the agent — there are no `With…` methods for that, so runtime
self-modification stays impossible.

- `turn.Messages` is the incoming conversation; `turn.Last()` is the latest.
- `chat.Add(msgs…)` chooses what the next `Ask` sends; `chat.AskWith(msgs…)` is
  `Add`+`Ask`.
- `turn.Reply(text)` appends output to the flow's chat (zero or many times); it
  does **not** go to the model.
- `reply.ReadAll()` / `Read()` / `Stream()` read the answer; `bb.Extract[T](reply)`
  decodes it into a schema type.
- `ctx` is this turn's context; it is done when the handler returns. Pass it to
  any I/O you do so cancellation is respected.
- `turn.Request()` is the client's request as context: the sampling parameters
  it sent (`model`, `temperature`, `top_p`, `max_tokens`, `stop`, `think`, …).
  They are **not** applied to your agent automatically — they are an input for
  the handler to read and act on.

**Request parameters.** The engine never silently honors a client's sampling
knobs; it hands them to the flow so _you_ decide. A handler can honor, clamp,
ignore, or branch on them:

```go
OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
    req := turn.Request()
    if n := req.MaxTokens; n != nil && *n < 64 {
        chat.Add(bb.NewMessage("Answer in one short sentence.").As("system"))
    }
    // req.Model is the model id the client asked for (also what selects a
    // named flow at the serving layer); req.Temperature/TopP/Stop are theirs
    // to weigh in. req.TopK is Anthropic-only (nil on OpenAI requests).
    // req.MaxTokens already resolves OpenAI's deprecated max_tokens vs. the
    // current max_completion_tokens for you — one field either way.
    // req.Tools() / req.ToolChoice() are the tools the client declared.
    // req.Think (Anthropic "thinking", OpenAI "reasoning_effort") is nil when
    // the client sent no opinion; non-nil is a request, not a command — the
    // agent's own model decides whether WithThink means anything to it.
    chat.Add(turn.Last())
    reply, err := chat.Ask() // asks with the agent's own model config, not req's
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
reply, err := chat.Ask()
if err != nil { return err }
it := bb.Extract[intent](reply)
turn.Select(it.Intent)
```

Struct tags shape the schema sent to the model: `doc:"…"` becomes a field
description, and `enum:"a,b,c"` constrains a field to a fixed set — handy for a
router that must pick one of a known list of ids.

`bb.Extract` is a free function (not `reply.Extract[T]()`) because Go forbids
type parameters on methods — the same shape as `bb.Schema[T]()`. It reads a
tool call's arguments too: `bb.Extract[sensorArgs](call)`.

**Talking to a model with no flow at all.** The same `ModelChat` works on its
own, which is useful in tests, scripts, or inside a Go tool a flow calls:

```go
reply, err := bb.Chat(ctx, bb.NewModel("smart")).AskWith(bb.NewMessage("hi"))
```

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
- Within one agent, the last `Select` wins (program order). Across _concurrent_
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
recognizer := bb.NewAgent().OnMessage(func(ctx context.Context, t bb.Turn, _ bb.ModelChat) error {
    t.Reply(classify(t.Last().Content)); bb.Reached(cp); return nil
})
guard := bb.NewAgent().OnMessage(func(ctx context.Context, t bb.Turn, chat bb.ModelChat) error {
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

## Tools

A model cannot run anything. It can only _ask_ — the client executes and sends
the result back. bb sits on both sides of that: it is a client to its upstream
model, and a model to whoever called it. So there are two boundaries, and they
are different problems:

- **Inner** — your agent wants the upstream model to call a tool **your Go code**
  runs. The caller never learns it exists.
- **Outer** — your _caller_ declared tools and bb must faithfully pass them
  through: surface them, relay the model's requests back, accept the results.
  bb never executes a caller's tool by itself.

Three plain data types cover both, and they are just messages in the chat:

```go
type Tool       struct { Name, Description string; Schema map[string]any } // a definition
type ToolCall   struct { ID, Name string; Input json.RawMessage }          // an invocation
type ToolResult struct { CallID, Content string; IsError bool }            // an answer
```

A `Message` carries `Calls` and `Results` alongside its text, so "let me check"
plus two tool calls is one ordinary message and reading one is a `len` check,
never a type assertion.

### Defining a tool

```go
type sensorArgs struct {
    Sensor string `json:"sensor" enum:"temperature,humidity" doc:"which sensor"`
}

readSensor := bb.NewTool().
    As("read_sensor").                     // the name the model emits
    Is("read one of the house sensors").   // the whole basis for the model choosing it
    WithSchema(bb.Schema[sensorArgs]())    // the argument shape
```

That is a _bare_ definition: pure data, no handler, no chat. It can be sent to a
model, forwarded from a client, or compared.

### Inner tools, the manual way

`Ask` sends the tools and **never runs your code** — executing a side effect can
never be an implicit consequence of asking a question. You get the calls back and
decide:

```go
OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
    chat.Add(turn.Last())
    for {
        reply, err := chat.WithTools(readSensor, setDevice).Ask()
        if err != nil { return err }
        calls := reply.ToolCalls()
        if len(calls) == 0 {                   // the model answered in prose
            turn.Reply(reply.ReadAll())
            return nil
        }
        results := make([]bb.ToolResult, 0, len(calls))
        for _, c := range calls {
            switch c.Name {
            case "read_sensor":
                a := bb.Extract[sensorArgs](c)  // decode this call's arguments
                results = append(results, bb.NewToolResult().WithId(c.ID).
                    WithContent(house.Read(ctx, a.Sensor)))
            }
        }
        chat.Add(bb.NewMessage("").WithResults(results...))  // ALL of them, one message
    }
})
```

Two rules are load-bearing here:

- **Nothing is forwarded implicitly.** A flow has several models, and a small one
  must not be handed every tool in the process, so each ask declares what its
  model may call. `WithTools` applies to that ask only.
- **Answer a round's calls in ONE message.** Parallel tool use is several calls
  in one message; splitting the answers across messages is a documented footgun
  on both providers that trains a model to stop calling in parallel.

### Inner tools, the short way: `bb.OnCall` + `Resolve`

The `switch` above repeats every tool's name as a string with nothing checking it
still matches. Bind a handler to the tool instead and bb dispatches:

```go
realSensor := bb.OnCall(readSensor, func(ctx context.Context, a sensorArgs) (string, error) {
    return house.Read(ctx, a.Sensor)
})

OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
    reply, err := chat.WithTools(realSensor, realDevice).Resolve(turn.Last())
    if err != nil { return err }
    turn.Reply(reply.ReadAll())
    return nil
})
```

- `bb.OnCall(tool, fn)` returns a **copy** — the bare definition stays bare, so
  one definition can carry several bindings (production, a test stub, or none at
  all where the tool is only forwarded).
- It **checks** the handler's argument type against the schema already on the
  tool rather than replacing it, so every tool is built the same way. A mismatch
  is recorded and surfaces at the first `Ask` that would send it; the broken tool
  never reaches a provider.
- `Ask` = one round, runs nothing. `Resolve` = ask → run the handlers → feed the
  results back → repeat, until the model answers without calling. The mode is on
  the verb, so the call site says which you meant.
- A handler returning an **error becomes an is-error result** the model reads and
  can retry against — not an aborted turn. Only a cancelled context stops it.
- `Resolve` is **capped** (`.WithMaxRounds(n)`, default 8) so a model that keeps
  calling errors instead of spinning.
- A round is **all-or-nothing**: if a batch mixes tools bb can run with tools only
  the client can, `Resolve` runs none of them and hands the whole batch back.
  Both providers reject a turn where some call went unanswered, and running a
  side effect whose result must then be discarded is worse than not running it.

### Outer tools: your caller's

The caller's tools arrive as read-only context, always **bare** — a tool that
crossed the wire never carries a handler, so bb cannot execute one by accident:

```go
turn.Request().Tools()        // []bb.Tool the client declared
turn.Request().ToolChoice()   // "" (auto), "any", "none", or a tool name
turn.ToolResults()            // results the client sent back
```

Forward them explicitly, and relay what the model asks for:

```go
OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
    reply, err := chat.ForwardTools().AskWith(turn.Messages...)  // tools AND choice
    if err != nil { return err }
    turn.Call(reply.ToolCalls()...)   // ask the CLIENT to run them
    turn.Reply(reply.ReadAll())       // text can accompany the calls
    return nil
})
```

`ForwardTools()` is sugar for `WithTools(turn.Request().Tools()...)` plus the
choice, and the two stack: `chat.ForwardTools().WithTools(readSensor)` sends the
caller's tools plus your own. There is no `ForwardCalls` — a reply is transient
and plural, so `turn.Call(reply.ToolCalls()...)` names its source instead.

### The one rule that ties it together

At the end of a request:

> **A tool call with no matching result in the chat goes to the client. A call
> that has one is settled history and stays internal.**

That single rule gives you all three behaviours with no extra machinery:

| what you do                                  | what the client sees                     |
| -------------------------------------------- | ---------------------------------------- |
| `turn.Call(c)` and never answer it           | `tool_use` / `finish_reason: tool_calls` |
| answer it (`Resolve`, or add a `ToolResult`) | nothing — it was internal                |
| one agent calls, a later agent/flow answers  | nothing — handoff needs no mechanism     |

**There is no tool state on the server.** bb emits the call and the turn _ends_.
The client runs the tool and re-sends the whole transcript — exactly as it would
to a real model API — and the flow re-runs from the top with the result in
`turn.Messages`. Nothing to checkpoint, no loop to resume. Counterpart lookups
(`call.ToolResult()`, `result.ToolCall()`) are therefore resolved **per flow**:
if the counterpart is not in the messages this flow saw, you get an id-only stub
rather than a nil or a panic.

### A bare agent is already tool-transparent

An agent with **no `OnMessage`** is a full transparent proxy: it forwards the
caller's tools and choice and replays the model's text and tool calls untouched.
Point any OpenAI or Anthropic SDK at it and it behaves exactly like the model
behind it, tools included. Write `OnMessage` and **all** of that stops — every
forward becomes explicit, which is the point: you own the loop.

## Memory

Memory is the brain's own state — bb does not impose a store. Keep facts in a
map, or in a KV via `bb.MemStore()` / `bb.FileStore(dir)` (a `Get`/`Put`
backend), and read/write it inside a handler, weaving recalled facts into the
persona:

```go
if facts := mem.recall(); len(facts) > 0 {
    chat.Add(bb.NewMessage("You remember: " + strings.Join(facts, "; ")).As("system"))
}
```

See `cmd/jarvis-demo` for a complete memory + tools + briefing brain.

## Streaming to the client

The user can see tokens as the model types them — but only at the **terminal**
boundary: the flow whose reply is the answer (the one before `bb.Respond`, or
the last in the chain). Everywhere upstream, flows hand each other _complete_
messages, because that is what durable checkpointing needs. So `State` always
carries whole messages; streaming is a parallel live tee that exists only at the
end.

A **default agent (no `OnMessage`) streams automatically** when it is terminal
and the client asked for it — nothing to write. To stream from a handler, tee
the model's live output into the outgoing channel `turn.Stream()` hands you:

```go
OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
    chat.Add(turn.Last())
    reply, err := chat.Ask()
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
  live tokens _and_ still get the whole text (e.g. to save to memory after). You
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
    bb.DefaultFlowName("jarvis"),          // reported id, default "brain"
)
```

`Serve`/`Handler` **validate the whole flow at startup** — modelless default
agents, unbuildable models, and declared Select exits with no matching member
all fail before the port binds. That is the single place wiring errors surface;
the other is `Ask` (schema/transport, at runtime).

`bb.DefaultFlowName` only labels a flow served **without** a registry name —
the `flow` passed straight to `Serve`/`Handler`, or one added via
`bb.WithDefaultFlow`. It sets what `/v1/models` and every response's `model`
field report for that flow; it never affects routing. A flow named via
`bb.WithFlow(f).As("acme/coder")` (below) already reports that name and
ignores `DefaultFlowName`.

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
these return the `Flow` interface, call them _after_ the `Basic`-only `WithAgent`
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
one _splits the chain_: the flow after it becomes a deferred, durable body that
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
  `bb.Serve` or `bb.Run` (not a bare `bb.Handler`).
- `bb.Run(ctx, bb.Store(...), ...)` drives triggers and the engine with **no
  HTTP endpoint at all** — for a brain that only reacts to crons/timers/
  internal events, never inbound requests. Same startup wiring as `Serve`
  (validates trigger chains, requires `Store`), minus the listener; `Addr` and
  request-only options are ignored.
- A scheduled body replays the request context: `turn.Request()` (the protocol
  params) and `bb.Payload[T](turn)` (arbitrary trigger data, seeded with
  `bb.WithSeedPayload(x)` or captured from the originating request) both work in
  the fired body.
- Loops and recursion are re-triggers: a body scheduling its own id again — each
  iteration a fresh, durable run. There are no cycles in the static `Next` graph.

## THE RULES (short list)

1. **Agent configures, Turn/ModelChat act.** No `Ask` at build time, no
   `WithModel` at runtime — the types won't let you.
2. **`turn` is the client, `chat` is the model.** Direction is carried by which
   handle you touch, never by an overloaded verb. `chat.Ask` asks the model;
   `turn.Reply` answers the client.
3. **Select ids are strings** (they come from a model). Declare exits with
   `Selects` to catch typos at startup.
4. **Nothing about tools is implicit.** `Ask` never runs your handlers;
   `WithTools`/`ForwardTools` apply to one ask; an unanswered call goes to the
   client and an answered one stays internal.
5. **Errors surface in two places**: `Serve`/`Handler` (wiring, startup) and
   `Ask` (schema, transport, and a tool whose handler disagrees with its
   schema — runtime). Builders never error mid-chain.
6. **Pass `ctx` to your I/O** so a cancelled turn cancels your calls.

## Testing a flow

Build a flow, drive a request through the `http.Handler` from `bb.Handler`, and
assert on the reply — or unit-test a handler by constructing an agent with
`bb.FixedModel(...)`. For structured output, `bb.Extract[T]` gives you the typed
value to assert against. See the package tests under `internal/flow` and
`internal/serve` for patterns.
