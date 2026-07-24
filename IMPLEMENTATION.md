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
                 value-immutable), the tag Registry (RegisterModel/Lookup/
                 Resolve), Message + As. Bound injects a specific Model. Leaf.

internal/agent/  Agent (build-time) + Turn (runtime) + Reply. An agent asks its
                 model, validates the reply against its schema (schema mismatch
                 is owned here, by Ask), replies, and selects. Depends on model.

internal/flow/   Flow orchestration. The sealed Flow interface; Basic (one or
                 more agents, run concurrently); seq (Next chaining); the four
                 grouping strategies Select/All/One/Group; Checkpoint/Wait/
                 Reached; Respond/Notify prebuilt flows; the trace seam; and
                 durable checkpointing over a Store. Depends on agent.

internal/serve/  The boring boundary. Validates a flow at startup, then serves
                 it over OpenAI- and Anthropic-compatible HTTP (+ /models, +
                 /v1/diagnostics/trace). Handler for embedding, Serve for the
                 runner. Depends on flow + internal/{openai,anthropic} wire.

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

**Durability.** With a `Store` configured, each leaf flow's result is
checkpointed, keyed by `(run id, structural path)`. A request carries its run
id via the `X-Run-Id` header; a client that retries a crashed run with the same
id resumes — flows that already completed replay from the savepoint (a
`flow.cached` trace event) instead of re-asking the model. Structural path (not
completion order) keeps keys stable under concurrency.

**Observability.** Every flow boundary, select, response, and cached-resume is a
timed trace `Event`. Tracers: the diagnostics ring (always on, exposed at
`/v1/diagnostics/trace`), a `JSONL` writer, or an author's own.

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
