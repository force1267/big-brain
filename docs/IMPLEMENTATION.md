# IMPLEMENTATION

How big-brain is built. The public surface is **`pkg/bb`** — the one package a
brain author imports. Everything else is an implementation concern in its own
package behind it (Effective Go: small, single-responsibility packages; `bb` is
pure wiring). `cmd/marvis-demo/main.go` is the **goal post** — the exact API an
author writes; the code exists to satisfy it. Read `PRODUCT.md` first, then
`docs/authoring-guide.md` for how to use the surface.

## The authoring model

A brain is a tree of **flows**. A flow runs one or more **agents** over an
incoming chat, collects their replies, and hands the result to the next flow.
Flows compose — a group of flows is itself a flow — and `Next` chains them:

```
router.Next(bb.Select(talk, remember, …)).Next(bb.Respond).Next(notify)
```

Control flow is Go: an agent's `OnMessage` handler is a plain function that can
branch, call tools, and `Select` the next flow. There is no graph DSL, no
`Vars` bag, no node vocabulary to grow.

The two-type split is the load-bearing decision: an **Agent** is build-time
configuration (`WithModel/WithRole/WithSchema/Selects/OnMessage`) and cannot
act; a **Turn** is the agent live on one message (`Add/Ask/Reply/Select`) and
cannot reconfigure. Each invalid state is unrepresentable at compile time.

## Packages

```
pkg/bb/          The facade. Type aliases + constructors delegating to the
                 packages below. Owns only the small value types with no
                 separate concern of their own: Prompt templates, typed Schema.
                 No business logic. The one package authors import.

pkg/model/       The model concern. Model interface (Stream), providers
                 (OpenAI, Mock), the Spec builder (WithName/Think/Temprature,
                 value-immutable), the tag Registry (Register/Lookup/Resolve
                 plus a process Default), Message + As. Bound injects a specific
                 Model. Leaf.

internal/agent/  Agent (build-time) + Turn (runtime) + Reply. An agent asks its
                 model, validates the reply against its schema (schema mismatch
                 is owned here, by Ask), replies, and selects. Reply is backed by
                 a record-replay streamBuf so reply.Stream() (live) and
                 reply.ReadAll()/Extract (whole) coexist. Turn.Stream() hands a
                 terminal agent a live client sink (claim-once). Depends on
                 model.

internal/flow/   Flow orchestration. The sealed Flow interface; Basic (one or
                 more agents, run concurrently); seq (Next chaining); the four
                 grouping strategies Select/All/One/Group; Checkpoint/Wait/
                 Reached; Respond/Notify prebuilt flows; the trace seam; and
                 durable checkpointing over a Store. Depends on agent.

internal/serve/  The boring boundary. Validates the flow(s) at startup, then
                 serves them over OpenAI- and Anthropic-compatible HTTP (+
                 /models, + /v1/diagnostics/trace). A process can serve several
                 named flows (a flow Registry, picked by the request's model id)
                 plus one default; last-wins default precedence. Handler for
                 embedding, Serve for the runner. Depends on flow +
                 internal/{openai,anthropic} wire.

internal/openai/ + internal/anthropic/   Wire request/response types and SSE.

pkg/engine/      A durable at-least-once job engine (Store, Step/Sleep, worker
                 loop, cron). bb uses only its Store implementations (MemStore/
                 FileStore) as the flow-checkpoint backend; the rest stands
                 alone for job-style use.
```

No cycles: `bb` points down at the internals and at `model`/`engine`; the
internals never import `bb`. `pkg/model.Structured` satisfies the agent's
`Schema` interface **structurally**, so no package imports another just for a
type.

## Key mechanisms

**Select routing.** An agent `Select`s a flow id (a string, because the
selector is model output — that is the honest type at the LLM boundary). At
request time an unknown id is a loud error, not a silent misroute
(`ErrUnknownSelect`). Optionally an agent declares its exits with `Selects(…)`;
then `flow.Validate` checks, **at startup**, that every declared exit is a
member of the downstream Select group.

**Concurrency.** A flow's agents run concurrently; `All`/`One`/`Group` run
member flows concurrently. `All` merges every reply, `One` takes the first
finisher and cancels the rest, `Group` runs members over one live shared chat
(`agent.SharedChat`, write-through replies) so a member sees another's reply as
it lands. Two agents selecting **different** ids is a loud `ErrSelectConflict`,
never a wall-clock last-writer race; the same id is fine. `Checkpoint`/`Wait`/
`Reached` coordinate agents within a flow. All concurrency is `-race` clean.

**Errors surface at two points only.** `Serve`/`Handler` (all wiring and config,
at startup, via `flow.Validate`) and `Ask` (schema mismatch + transport, at
runtime, because it depends on live model output). Builders only record;
`Extract` is a pure typed getter.

**Durability (loud, opt-in).** Durability is a per-flow, typed choice, not an
ambient effect of configuring a store. `WithId` yields a `NamedFlow`; only a
`NamedFlow` has `Durable(opts…) DurableFlow`, so durable-but-anonymous is a
compile error. `Serve` puts the store on the run context (`WithStore`) but
checkpoints nothing by itself; a `Durable` flow activates a checkpoint for its
subtree (`activateDurable`), and the leaf flows under it save/load keyed by
`(run id, structural path)`. A flow without `Durable` never persists, even with
a `Store`. A request carries its run id via `X-Run-Id`; a retry resumes
completed sub-flows (a `flow.cached` trace event) instead of re-asking. A
structure-version guard discards a checkpoint whose graph changed since (unless
`ForwardCompatible`), so a redeploy is not resumed into a different tree.
`Durable` also records resume-trigger / retries / TTL options for the trigger
work (Phase C) to honour. Structural path (not completion order) keeps keys
stable under concurrency.

`WithId`/`WithModel` are `Flow`-interface methods (every flow kind, not just
`Basic`): naming a composite makes it one addressable unit; `WithModel` on a
group is the default model for its agents, so model resolution is lexical scope
over the tree (a ctx-carried "nearest flow model", innermost wins). Composites
carry id/model/durability via a shared `decorated` wrapper; `Basic` carries its
own.

**Triggers & initiative.** Scheduling is composition: `Every(spec)`/`Once(t)` are
flow nodes, and reaching one in a chain **splits** it — `seq.run` hands the flows
after the trigger to a `Scheduler` (on ctx) as a deferred body and stops the
pass. `flow` defines only the `Scheduler` seam; `serve` provides the engine-backed
impl (`engineScheduler`) that registers each body once by id and schedules it —
`engine.Every` for a cron, `engine.Enqueue` for a one-shot — then runs
`engine.Run` as a worker under `Serve`. `Trigger(opts…)` heads a startup chain
(self-registers; `Serve` runs it at boot, which reaches its `Every`/`Once` and
schedules the rest); a bare `Trigger().Next(f)` is a boot task. A mid-request
`Once` works the same way (the scheduler is on the request ctx too), capturing the
chat + request params as the payload so the fired body replays the request
context. The body runs durably at **job** granularity (the engine re-runs it on
crash); it must be a named flow so it resolves after a restart (an unnamed body is
warned and skipped). Triggers require a `Store` (durable scheduling); with none,
initiative is off. `Handler` schedules but only `Serve` runs the worker.

**Observability.** Every flow boundary, select, response, and cached-resume is a
timed trace `Event`. Tracers: the diagnostics ring (always on, exposed at
`/v1/diagnostics/trace`), a `JSONL` writer, or an author's own.

**Streaming (terminal-only).** Durability forces sequential flows with
*complete*-message handoff — the checkpoint between flows is a whole message, so
a live token stream cannot cross a flow boundary and stay resumable. Live
streaming is therefore confined to the **terminal** boundary, and there are two
output paths: `State` (durable, always whole) and an ephemeral live tee to the
client. Mechanics: `Serve` installs a per-request client `Sink` on the context
for a streaming request; `seq.run` strips that sink from every step except the
terminal one (the step before the first `Respond`, else the last step), so only
terminal turns see it. `Turn.Ask`, when a sink is present and the agent has no
schema, returns a **live** `Reply` (a goroutine pumps `model.Stream` into a
`streamBuf`); otherwise it buffers and validates as before. `Turn.Stream()`
returns `(chan<- string, ok)` — `ok` is a **claim-once** atomic, so exactly one
agent (the first) streams; concurrent/`Group` turns and non-terminal turns get
`ok=false` and fall back to `turn.Reply`. The framework tees the author's
outgoing channel to the client sink *and* accumulates it into `State` as one
complete message (so `Respond`/`Notify` downstream and the checkpoint all see
whole text). A default (no-handler) agent does this tee automatically. Errors
after the first streamed byte cannot be an HTTP status: `Serve` emits an SSE
error frame and truncates. At-least-once still holds — a crash mid-stream
re-runs the terminal flow and re-streams from the top.

## Planned surface (design decided, not yet built)

Recorded so the built system and the goal post (`cmd/marvis-demo/main.go`) don't
silently diverge; the reasoning is in `docs/discussion.md` ("Everything is a
flow…") and the plan in `next.md` (#2). Phase A (`WithId`/`WithModel` on the
interface), Phase B (loud typed durability), Phase C (triggers + engine wiring),
and Phase D's turn data model (`bb.Payload[T]`) are now built (above). The
payload — arbitrary trigger data — rides the context as raw JSON (`agent.WithPayload`),
is seeded by `WithSeedPayload`, captured into a scheduled body's payload, and
replayed when it fires, so `bb.Payload[T](turn)` reads the same data in a request,
a startup chain, or a fired body. Still to come:

- **`Webhook` trigger** — needs an inbound HTTP endpoint in `serve` that fires a
  trigger on request (not just startup); `bb.Payload` already provides the data
  half, so this is the reception half.
- **`Respond` as sink finalizer** — deferred. It was unnecessary for triggers: a
  cron/once body runs on the engine worker, not through `Serve`'s HTTP delivery,
  so `Respond` is already a no-op there. `Serve` still emits the response for
  requests; folding delivery into `Respond` is a later cleanup.
- **Finer durability inside a triggered body** — a fired body is durable at job
  granularity (engine re-run on crash); a `Durable` flow *inside* it does not yet
  checkpoint (the worker ctx has no `WithStore`). Honoring `Durable`'s
  retries/TTL via the engine's cron/enqueue options is also pending.
- **Cycles as re-triggers** — supported in principle (a body re-schedules its id);
  no guard/observability yet.

## The Go-impossible bit (one deliberate divergence from the goal post)

`reply.Extract[intent]()` cannot exist — Go forbids type parameters on methods.
It is a **free function**, `bb.Extract[intent](reply)`, exactly like
`bb.Schema[intent]()`. This is the only place the built API differs from the
goal-post source, and it is recorded here so it is not mistaken for an omission.

## Build order (how it was built; how to extend it)

Leaves first, each package real and exhaustively tested (happy, unhappy, edge,
every branch; concurrency under `-race`) before the next:

1. **model** — Spec, Registry, Message.
2. **agent** — Agent/Turn/Reply, Extract.
3. **flow** — Flow/Basic/seq/Next, Select, Respond.
4. **concurrency** — concurrent agents, All/One/Group, Checkpoint.
5. **serve** — Validate + OpenAI/Anthropic HTTP + diagnostics.
6. **durability + trace** — checkpoint/resume, timings, JSONL, Notify.

## Status and deferred depth (documented, not hidden)

Built, green, and demonstrated end to end by both `cmd/marvis-demo` (intent
routing with a model + schema) and `cmd/jarvis-demo` (a full smart-home brain
with a self-contained dummy world, memory, tools, briefing, notify, durability).

Deliberately deferred:

- **True token streaming through flows.** `Serve` runs the flow to completion,
  then streams the buffered reply. A live token stream (and genuine
  reply-then-keep-working after `Respond`) needs a streaming-chat pass.
- **Scheduled/self-installed triggers (cron).** Not in the bb request→flow
  surface; `pkg/engine` has the durable scheduler (crontab, catch-up, durable
  retry-with-yield) if a job-style API is wanted.

(`Group` now runs members over one live shared chat — a member sees another's
reply as it lands — via `internal/agent.SharedChat`; it is no longer a
same-starting-chat approximation.)

## Repo rules (from CLAUDE.md)

`effective.go` per package; interfaces small (Flow is 3 methods: `run`/`id`
unexported seal it, `Next` exported); sentinel errors wrapped `%w`; tests for
happy/unhappy/edge/every branch; every session appends to `LOG.md`;
`docs/authoring-guide.md` moves with the code.
