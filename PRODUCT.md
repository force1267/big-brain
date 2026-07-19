# PRODUCT

What big-brain is, decided so far. Product only — no implementation, no
business strategy. History of the discussion lives in `discussion.md` and
`LOG.md`.

## Core idea

**An agent that disguises itself as a model.** big-brain wraps large models
(text, vision, voice) behind standard model-provider APIs (OpenAI- and
Anthropic-compatible). From the outside it is just another model endpoint —
every existing chat UI, IDE plugin, and SDK is a free client. Inside, a
request runs through a **brain**: a pipeline of model calls, memory, files,
search, and tools that makes it far more effective than one model for a
specialized task.

The engine ships the building blocks; a brain composes them into a specific
character. The constraint is a feature: everything clever must fit behind a
boring API.

## The brain's faculties (what the product promises)

- **Memory** — the brain remembers across sessions, ambiently: it decides
  what to remember. A memoryful "model" is visibly unlike every provider it
  imitates.
- **Initiative (asynchrony)** — the brain can keep working after the
  response ends and initiate contact later ("I'll text you when it's
  done"): background jobs, incoming webhooks, outgoing notifications. A
  deliberate, loud break from the stateless-model illusion.
- **Senses** — vision, voice, text inputs.
- **Hands** — tools; complicated tools get their own internal pipelines.
- **Character** — persona, mood, when to joke.

Pipelines are how the brain thinks; faculties are what the product promises.

## Decision: how a brain is authored

**Library-first. A brain is a Go program.** It imports
`github.com/force1267/big-brain/pkg/...`, defines its nodes, conditionals,
and tools as code, assembles the graph as a runtime object, and hands it to
the engine, which serves it.

Why not a data format (JSON/YAML/protobuf graph): a format is a programming
language in denial — real brains need conditionals, loops, and arbitrary
tool logic, and the node vocabulary would grow forever (the n8n/Terraform
trajectory). Serialization choice (protobuf vs JSON) was a red herring;
authorship is what matters.

Key insight that keeps doors open: **a data format can be layered on top of
a code API, never the reverse.** Deferred, both expressible later as node
types / loaders without touching the engine:

- A file-format brain = a generic brain whose graph is loaded from a file.
- Split topology (orchestrator here, private "small-brain" hosted elsewhere)
  = a remote-node type. It's a deployment topology, not an authoring model;
  don't pay the network-protocol cost before it's needed.

## Decision: one brain per process

**This codebase is vLLM, not OpenAI.** One big-brain process serves exactly
one brain = one "model" = one memory, one character. Being a *provider*
(tenants, billing, catalogs) is somebody else's product built around this
one — enabled by the embeddable `pkg/`, possibly with a stateless brain
that treats tenant identity as request context.

"Many users" in scope means **speaker identity within one brain**: the
household members of a home assistant, the lab members of a research
helper. The brain tells speakers apart (API key / user field) but remains
one brain with one memory.

## Decision: reference brain

**The home assistant** is the first brain and drives which building blocks
exist in v1. Chosen over the research-lab helper because it exercises both
differentiators — memory and initiative — with the fewest heavy
dependencies (no Elasticsearch/vector/vision stack on day one).

## Decision: building-block taxonomy

Every building block plays one of three roles in how a brain runs:

- **Triggers** start a pipeline run: `chat` (the standard API request —
  not special, just the most common trigger), `webhook`, `cron`. Brains
  can install their own triggers at runtime; that is what makes
  initiative real.
- **Nodes** are the steps: prompt template, structured output
  (json-schema; validate first, repair-model only on mismatch), tool/HTTP
  call, conditionals, fan-out/join, and explicitly **reply** and
  **notify**. Background work is not a node type — it is the pipeline
  continuing after the reply node fires.
- **Context & effects** are ambient, visible to every node: memory,
  speaker identity, time/system awareness, model roles, outgoing
  channels.

**Model roles** (fast, smart, cheap…) are first-class: nodes reference a
role; which provider/model backs each role is deployment configuration.
This keeps brain code portable across providers.

## Decision: dynamism ladder

Behavior change lives in data whenever possible; structure change only
when data can't express it. Because graphs are runtime objects built by
code, dynamism comes in grades, none requiring engine features:

1. Fixed graph, dynamic data (memory) — most "learning".
2. Dynamic construction — brain code assembles subgraphs at runtime.
3. Self-installed triggers — the brain schedules its own future runs.
4. Self-modifying structure — the brain registers new pipelines for
   itself (skill learning, self-healing).

Levels 1–3 are v1 scope; all ten reference stories run on them. Level 4
is deliberately deferred pending its own discussion (persistence, audit,
rollback of learned skills). Engine constraint preserving it: graphs are
first-class values and registration is not restricted to startup.

## Reference stories

Ten home-assistant functionality stories define v1 coverage — each block
must appear in at least one (full text in `discussion.md`): persona chat
through any standard client; ambient memory; speaker identity; intent →
structured tool call; reply-then-finish with notification; webhook-driven
runs; scheduled and self-scheduled runs; time/system awareness; model
roles; parallel multi-stage reasoning behind one streamed reply.

## Decision: continuity — transcripts vs memory

The chat API is stateless: the client sends history each request, and the
engine keeps no server-side conversation. **Transcripts belong to the
client; durable facts belong to memory.** Memory is the only continuity
there is; the brain never assumes the client sent full history, and two
different clients share nothing except what memory carries.

## Decision: caller-declared tools and reasoning are the brain's concern

The interface is chat, and chat includes tool calls — a caller may declare
tools and receive tool-call responses; a brain may likewise emit `<think>`
blocks to explain its reasoning. Whether and how to honor caller tools or
expose reasoning is the **brain developer's choice**; the engine only
faithfully carries the chat protocol in both directions.

## Decision: background-job outcomes are not policed

The engine does not enforce "every background continuation ends in a
notification." Whether a failed background job notifies the user is a
per-story, per-brain decision by the brain developer. The concern (a
promise followed by silence) is real and is documented in an authoring
guide, not enforced by the engine. The home-assistant reference brain
*does* notify on background failure.

## Decision: notification channel v1 = outgoing webhook

The one built-in channel is an HTTP POST to a configured URL — it composes
with any relay (Telegram bots, ntfy, …) without integrating them. The
channel concept is **explicitly open to extension**: channels are an
extensible family, outgoing webhook is merely the first member.

## Decision: v1 API surface

Chat completions (OpenAI-compatible) and messages (Anthropic-compatible),
both with streaming, plus `/models` listing the single brain. No voice,
vision, or realtime endpoints in v1 — senses remain a roadmap faculty, not
a v1 promise. Sampling parameters clients send (temperature, etc.) are
accepted, never an error, and exposed to the brain as context. Recorded
here so future work doesn't silently widen or shrink this contract.

## Decision: persistence

One line: **memory and installed triggers always survive; background jobs
survive as intent and re-run rather than resume; storage is pluggable with
a zero-setup default.** The three kinds of engine-owned state carry three
different promises:

- **Memory** — survives unconditionally. Memory *is* the product.
- **Self-installed triggers** — survive. Initiative that evaporates on
  restart is fake initiative. (Config-defined triggers need no durability;
  they reappear from brain code on startup.)
- **Background jobs** — *durable intent, not durable execution*: the job
  record survives and re-runs from the start after a crash (at-least-once);
  partial execution is not resumed. Queued work is never silently lost,
  but brain developers must write background pipelines to tolerate a
  duplicate run (authoring-guide material). Durable mid-pipeline
  resumption (workflow-engine territory) is a possible future grade, like
  the dynamism ladder — not v1.

The promise is *what* survives, never *where* it is stored: all three live
behind engine-owned interfaces with pluggable backing. Default backing
requires zero setup — one binary, durability included. Externalizing these
interfaces is what enables the provider/stateless-brain deployment: same
brain code, tenant-keyed external state.

## Decision: build order — vertical slices in story order

Blocks are built as vertical slices, each making a reference story pass
end-to-end — never layer-by-layer. The order (rationale in
`discussion.md`):

1. **Story 1** — walking skeleton: chat trigger, prompt + model-call +
   reply nodes, model roles, streaming, OpenAI-compatible serving.
2. **Story 4** — structured output, tool node, conditionals.
3. **Stories 2+3** — memory interface (zero-setup default) + speaker
   identity.
4. **Story 5** — post-reply continuation, outgoing-webhook channel,
   durable-intent job store.
5. **Stories 6+7** — webhook and cron triggers, self-installed triggers.
6. **Stories 8+10** — context injection, fan-out/join (9 falls out of
   slice 1's roles).

Anthropic-messages compatibility is deferred until after slice 2; one
client protocol proves the boundary.

There are no open product questions; how the code is organized lives in
`IMPLEMENTATION.md`.
