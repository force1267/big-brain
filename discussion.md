# Product discussion

Summary of the product-discovery discussion (2026-07-19). Decisions that
came out of it are recorded in `PRODUCT.md`; this file preserves the
reasoning. Scope of the discussion: what the product does — not how it is
implemented, not how it is sold.

## Premise

big-brain wraps large models (text, vision, voice) and exposes them through
standard model-provider APIs (OpenAI- and Anthropic-compatible). From the
outside it looks like just another model provider; inside, a request runs
through background machinery — memory backed by vector stores, file
management, skill learning, endpoint calls, and multiple staged calls to
first-party models — before a response is streamed back.

## Motivating examples

**Home assistant.** The assistant holds a memory of names and photos of
approved guests. When the owner says "add my friend John to the guest list
for tomorrow's party," the wrapper runs the message through several LLM
stages to determine intent, calls a tool (an HTTP endpoint that registers a
face with the door camera), records the query and tool result in memory,
and replies "On it — I'll text you when it's done." A background job later
completes the work via a webhook, updates memory, and notifies the owner.

**Research-lab helper.** A pipeline of LLM calls and vision inputs combined
with a vector database and a text search engine. It maintains the
researcher's logs, deciding itself when a finding should be recorded. It
has a persona that may respond with humor depending on context and mood.
Its tools are complicated enough that each has its own internal pipeline.

## The brain

The common shape behind both examples: a **pipeline** — a dynamic graph of
model calls, memory operations, and tools. This project provides the
building blocks; a specific pipeline composed from them is a **brain**. A
brain responds like a regular LLM, but internally it is not one model
thinking — it is a pipeline specialized for its task.

## Discussion highlights

- The standard-API boundary is the strongest feature: every existing chat
  UI, IDE plugin, and SDK becomes a free client, and it forces all
  complexity to hide behind a boring interface.
- Two faculties make the product visibly different from the providers it
  imitates: **memory** (it remembers you across sessions, ambiently) and
  **initiative** (it can keep working after the response ends and contact
  the user later).
- Faculties (memory, initiative, senses, hands/tools, character) are what
  the product promises; pipelines are merely how it thinks.

## Brain authoring — options weighed

- **Data format** (JSON/YAML, or a protobuf-encoded graph): inspectable,
  safe, toolable — but a format grows into a programming language in
  denial; real brains need conditionals, loops, and arbitrary tool logic,
  and the node vocabulary would expand forever. Serialization choice was a
  red herring; authorship is what matters.
- **Code-first** (a Go program importing `pkg/`): unlimited expressiveness,
  the compiler as validator — at the cost of requiring Go and trusting the
  brain's code.
- **Split processes** (orchestrator here, a private "small-brain" hosted
  elsewhere): attractive for private hosting, but a deployment topology
  rather than an authoring model, with a permanent network cost.

Resolution: code-first, because a data format can be layered on top of a
code API but never the reverse; file-loaded brains and remote small-brains
remain expressible later as a loader and a node type respectively.

## Serving scope

One big-brain process serves one brain — the vLLM analogy, not OpenAI:
serving a model and being a provider are different products. A provider
(tenants, billing, catalogs) would be built *around* this project, e.g.
running many instances or a stateless brain that treats tenant identity as
request context. In scope: speaker identity *within* one brain (household
members, lab members) — one brain, one memory, aware of who is talking.

## Functionality stories (home assistant)

Ten stories that together exercise every v1 building block; each block must
earn its place by appearing in at least one story.

1. **Basic chat with character** — "good morning" answered in persona,
   streaming, through any off-the-shelf OpenAI-compatible client.
2. **Remembering ambient facts** — "we're vegetarian now" said in passing
   shapes a menu plan weeks later; the pipeline itself decided the fact was
   worth keeping.
3. **Knowing who's talking** — owner and kid ask "when is my dentist
   appointment?" and each gets their own; same brain, speaker identity from
   the API credential.
4. **Intent → tool call** — "add John to the guest list" becomes a
   structured request matching the door-camera endpoint's schema, is
   executed, remembered, and confirmed.
5. **Finish later, then reach out** — "on it — I'll text you when it's
   done": work continues after the HTTP response closes; a notification
   goes out on completion.
6. **Reacting to the world** — the door camera POSTs "unrecognized face";
   the pipeline checks the guest list in memory and opens or alerts. No
   human prompted this run.
7. **Acting on schedule** — a nightly 21:00 calendar review; "party
   tomorrow" in chat becomes a one-shot trigger the brain installed for
   itself.
8. **Time and situation awareness** — "is it too late to run the
   dishwasher?" answered knowing current time, timezone, quiet hours, and
   the system/caller context, without hand-crafted prompt plumbing.
9. **Choosing the right mind for the job** — small-talk on a fast/cheap
   model, party budget on a smart one: nodes reference declared model
   *roles*; which provider backs each role is deployment config.
10. **Multi-stage reasoning behind one reply** — weather check and RSVP
    check fan out in parallel and merge into one streamed answer; the
    caller sees one model reply, never the pipeline.

## Building blocks — taxonomy

The blocks first came up as a flat list (prompt template step, json-schema
structured output, webhook initiation, background tool call, time
awareness, cronjobs, system awareness). The agreed structure gives each
block one of three roles matching how a brain runs:

- **Triggers** — what starts a pipeline run: `chat` (the API request
  itself), `webhook`, `cron`. The load-bearing unification: a chat message
  is not special, just the most common trigger. Brains can install their
  own triggers at runtime — that is what makes initiative real.
- **Nodes** — the steps: prompt template, structured output (json-schema,
  validate first, repair model only on mismatch), tool/HTTP call,
  conditionals, fan-out/join, and — explicitly — **reply** and **notify**.
  "Background" is not a node type; it is the pipeline continuing after the
  reply node has fired.
- **Context & effects** — ambient things every node can see: memory,
  speaker identity, time/system awareness, model roles, outgoing channels.
  Not steps; injected.

Model roles (fast, smart, cheap…) are a first-class concept, not a prompt
parameter — they keep brain code portable across providers.

## Dynamism — the graph is dynamic, in grades

"We're vegetarian" was raised as a case for dynamic graphs; on inspection
it is not one — the graph is identical before and after, only *data*
(memory) changed. Principle: **behavior change lives in data whenever
possible; structure change only when data can't express it.** A brain that
learns through memory is auditable; one that rewires itself has bugs you
can't reproduce.

Because graphs are runtime objects built by code (the code-first decision
paying off again), dynamism is a capability ladder the brain author climbs,
not an engine feature:

1. **Fixed graph, dynamic data** — memory and context; ~90% of "learning".
2. **Dynamic construction** — brain code assembles or parameterizes
   subgraphs at runtime (per intent, per N events). Free; it's just Go.
3. **Self-installed triggers** — the brain schedules its own future runs.
4. **Self-modifying structure** — the brain writes and registers new
   pipelines for itself: skill learning, expanding, self-healing brains.
   Expressible as a node whose effect is "register this graph."

Levels 1–3 are in scope for v1 (all ten stories run on them). Level 4 is
deliberately deferred — it raises product questions with teeth (do learned
skills survive restarts? can you audit and roll back what the brain taught
itself?) that deserve their own discussion. The engine constraint that
keeps it possible costs nothing now: graphs are first-class values, and
registration is not restricted to startup.

## Pre-build double-check

Before building, the ten stories were re-walked for hidden assumptions.
Six surfaced; five were resolved (see `PRODUCT.md`), one stays open:

- **Transcripts vs memory** — the chat API is stateless, so memory is the
  only continuity; transcripts are the client's, facts are the brain's.
- **Caller-declared tools / reasoning** — chat includes tool calls and may
  include `<think>` blocks; honoring them is the brain developer's choice,
  the engine just carries the protocol faithfully.
- **Background-job failure** ("the broken promise") — deliberately *not*
  an engine rule. Notify-on-failure is a per-brain, per-story choice,
  documented as guidance in an authoring guide; the reference home
  assistant chooses to notify.
- **Notification channel** — v1 ships one: outgoing webhook (HTTP POST to
  a configured URL), with channels explicitly an extensible family.
- **v1 API surface** — chat completions + messages + streaming +
  `/models`; no voice/vision/realtime; client sampling params accepted and
  passed to the brain as context.
- **Persistence across restarts** — resolved below.

## Persistence

The trap was treating engine-owned state as one question; it is three,
with three promises. Memory survives unconditionally. Self-installed
triggers survive (config-defined ones reappear from code for free — it is
specifically the runtime-installed ones needing durability). For
background jobs, two options were weighed: *durable intent* (the job
record survives, re-runs from the start, at-least-once) versus *durable
execution* (resume mid-pipeline — Temporal territory: journaling, replay,
determinism constraints leaking into every brain). Durable intent won:
"your intent survives, your execution restarts" is easy to state, honest
to build, and fits the reference brain — re-registering a face twice is
harmless, losing it silently is not. At-most-once was rejected as
betraying story 5.

Cross-cutting: promises are about *what* survives, never *where*; state
lives behind engine-owned pluggable interfaces with a zero-setup default.
Externalizing them is what makes the provider/stateless-brain deployment
possible — statelessness becomes a deployment choice, not a different
product.

## One binary — dissolving the "what does small-brain produce" worry

A late question probed whether the authoring model hides a static graph
after all: if the big-brain binary "runs what the brain author produces,"
isn't that product the data format we rejected? It is not, because there
are not two binaries. big-brain is a **library**; the brain author's Go
program imports `pkg/`, builds the graph as runtime values (node bodies
are arbitrary Go closures), and calls the engine's serve entry point.
That program *is* the executable. The engine — linked into the same
process — owns the HTTP server and trigger dispatch (webhooks included);
the author's code owns the thinking. Conditionals and loops run in two
places: inside node bodies during a run, and at graph-construction time
(dynamism level 2). A protocol between orchestrator and thinker only
appears in the deferred remote-node deployment variant.

## Build order

Blocks are built as vertical slices, each making one story pass end to
end, because each slice ships something demoable and hits the risky
unknowns (pkg API shape, run model, post-reply continuation) in the order
we can afford to learn from them. Story 1 is the walking skeleton — it
forces every load-bearing decision (graph runtime, chat trigger, model
roles, streaming, serving) while staying thin. Then: 4 (structured
output/tools/conditionals), 2+3 (memory + speaker identity), 5
(continuation + notify + durable intent — hardest engine work, done after
the run model is proven), 6+7 (more triggers), 8+10 (context, fan-out).
The slice-1 author code (`cmd/jarvis-demo`) is written *first*, as the
spec the engine must satisfy.

## Engine internals — pkg/ vs internal/

A brain author is an external module, and Go forbids importing our
`internal/`; therefore everything a brain needs to compile lives in
`pkg/`, and `internal/` holds only what runs behind that API (first: the
OpenAI wire types and SSE encoding). One deliberate deviation from the
repo rule "initialization lives in internal/": the reference brain's
`main` uses only `pkg/`, exactly like a stranger would — if `pkg/` can't
comfortably init a brain in one small `main`, that is a pkg API defect,
not something to paper over with internal wiring only we can reach.

## Outcome

Decisions locked in `PRODUCT.md`: code-first authoring, one brain per
process, the home assistant as the reference brain, the
trigger/node/context taxonomy, and the dynamism ladder with levels 1–3 in
v1 scope.
