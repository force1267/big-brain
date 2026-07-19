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

## Open (next discussion)

- Ranking: which building blocks get built first.
