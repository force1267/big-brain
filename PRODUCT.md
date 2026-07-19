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

## Open (next discussion)

- Which building blocks to create first, derived from what the home
  assistant brain actually needs from `pkg/`.
