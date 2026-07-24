# PRODUCT

What big-brain is, decided so far. Product only — no implementation, no
business strategy. This is the **third** framing: the first was a node-graph
DSL, the second "a brain is a plain Go function with durable *intent*". Both
are scrapped. The through-line that survived every round is in `new-arch.md`;
the reasoning that produced this framing is in `LOG.md`, `CRITIQUE.md`, and
the `conversation-*.txt` transcripts.

## Core idea

**An agent that disguises itself as a model.** big-brain wraps large models
(text, vision, voice) behind standard model-provider APIs (OpenAI- and
Anthropic-compatible). From the outside it is just another model endpoint —
every existing chat UI, IDE plugin, and SDK is a free client. Inside, a
request runs through a **brain**: model calls, memory, tools, and background
work that make it far more effective than one model for a specialized task.

## The one thing the engine sells

**Durable, resumable, observable execution — and nothing you didn't ask for.**
A brain author could hand-write the model calls, the prompt templates, the
graph, the database wiring, and get exactly what the reference brain does. The
engine earns its place by owning the parts they would get wrong or forget:

- **Composition.** Agents, flows, `Select` routing, and the concurrency
  strategies (`All`/`One`/`Group`, `Checkpoint`) — the author writes handlers
  and wiring; the engine runs the tree, merges replies, and resolves selection.

- **Durable, resumable execution.** With a store configured, each flow's result
  is checkpointed; a client that retries a crashed run (same run id) resumes
  from the flow that was interrupted instead of re-asking the model. Like a save
  point in a game — the run continues from where it was.

- **Observability, free from the same boundaries.** Every flow start/end,
  select, response, and cached-resume is a timed trace event. Backends: a
  diagnostics ring (always on, at `/v1/diagnostics/trace`), a jsonl writer, or
  the author's own. Debugging is a byproduct of running the tree.

- **The boring boundary.** OpenAI/Anthropic-compatible serving, `/models`,
  startup validation of the whole wiring, faithful passthrough of the chat
  protocol.

- **Faculties.** Model roles, structured extraction (typed `Schema`/`Extract`),
  typed prompt templates, a `Notify` outgoing flow — the common machinery,
  abstracted so it adds value without getting in the way of business logic.

## The authoring model

**A brain is a tree of flows, and control flow is Go.** A flow runs one or more
agents over a chat and hands the result to the next flow; flows compose (a group
of flows is a flow) and chain with `Next`. An agent's `OnMessage` handler is a
plain Go function: it branches, calls tools, reads memory, and `Select`s which
flow runs next. There is no graph DSL, no `Vars map[string]any`, no node
vocabulary to grow — `if` is `if`.

The load-bearing split: an **Agent** is build-time configuration (model, role,
schema, declared exits, handler) and cannot act; a **Turn** is the agent live on
one message (`Add/Ask/Reply/Select`) and cannot reconfigure. Each invalid state
is unrepresentable at compile time — a builder can't ask, a running turn can't
change its model.

The library is the product; a brain is a Go program that imports `pkg/bb`,
assembles its flows, and calls `bb.Serve`. A data-format brain (a graph loaded
from a file) can be layered on top later — a graph is just a flow that walks a
node list; the reverse is impossible — so that door stays open without paying
for it now.

## What the engine does NOT promise

- **Streaming is buffered today.** A reply is produced by running the flow to
  completion, then streamed to the client. True token-by-token streaming (and
  genuine keep-working-after-reply) is a known future pass.
- **At-least-once, not exactly-once.** A durable run that a client retries with
  the same run id resumes from the last completed flow; a crash in the narrow
  window before a result is checkpointed re-runs that flow. Side effects that
  must not double are the author's responsibility (an idempotency key derived
  from the run), aided but not guaranteed.

## The faculties (what the product promises the end user)

- **Memory** — the brain remembers across turns and decides what to remember.
  Memory is the brain's own state: an agent's handler reads and writes it (a
  map, a KV via `bb.MemStore`/`bb.FileStore`, a vector DB) and weaves recalled
  facts into the persona. The engine gives durable execution and a KV; what to
  remember and how to recall is the author's — deliberately, so memory strategy
  never gets in the way. A memoryful "model" is visibly unlike the providers it
  imitates.
- **Initiative** — the brain keeps acting past the reply: flows chained after
  `bb.Respond` (e.g. a Notify that reaches out), and an outgoing-notification
  flow. Durable, so a promise made survives a restart. (Scheduled/self-installed
  triggers live in `pkg/engine`, not yet surfaced in the request-driven bb API.)
- **Senses** — vision, voice, text (roadmap beyond v1 text).
- **Hands** — tools; complex tools get their own internal flows.
- **Character** — persona, mood, when to joke.

## Working state within a run

The chat threading through a flow chain *is* the run's working state: each flow
appends its replies, and the next flow sees them. Beyond that, an agent handler
holds ordinary Go variables for the span of a turn. Long-term facts are memory
(above); the chat is the scratch a single run carries between flows.

## Reference brains

**The home assistant.** Chosen because it exercises both differentiators —
memory and initiative — with the fewest heavy dependencies. Two reference
brains, both `pkg/bb`-only, exactly as an external author writes them:

- `cmd/marvis-demo` is the **goal post**: an intent router that classifies each
  message with a model + typed schema, then `Select`s a capability. The bb API
  was designed to make this program read well; the framework exists to satisfy
  it.
- `cmd/jarvis-demo` is the **runnable smart-home brain**: a keyword router into
  capabilities (talk, remember, recall, house, briefing) over a self-contained
  dummy world (sensors, devices, a notification sink), with memory kept across
  turns, concurrent sensor reads, a Notify flow after the reply, durable
  execution, and a jsonl trace. It runs with no API key.

## One brain per process

**This codebase is vLLM, not OpenAI.** One process serves exactly one brain =
one "model" = one memory, one character. Being a *provider* (tenants, billing,
catalogs) is somebody else's product built around this one — enabled by the
embeddable `pkg/` and an externalized store (tenant-keyed state, stateless
brain). "Many users" in scope means **speaker identity within one brain**: the
household members of a home assistant. The brain tells speakers apart but
remains one brain with one memory.

## Configuration

Serve takes functional options (`bb.Addr`, `bb.Store`, `bb.Trace`, `bb.Workers`)
with zero-config defaults, and provider credentials come from environment
(`BIG_BRAIN_API_KEY`/`_BASE_URL`/`_MODEL`, 12-factor). Model roles are
first-class and portable: `bb.RegisterModel(m, "fast", "cheap")` binds a model
to tags, `bb.NewModel("fast")` fetches it, and flow code names the role while
deployment config decides which provider backs it.

## Serving: handler first, runner second

The engine exposes an `http.Handler` the author can mount anywhere, and a
convenience runner that owns the listener and graceful shutdown. Author-added
routes are served either way — mounting the handler yourself still serves the
engine's routes plus yours. Chat completions (OpenAI) and messages
(Anthropic), both streaming, plus `/models`. Sampling parameters clients send
are accepted, never an error, and surfaced to the brain as context. Caller
tools and `<think>` blocks pass through untouched; honoring them is the
brain's choice.

## Continuity — transcripts vs memory

The chat API is stateless: the client sends history each request; the engine
keeps no server-side conversation. **Transcripts belong to the client;
durable facts belong to memory.** Memory is the only continuity there is.

## Parallelism

A flow's agents run concurrently, and the grouping strategies run member flows
concurrently: `All` (merge every reply), `One` (first finisher wins, others
cancelled), `Group` (one live shared chat — a member sees another's reply as it
lands). `Checkpoint`/`Wait` coordinate agents within
a flow. Two agents selecting different next-flows is a loud error, never a
silent last-writer race. The same tracing applies to parallel work.

## Non-goals (v1)

Voice/vision/realtime endpoints; multi-tenancy or provider features; a graph
file format or node-type registry; a generic plugin system; server-side
transcripts; exactly-once delivery; durable execution of un-`Step`-wrapped
code. Redis/vector backends and richer join policies: later, when a slice
demands them.
