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

## Outcome

Decisions locked in `PRODUCT.md`: code-first authoring, one brain per
process, and the home assistant as the reference brain that drives which
building blocks are created first.
