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

## Post-build: dependency-graph audit and the trigger redesign (2026-07-20)

After the ten reference stories shipped and were live-tested against a
real local model, a dependency-graph review of `pkg/` surfaced five
ownership questions, four of which were acted on directly (`pkg/cron`
extracted from `pkg/brain`; `brain.Situation` removed as a taxonomy
mismatch — PRODUCT.md always called time/system awareness ambient
*context*, not a node; `job.Job` gained a plain `Source` provenance
string instead of a `Trigger` interface, rejected as premature with only
one readiness shape; `pkg/memory.Recall` lost its `limit` param, moved
into each implementation's own constructor). The fifth — `pkg/notify`
holding its webhook implementation inline instead of in its own file —
was applied along with a full pass to make `cmd/jarvis-demo`
self-contained (a built-in dummy door-camera/notify server) after live
testing showed the demo otherwise needed two hand-rolled Python servers
to exercise stories 4–6.

That live test also reopened a terminology question: is `notify.Webhook`
misnamed, since a "webhook" should mean something *else* calls *us*? The
resolution, after checking how Stripe/GitHub/Slack actually use the term:
the industry sense is looser than that — an outgoing event-POST to a
subscriber-configured URL (exactly what `notify.Webhook` does) is the
more textbook use, not the less textbook one. Slack resolved the same
ambiguity years ago by keeping the word and qualifying it — "Incoming
Webhooks" vs "Outgoing Webhooks" — rather than renaming one side.
Decision: do the same. No identifier renames; fix the *prose* wherever
both concepts appear together (`PRODUCT.md` already did this correctly —
"incoming webhooks" / "outgoing webhook" — `docs/authoring-guide.md` did
not, and was fixed).

That reopened whether `Brain.Webhooks`/`Brain.Crons` should be owned by a
`Trigger` interface instead of `Brain` — the same move as the `Cron`
extraction, generalized. Two designs were proposed and both rejected on
inspection:

- **`Trigger.Start(ctx, enqueue) error`, blocking until `ctx` is done.**
  Rejected: nothing enforces the "must block" contract, so a forgetful
  implementer's trigger silently stops working with no error. The fix
  isn't a better-documented contract — it's the engine owning the one
  loop that waits on `ctx`, so implementers never write that code and
  can't forget it.
- **`Scheduled`/`Requested`, splitting by trigger shape.** Rejected on a
  sharper diagnosis: webhook's `Start` had to be an empty no-op, because
  all of its real logic lived in a `Mount`-returned HTTP handler. An
  interface whose primary method routinely sits empty is telling you the
  method is wrong, not that the implementation is trivial. Traced further
  ("what business logic does a trigger actually have?"): a trigger isn't
  a process with a lifecycle, it's a pure decision — "when do I next
  fire" (cron) or "what does this request produce" (webhook) — neither of
  which needs to know `ctx` exists.

The actual fix, reached by decomposing further with three concrete
examples (a node pausing mid-pipeline for an HTTP callback; a webhook
trigger defined as "the HTTP part and the graph part, separately"; a
nightly cron trigger where "time and trigger are separate concepts"):
**there is no need for a `Trigger` interface at all.** "Starting a
pipeline" was already a primitive — `Enqueue` — just never exposed outside
`serve.Run`. Once it is, every trigger kind (existing or future) is a few
lines of the brain author's own code composing `Enqueue` with either a
timer (`pkg/cron`'s already-pure `Next` function, stdlib `time`) or an
HTTP route (a new `serve.WithEndpoint` option, mirroring the equally new
`serve.WithBackground` for non-HTTP sources). No interface to implement,
no empty methods, and a genuinely new trigger kind (a Kafka consumer, a
file watcher) needs zero `pkg/` changes — it's just more code calling
`Enqueue`.

A third, distinct primitive surfaced along the way — a *node*, not a
top-level trigger, pausing a running pipeline until an inbound HTTP
request arrives (`Run.Await`) — needed for "wait for a database write a
human confirms via callback." Deliberately deferred: it's a different
problem (dynamic per-run route registration/demuxing against Go's
`http.ServeMux`, which supports neither), not a variant of the trigger
question, and deserves its own focused design pass.

Outcome: `Brain.Webhooks`/`Brain.Crons` removed; `serve.WithEndpoint` and
`serve.WithBackground` added as the two composable primitives; `pkg/cron`
kept exactly as it already was (a pure `Cron` + `Next`, zero deps) and is
no longer imported by the engine at all — only by whatever brain code
chooses to use it.

---

## The bb rewrite — from primitives to a shipped framework

The node-graph engine and its `pkg/serve`/`pkg/brain` surface were scrapped in
one long pairing session. It began not from a spec but from a list of candidate
primitives the author sketched (`Task`, `Event`, `Trigger`, `Reliable`, `Flow`,
`Model`, `Memory`, `Session`, `Chat`, `Prompt`, `Trace`) and the question "are
these any good?" The useful move was refusing to treat them uniformly: some are
real dispatch points (Task, Trigger, Model, Memory), some are just data (Event,
Chat, Prompt, Trace — the engine never calls a method on them, so they aren't
interfaces), and some are properties, not things (Reliable is a decorator, not
an interface with a method). The lesson that stuck: *only a dispatch point earns
an interface; everything else is a value or a function.*

That first exploration converged on a durable-savepoint engine (Step/Sleep over
a Store, a worker loop, cron) — which was built and hardened and now lives on as
`pkg/engine`. But the authoring surface it implied still felt wrong, and the
author restarted from a blank `cmd/marvis-demo/main.go`, writing the API they
*wanted to type* and having each symbol implemented to match. The main.go became
the goal post; the framework existed to satisfy it. This inverted the usual
order (spec → code → example) and was the single best decision of the session —
every interface question was answered by "how does it read at the call site."

The decisions that came out of that pairing, each reached by argument:

- **A brain is a tree of flows; control flow is Go.** The node-graph DSL had
  reinvented `if`, loops, and a `Vars map[string]any` bag. The fix was to make an
  agent's `OnMessage` a plain Go function and compose flows with `Next` and
  grouping. No node vocabulary to grow (the n8n/Terraform death spiral avoided).

- **Agent and Turn are two types, not one.** The author noticed the build-time
  agent (`WithModel/WithRole/...`) and the runtime handle inside `OnMessage`
  were the same type but had disjoint valid method sets — a builder shouldn't
  `Ask` (no live message), a running turn shouldn't reconfigure itself. Splitting
  them makes each invalid state a compile error. Naming was debated (`Agent`/
  `Turn` won over `AgentSpec`/`Agent`, because `NewAgent()→Agent` reads right and
  `Turn` matches the live-handle concept already used in serving).

- **Select is a string, and that's correct, not lazy.** The router selects the
  next flow by id. The first instinct was to feel bad about stringly-typed
  routing — until we named *why* it's a string: the selector is model output, and
  model output is a string. A typed `Select(flow)` would just force every author
  to write the same `switch` mapping the string to a handle. So the string
  boundary is the honest type at the LLM/Go seam. The safety net that *is* ours to
  add: validate the selector against the linked group — loudly at request time,
  and (via an optional `Selects(...)` declaration) at startup.

- **Concurrency conflicts are loud, not last-writer.** A multi-agent flow runs
  its agents concurrently, so "last Select wins" is only well-defined within one
  agent (program order); across concurrent agents a *divergent* select is a
  wiring error the flow raises, never a wall-clock race. The author's framing —
  "handling concurrency is what Go devs do; we gave them Checkpoint/Wait" — was
  right, but the nondeterministic select was *our* hazard leaking into their code,
  so we made it an error instead of a silent race.

- **`reply.Extract[intent]()` is impossible; it's a free function.** Go forbids
  type parameters on methods. This was the one place the goal-post couldn't be
  satisfied as written, flagged for the author to decide; the resolution
  (`bb.Extract[intent](reply)`) matched the `bb.Schema[intent]()` shape already
  blessed. A good reminder to surface impossibilities rather than hack around
  them.

- **Errors surface at exactly two points.** Builders only record data (they never
  fail mid-chain); everything structural surfaces at `Serve`/`Handler`
  (`flow.Validate` at startup) and everything model-dependent at `Ask` (schema
  mismatch + transport, at runtime). Two surfaces a developer must hold in their
  head, nothing scattered. `Extract` owns nothing — `Ask` already validated.

- **`NewModel(tags...)` always returns a builder.** Zero tags = blank, one-or-more
  = seeded from the tag registry, and *still overridable* (`NewModel("cheap").
  WithTemprature(0.9)`), because it's a value-immutable `Spec`. The author probed
  this directly ("can a registered model still be tweaked?") and value semantics
  made the answer yes for free.

Implementation followed a strict discipline: leaves first, each package real and
exhaustively tested (happy/unhappy/edge/every branch, concurrency under `-race`)
before the next — model → agent/turn → flow → concurrency → serve → durability.
The one goal-post divergence (Extract) and one goal-post *bug* (a `return flow`
missing `, nil`) were both surfaced by compiling a throwaway copy of main.go with
the import added — a cheap way to prove API conformance without touching the
author's file.

Then the reference brain was rebuilt on the new surface. `jarvis` was
deliberately *not* a copy of `marvis`: where marvis routes with a model + typed
schema, jarvis uses a keyword router into a Select group (talk/remember/recall/
house/briefing) over a self-contained dummy world (sensors, devices, a notify
sink), with memory as author state, concurrent sensor reads, a `Notify` flow
after `Respond`, and durability. It runs with no API key. Building it surfaced an
honest limitation to report rather than paper over: the request→flow surface has
no scheduled/cron trigger and no true fire-after-reply async — those live in
`pkg/engine`, unexposed (captured as a follow-up item at the time).

Cleanup was aggressive, as the author encouraged: `pkg/{brain,serve,memory,
notify,cron,job}`, `internal/{app,config,logging,telemetry}`, and `cmd/cli` were
deleted — anything the bb design superseded — and `go mod tidy` dropped the
orphaned dependencies. Memory, notably, has *no* bb primitive: it's the brain's
own state (a map, or a KV via `bb.MemStore`), woven into the persona by a
handler. That was a deliberate call — memory *strategy* varies too much to
impose one, so the engine gives durability and a KV and stays out of the way.

Finally, the ponytail debt ledger was cleared: seven deliberate shortcuts fixed,
including two that were real features rather than one-liners — a min-heap for the
pending queue, sharded FileStore, cron catch-up, retry-with-yield on long
backoff, bounded cancel tombstones, schema enum tags, and — the biggest — turning
`Group` from a same-starting-chat approximation into a genuine live shared chat
(`agent.SharedChat`, write-through replies), so a group member sees another's
reply as it lands.

The through-line of the whole session: *write the call site first, let only
dispatch points be interfaces, name why each shortcut is honest before taking
it, and surface impossibilities instead of hacking around them.*

## Post-bb evolution: ladder, multi-flow, request params, streaming (2026-07-24/25)

After the bb framework shipped, a run of feature discussions pushed the surface
forward, each one starting from the goal-post `marvis-demo/main.go` — the file
the author edits to *state* the desired API before any of it exists.

**Model inheritance ladder.** "No agent is model-less." An agent inherits its
model from the flow it runs in, and a flow from a default model. The registration
call became fluent — `bb.WithModel(m).WithTag("cheap","fast")` — with a
`WithDefaultModel` and an implicit "first registered is the default", resolved
along agent → flow → default. The point was ergonomic honesty: leaving `WithModel`
off should mean "use what the flow/deployment provides", not a silent nil.

**Multi-flow serving.** One process, several named flows, chosen by the request's
`model` id, with a default for unnamed/unknown — `bb.WithFlow(f).As("acme/coder")`.
This forced a reconciliation with the "one brain per process" framing: the
resolution was *one deployment, one owner, several flows* — a routing convenience,
explicitly **not** first-class multi-tenancy (no tenant boundary, no isolated
memories; that stays somebody else's product built around this one).

**Request params as context.** A client's sampling params (`model`,
`temperature`, `max_tokens`) reach the flow read-only via `turn.Request()` — for
the handler to honor, clamp, ignore, or branch on. Never auto-applied to the
agent's own model config. The principle: a brain stays a brain, not a passthrough
model the caller reconfigures.

**Streaming — the design conversation that mattered most.** The author's first
instinct was a `turn.Stream()` that threads a live channel flow-to-flow, each
agent teeing the previous stream onward. The load-bearing objection came from the
author themselves: durability means sequential flows with *complete*-message
handoff — the checkpoint between flows is a whole message — so a live stream
cannot cross a flow boundary and stay resumable. That killed the thread-it-
everywhere design and collapsed the problem to its tractable core:

> There are two output paths. The durable one (`State`) always carries complete
> messages. The live one is a parallel tee to the client that exists only at the
> **terminal** boundary (the flow before `Respond`, else the last flow).

The interface settled on `reply.Stream()` (live, backed by a record-replay buffer
so `ReadAll`/`Extract` coexist — the developer is never forced to choose) and
`turn.Stream() (chan<-, ok)` — a **claim-once** client sink, `ok=false` for
non-terminal / concurrent / non-streaming turns. The framework tees the author's
channel to the client and records the whole message into `State`, so downstream
and durability see complete text. A default agent auto-streams. Errors after the
first byte become SSE error frames (no HTTP status left). Coextensive discovery:
a latent panic — `Handler` deduped flows via a map keyed by `Flow`, but a `seq`
is unhashable.

## The trigger redesign — "time is a flow" (2026-07-25)

Surfacing `pkg/engine` for *initiative* (durable background + scheduled work)
started as a `bb.Task`/`bb.Later` proposal and was replaced, by the author, with
something far more in the grain of the framework: **scheduling is composition.**

`WithId` already names flows, so there is no need for a separate "task" concept —
a named flow is already the addressable unit. The missing piece is triggers:

- `bb.Every(spec)` and `bb.Once(t)` are **flows** that split the chain — reaching
  one defers the flow after it to the engine (repeatedly for `Every`, once for
  `Once`) and ends the current pass.
- `bb.Trigger(opts...)` is the non-request entry point: a chain headed by it runs
  at startup (a bare `Trigger.Next(f)` is a boot task), and it can seed a
  synthetic context — `bb.Trigger(bb.WithRequest(...))`.

The unifying realization: **an HTTP request is just a synchronous trigger seeded
with the request envelope, carrying a response sink.** `bb.Respond` is not a
special node — it is the sink finalizer: deliver to the response sink if one is on
the context, else do nothing (so `Respond` after a cron trigger is a genuine
no-op). That is the same sink the streaming work already threads flow-by-flow. A
webhook is then just `bb.Trigger(payload)` sugar.

Two distinctions were drawn deliberately:

- **Unify the abstraction, not the execution.** One `Trigger` primitive and one
  flow-execution path — but request triggers run *inline and ephemeral* (no store
  write per request), while cron/once triggers are *enqueued and durable*. Routing
  every chat request through the durable job queue would be write amplification
  and latency for nothing.
- **Tier durability instead of forbidding dynamism.** The author rejected
  restricting schedulers to the top level. Because the engine resolves a scheduled
  body by its flow *id* at fire time, the honest line is: a **startup-registered
  named flow** is durable across restart; a **dynamic/anonymous** body runs while
  the process lives but not across a restart (its registration was code that only
  ran for that request). Not a chosen limit — a forced one. Scheduling is allowed
  anywhere sequential; only inside a concurrent group is "what comes after me"
  undefined.

Data on the turn split cleanly: `turn.Request()` for the well-known protocol
envelope, and a generic `bb.Payload[T](turn)` for arbitrary trigger-specific data
(free function, since Go methods can't be generic — the `Extract`/`Schema`
precedent). `Trigger` options seed both.

## Everything is a flow: interface-level WithId/WithModel, loud durability, scheduling (2026-07-25)

The trigger discussion kept going and settled the flow *interface* itself.

**WithId and WithModel belong on `Flow`, not just `Basic`.** Every implementation
(basic, Select, All, Group, a Next chain) defines them in its own sense. The
consequence the author liked: `WithModel` on a group means "the default model for
the agents inside me", which turns the model ladder into **lexical scope over the
composition tree** — agent → nearest enclosing flow/group that set a model →
`WithDefaultModel` → first registered — implemented by each composite pushing its
model onto the context as it descends, innermost wins. `WithId` on any flow names
a whole composite as one addressable unit (selectable, triggerable, durable).

**Durability made loud and typed.** The author rejected durability as an ambient
effect of configuring a store — it should be visible in the code and controlled
per flow. So a type-state builder: `Flow.WithId() → NamedFlow`, and
`NamedFlow.Durable() → DurableFlow`. You cannot make an anonymous flow durable —
`Durable` only exists on `NamedFlow`, which only `WithId` produces — so the
id-before-durability rule the discussions kept restating becomes a *compile
error*, in the same spirit as Agent-can't-Ask. `Store` demotes to "the backend
for flows that opted in." A flow without `.Durable()` is ephemeral even with a
store configured. Durability tiers by registration: a **startup-registered named
flow** survives restart (its id re-resolves after reload); a dynamic/anonymous one
runs while the process lives but not across a restart — a forced line, not a
chosen one. Options on `Durable()`: resume trigger (at-startup vs on-reregister),
retries, TTL (inherited down the tree like the model), and `.ForwardCompatible()`
to opt out of the discard-on-structure-change guard; a `--no-resume` flag wipes
pending state for a clean redeploy.

**What `WithId`/`Durable` scope over — and why "sticky" was wrong.** The author
probed `A.Next(B).WithId("x").Durable().Next(C)`: what is named/durable? The
resolution: `WithId`/`Durable` are **properties of a flow value**, so they attach
to the **receiver** — everything composed so far, to their left — as a *suffix*,
not a forward-opening prefix (that distinction is what separates them from the
scheduler nodes `Every`/`Once`, whose meaning *is* forward). So the durable named
unit is `A→B`; `C` is a sibling outside it, because naming creates a boundary that
does not flatten. This killed an earlier "sticky NamedFlow" idea (letting
`Durable` be called after more `.Next`s): it let the name and the durable boundary
*drift apart*, reintroducing the exact ambiguity the type-state removes. The
crisp rule instead: **`WithId` and `Durable` are adjacent, together defining one
boundary = the receiver; `.Next` closes it and starts a sibling.** Whole-chain
durability → name+durable at the end; sub-span → build it, name+durable it, embed
it. Nested names fall out as the zoom hierarchy for the monitoring UI, and the
same names anchor the checkpoint keys — identity, persistence, and observability
all ride one tree.

**Schedulers inside groups — defined, not forbidden.** An earlier instinct to ban
`Every`/`Once` inside `All`/`One`/`Group` was wrong: a scheduler sits inside a
*member*, and a member is sequential, so its suffix is well-defined. Reaching a
scheduler **resolves** that member (it contributes its chat-so-far plus a deferred
continuation). The subtle part is `One`: registering a schedule is a durable side
effect, and `One` cancels losers, so *whether the cron exists* would depend on a
race. The fix ties the side effect to the chat: **a schedule commits iff the
member's contribution is kept** — all members in `All`/`Group`, only the winner in
`One` — implemented by staging enqueues per member and flushing on acceptance
(immediate everywhere non-concurrent).

**Cycles are re-triggers.** `Next` builds an acyclic chain; loops and recursion
are expressed by a flow (or its agent) scheduling its own id again — each
iteration a fresh, checkpointed run. No cycles in the static graph, no in-memory
recursion; the only hazard (runaway self-trigger) is met with observability and an
optional cap, not a prohibition.

**Durability, right-sized.** The author's sharp observation: durability is, in
practice, "a big word for persisted crons" — and the trigger design makes that
*more* true, since request triggers run inline and ephemeral (no store write) and
only enqueued (cron/once/background) runs touch the store. The one thing it adds
beyond persisted schedules is **mid-run resume across expensive/side-effecting
steps** (skip the model call already paid for, don't re-run the tool that already
booked) — latent today because runs are short, valuable exactly for the complex
tool-pipeline brains the product keeps aiming at. So: keep the flow-checkpoint
resume (cheap, built), scope it to opted-in durable flows, don't gold-plate.

The through-line: *everything is a flow; the interface methods mean what each flow
kind makes them mean; make the load-bearing rules unrepresentable-when-violated
rather than documented; and let the composition tree be the one spine that carries
identity, model scope, durability keys, and trace spans at once.*

## Tool use: two boundaries, three types, and the turn/chat split (2026-07-26)

The design session that settled how a model can *request* a tool call, and how
caller-declared tools pass through. It started from a rejected sketch
(`WithTool(...)` plus an automatic tool-call loop inside `Turn.Ask`) and ended
somewhere structurally different — a split handler signature — so the reasoning
matters more than the API listing.

**Two tool boundaries, not one.** The clarifying move was noticing that "tools"
names two opposite conversations:

- *Outer* (bb-as-model ↔ its client): the client declares tools; bb must surface
  them, emit tool-call requests back, accept tool results, and **never execute the
  caller's tools itself**. This is mimicry-consistency and PRODUCT already
  promises it ("caller tools pass through untouched") while `internal/serve`
  doesn't even parse the field.
- *Inner* (a bb agent ↔ the upstream model it calls): the agent wants the model
  to call a tool that **bb's own Go code** runs; the client never sees it. This is
  just a Go loop the author writes.

Both boundaries need the same load-bearing prerequisite: **`model.Stream` must
carry tool definitions in and surface tool-call blocks out**, and the
`internal/openai` + `internal/anthropic` adapters must parse `tools`/`tool_choice`
on the wire and emit `tool_use`/`tool_calls`. Nothing else is possible until then,
which is why it's the first commit and why the surface above it is thin.

**Why `WithTool` + an auto-loop lost.** A flow can have several models, some small
with tiny context; a developer may declare tools for an upstream model that have
nothing to do with what the end user sees; and running a model agentic-style in a
loop is *the reason it's called an agent*. Auto-forwarding the caller's tool list
to every `Ask` takes that control away. So forwarding became explicit and stacked
(`ForwardTools()` + `WithTools(...)`), and the loop stayed the author's Go — the
same stance as `Durable()` being opt-in over an ephemeral default.

**The turn/chat split — the change that made tools tractable.** With tool calls
and tool results flowing in *both* directions, one `Turn` object was carrying two
opposite conversations, and the verbs collided: was `Add(result)` feeding the LLM
or answering the client? Was a tool call outbound or inbound? Splitting the
handler into two handles removed the ambiguity at the type level:

```go
OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error { … })
```

`turn` is purely client-facing (`Messages`/`Last`, `Request()`, `Reply`, `Stream`,
`Call`, `ToolResults`, `Select`); `chat` is purely model-facing (`Add`,
`WithTools`, `WithToolChoice`, `ForwardTools`, `Ask`/`AskWith` → a `Reply` with
`ReadAll`/`Stream`/`ToolCalls`/`Messages`). `Ask` was *always* model-facing and
`Reply`/`Select` *always* client-facing — tools only made the seam impossible to
ignore. `turn.Add` moves to `chat.Add`. The cost is one extra parameter and
holding two conversations in your head, but that *is* the shape of an agent: it
mediates a client and a model. The Turn-only design was hiding the duality behind
overloaded verbs.

A bonus fell out: separating the model **definition** from the **conversation**.
`Model` stays immutable config; `Model.Chat() ModelChat` is the live handle the
handler receives — and is usable standalone, giving bb a "just talk to a model, no
flow" story for free (tests, scripts, inside a Go tool a flow calls).

**Three types, and the conflation that had to be resolved.** A definition and an
invocation are not the same thing, and forcing them into one type would have hurt:

```go
type Tool       struct { Name, Description string; Schema map[string]any } // definition
type ToolCall   struct { ID, Name string; Input json.RawMessage }          // invocation
type ToolResult struct { CallID, Content string; IsError bool }            // answer
```

`WithTools` takes `Tool`s; `reply.ToolCalls()` and `turn.Call(...)` speak
`ToolCall`. The symmetry felt in the design was real, but it lives on the
`ToolCall` side: *the same value* comes out of the upstream model and goes into
`turn.Call`. These are structs and stay structs — pure DTOs, no varying behavior to
seal; the bb types that are interfaces (`Model`, `Agent`, `Flow`, `Turn`) are
interfaces because behavior varies or the set is sealed.

**No intersection types, so payloads hang off `Message`.** Go has no
`Message & ToolResult`, and type assertions at every use site were unacceptable.
The idiomatic answer is one struct with optional typed payloads — and providers
put *several* calls in one assistant message and several results in one user
message, alongside text, so they're plural:

```go
type Message struct { Role, Content string; Calls []ToolCall; Results []ToolResult }
```

Access is a typed nil/len check, never an assertion. "Text plus a tool call" then
exists naturally. `ToolCall.Message()` / `ToolResult.Message()` wrap a lone payload
so `chat.Add(...Message)` stays single-typed.

**The keystone: the resolution rule.** At `bb.Respond`, a tool-call message with
**no matching tool-result message in the chat** becomes the client-facing
`tool_use`/`tool_calls` response; an answered call is resolved history and stays
internal. That single rule covers all three cases at once — relay to the client
(call it, don't answer it), handle internally in Go (answer it, loop), and
cross-agent/cross-flow handoff (agent A calls, flow B answers). Resolution is
**local to a flow**, because the chat slice is per-flow: a counterpart living in
another flow's slice, or never returned by the client, resolves to an id-only stub.
`ToolCall.ToolResult()` / `ToolResult.ToolCall()` are therefore *linked-or-stub* —
populated when bb hands you the value from a chat, a stub when you constructed it.
The stub isn't a compromise; it's required precisely *because* bb is stateless: a
client often resends only the result, not the original call.

**No tool state to persist.** There is no server-side tool loop. bb emits
`tool_use` and the turn ends; the client runs the tool and resends the whole
transcript, exactly as a real model API expects — transcripts belong to the client.
On the next request the flow re-runs from the top and deterministic routing lands
it on the same path (the same property durable-resume relies on). This is the big
payoff of mimicking a model faithfully: nothing to checkpoint, no loop to resume.

**Two invariants that are correctness, not taste.** Parallel tool use is *several
`tool_use` blocks in one message*, so: (1) bb coalesces all of a turn's `Call`s into
one outgoing message — that, not variadic `Call`, is the mechanism; and (2) all
results answering parallel calls go in **one** message
(`bb.NewMessage("").WithResults(r1, r2, r3)`), because splitting them across
messages is a documented footgun on both providers that trains a model out of
calling in parallel.

**The naming grammar.** `WithXs()` = setter, `Xs()` = getter, `X(arg)` = action;
arity reinforces it. Hence `chat.WithTools(...)` / `turn.Request().Tools()` /
`turn.Call(...)`. Two deliberate deviations from pure symmetry: `turn.Call` over
`turn.ToolCall` (a noun pretending to be a verb reads worse than it symmetrizes),
and `ForwardTools()` kept while `ForwardCalls()` was dropped. The latter looks
arbitrary but isn't — the distinction is the *source*, not the coupling (both
couple turn and chat equally): the request is singular and stable, so "the client's
tools" has one obvious meaning and earns a name; a reply is transient and plural in
an agentic loop, so `ForwardCalls()` would have to guess "the last one." Variadic
`turn.Call(reply.ToolCalls()...)` keeps that explicit and names its source.

Also rejected: `Tool.ToolCalls()`/`Tool.ToolResults()`. A `Tool` is a static
definition; listing "every call to this tool" would give it a back-reference to the
whole chat, turning a value type into a context-bound object. If it's ever real,
it's a turn query, not a method on the definition.

**Staged builders, matching the library's style** (don't stuff parameters into one
function); builder weight tracks how many fields are genuinely required:

```go
bb.NewTool().As(name).Is(desc).WithSchema(bb.Schema[T]())  // 3 required → 3 stages
bb.NewToolCall().As(name).WithInput(v)                     // id auto-assigned
bb.NewToolResult().WithId(callID)                          // usable as-is;
                                                           // .WithContent / .AsError optional
bb.NewMessage(text).WithCalls(...).WithResults(...).As(role)
```

Schema belongs on `Tool` **only**. A `ToolCall` is an instance — its shape is
defined by the `Tool` it names. The write-side `any` on `WithInput` is the honest
JSON boundary (it's your own Go value); the read side is `json.RawMessage` decoded
with `bb.Extract[T](call)`. A generic `ToolCall[T]` was rejected: `reply.ToolCalls()`
is a heterogeneous stream (different tools, different arg types) and the type
parameter would infect the whole read path. Type safety lives at the two edges that
can actually carry it — the schema on the definition, `Extract` on the read.
`ToolResult.Content` is an opaque string the model just reads; no schema, and
optional (a void result is legal).

**The default agent is a full transparent proxy — and that reversed an earlier
call.** An earlier position had a bare agent *not* forwarding tools ("control in
the dev's hands"). The better rule is a hard binary: a bare agent forwards
everything untouched and replays everything untouched; the moment the author writes
`OnMessage`, all magic stops and every forward is explicit. No middle ground, no
surprising partial defaults. The provided default is essentially:

```go
reply, err := chat.ForwardTools().AskWith(turn.Messages()...)
if err != nil { return err }
if out, ok := turn.Stream(); ok { /* stream text */ } else { turn.Reply(reply.ReadAll()) }
turn.Call(reply.ToolCalls()...)
```

Two things fall out free: the stateless tool loop just works through the proxy
(client sends tools → forwarded → model requests a tool → replayed → client runs it
and resends the transcript → `AskWith(turn.Messages()...)` includes the result →
model continues, with no special handling); and pointing any OpenAI/Anthropic SDK
at a bare bb agent makes it behave *exactly* like the underlying model, tools
included — the "agent disguised as a model" promise honored at its most literal.
`ForwardTools()` forwards the client's **tools and its tool choice** — it's
"forward the client's tool intent," not just the list.

**Deferred:** streaming tool-call *arguments*. Text streams live; emitted calls are
buffered whole in v1. When it returns, `turn.StreamCall(name)` reads better than
the `CallStream` first floated.

**Left to decide at implementation time**, deliberately not now: what
`WithSchema` means on a *no-handler* agent (leaning: the default proxy validates
the replayed output and the error surfaces from `Ask`).

The through-line: *tool calls and tool results are just messages in the chat; the
direction is disambiguated by which handle you touch, not by an overloaded verb;
one resolution rule (unanswered call ⇒ client-facing) makes relay, internal
execution, and cross-flow handoff the same mechanism; and the client owns the
transcript, so the loop needs no state at all.*

### Sugar over the manual loop: `OnCall` + `Resolve` (2026-07-26)

The design above leaves every tool-using agent writing the same twenty lines: a
`switch` on `call.Name`, an `Extract`, a `NewToolResult`, coalescing the results
into one message, and the surrounding loop. That is real boilerplate, and worse,
dispatching by name in a `switch` **duplicates the tool's name as a magic string**
— the definition says `"read_sensor"` and so does the case label, and nothing
checks they agree. So bb should own dispatch. The question was only *how*, and the
first proposal had the right pieces in the wrong place.

**`OnCall` belongs on the `Tool`.** Not on the agent (`agent.WithTool(t, fn)`),
which would kill per-ask tool sets and stop tools from being reusable values. The
rule that keeps `Tool` honest as a DTO: **`OnCall` is local-only.** A `Tool` parsed
off the wire (`turn.Request().Tools()`) can never carry one, by construction — so
forwarding a tool drops nothing, because there was nothing to drop. `Tool` stays
pure data plus an optional local handler.

**`bb.OnCall` is a free generic function that returns a copy** — Go methods can't
take type parameters, which is the same reason `bb.Schema[T]` and `bb.Extract[T]`
are already free functions here:

```go
var readSensor = bb.NewTool().As("read_sensor").Is("read a house sensor").
    WithSchema(bb.Schema[sensorArgs]())                              // bare definition, reusable

realHouse := bb.OnCall(readSensor, house.Read)   // production binding
fakeHouse := bb.OnCall(readSensor, stub.Read)    // test binding — same definition
```

The first draft had the handler *replace* `WithSchema`, deriving the schema from the
handler's argument type so that schema/handler drift would be unrepresentable. That
was revised, for two reasons that turned out to matter more:

- **One `Tool` shape, always.** `As` → `Is` → `WithSchema` is how every tool is
  built, including the ones parsed off the wire, which never had a handler stage to
  begin with. Two construction shapes for one type is worse than a weaker guarantee.
- **Copy semantics buy the mock pattern.** A bare definition at package level with
  different handlers bound per agent — production and stub, or forwarded bare in one
  agent and handled locally in another — is the project's own
  mock-for-test-injection rule falling out for free. Fusing definition and handler
  forecloses it.

So `OnCall` **checks** the schema instead of deriving it: compare `bb.Schema[T]()`
against the tool's recorded schema and record a wiring error on mismatch. Drift
still cannot ship; it fails at `Serve` rather than at compile time, alongside every
other wiring error (unknown model name, bad `Selects` id) — the pattern the `WithX`
builders already follow. Since `Tool` cannot be generic (heterogeneous `WithTools`,
wire-parsed values), boot is the earliest honest point anyway.

A raw `.OnCall(func(ctx, bb.ToolCall) (string, error))` method stays as the escape
hatch for a tool that wants to inspect its own arguments.

**Not staged.** `bb.OnCall(tool).Does(fn)` was considered and rejected. Mechanically
it can't infer `T` (a method can't introduce a type parameter), so the type would
have to be written at the first stage *and* again in the closure signature. And
stylistically, every staged builder here adds a differently-named **required** field
per stage — the stages exist because there are several things to say and type-state
orders them. `OnCall` adds exactly one thing; a two-stage builder for one field is
ceremony, `OnCall(...).Does(...)` reads redundantly, and the intermediate value is
meaningless on its own ("a tool that will call… something"). `OnCall` belongs to the
`Schema[T]`/`Extract[T]` family of free generic functions, not to the builder family.

**A consequence of copy semantics, allowed deliberately:** a developer *can* write
`bb.OnCall(turn.Request().Tools()[0], fn)` and answer a caller's tool themselves.
That is fine — "never execute the caller's tools" means never *implicitly*; an
explicit binding is the author choosing to serve that capability. The invariant is
better stated as: **a forwarded tool never gains a handler by itself.**

**Two `Ask`s with different behavior was the part to reject.** The first sketch had
plain `chat.Ask()` run `OnCall` handlers without sending their results, and a
`chat.UsingTools(...)` variant whose `Ask` also fed them back. Both halves are
wrong:

- `Ask()` must **never** run `OnCall` — not even the run-but-don't-send middle
  state. That state means `Ask` silently turned on the heater and then declined to
  tell the model. Executing a side effect cannot be an implicit consequence of
  asking a question.
- `WithTools` vs `UsingTools` is a coin-flip pair: nobody remembers which noun runs
  your Go code. That is precisely the overloaded-noun problem the turn/chat split
  had just removed, sneaking back in one layer down.

**So the mode goes on the verb, where the call site shows it:**

```go
chat.WithTools(readSensor, setDevice).Ask()      // one round; never runs your Go; returns the calls
chat.WithTools(readSensor, setDevice).Resolve()  // ask → run OnCall → feed results back → repeat
```

`Resolve` is the design's own vocabulary, not a new coinage: a call is *resolved*
when it has a matching result (the keystone rule). The method reads as exactly what
it does — **ask, resolve every call I can resolve locally, hand back only the ones
I can't.**

**The third piece of the proposal — giving client-declared tools a default
"client-facing `OnCall`" that tags them for an automatic `turn.Call` — was cut, and
the keystone rule is why it's unnecessary.** A tool with no local handler simply
comes back unresolved in `reply.ToolCalls()`, and one visible line relays it:

```go
reply, _ := chat.ForwardTools().WithTools(readSensor).Resolve()
turn.Call(reply.ToolCalls()...)  // whatever bb could not resolve locally goes to the client
turn.Reply(reply.ReadAll())
```

An auto-tag would make a `chat`-facing method produce a `turn`-facing side effect,
re-coupling the two handles the split had just separated, and it would revive the
auto-relay that was deliberately killed. The win is that this falls out for free:
**`Resolve` is the mixed case done right** — server tools execute, client tools fall
through, in one call, with the boundary decided by nothing more than "does it carry
a local handler."

Coalescing also stops being a rule the author must remember and becomes bb's
invariant: `Resolve` batches one round's results into one message itself.

Four semantics pinned with it:

1. **A handler error becomes an `IsError` result, not an aborted `Resolve`.** A
   failing tool is information the model should see and retry against — that is what
   `is_error` is for. Only context cancellation and panics abort.
2. **A round cap.** `Resolve` loops, so a model can loop it forever: a default cap
   (~8) with `.WithMaxRounds(n)` and an error on exhaustion. This is the
   runaway-cycle guard already flagged as open work, arriving early because `Resolve`
   is the first construct that can spin without the author writing a `for`.
3. **`Resolve` stays opt-in inside `OnMessage`.** The bare-agent proxy still does
   not resolve — it forwards and relays, and caller tools have no local handler to
   run anyway. The hard binary survives untouched.
4. **Durable flows re-run side-effecting tools.** `OnCall` runs server-side, so an
   at-least-once resume re-runs it. Identical to the hand-written `switch`, but it
   now hides inside a framework call, so it is owed an explicit line in the guide.

The manual path is never taken away: `Ask` + `reply.ToolCalls()` + `bb.Extract` +
`bb.NewToolResult` remains the low-level surface, and `Resolve` is sugar sitting on
exactly it — the same stance as `Durable()` over the ephemeral default and
`ForwardTools()` over `WithTools(turn.Request().Tools()...)`.
