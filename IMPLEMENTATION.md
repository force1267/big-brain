# IMPLEMENTATION

The bridge between `PRODUCT.md` (what big-brain is) and the code (how it
is built). Read `PRODUCT.md` first; read `docs/effective-go-rules.md` and
`CLAUDE.md` before writing any Go. This file records the implementation
requirements and guidance that follow from the product decisions — it
does not restate them.

## Shape of the system

One process, one binary, authored by the brain developer:

```
brain author's main.go
  └── imports pkg/...        builds the graph, calls Serve
        └── engine (library) owns HTTP, triggers, run loop, state
              └── internal/  wire formats and guts, invisible to authors
```

- **big-brain is a library.** There is no engine executable that loads
  brain artifacts. The author's program *is* the deployable binary.
- **`pkg/` is the whole author-facing surface.** A brain author is an
  external module; Go forbids importing `internal/`. Anything a brain
  needs to compile — graph types, nodes, triggers, model roles, serve —
  must be exported from `pkg/`.
- **`internal/` holds only what runs behind the pkg API** and that we
  want to change without breaking authors. First occupant:
  `internal/openai` (chat-completions wire structs, SSE encoding,
  `/models` payloads). Extract more (e.g. a run executor) only when a
  pkg package outgrows its home — extraction later is mechanical, a
  premature boundary is not.
- **Deliberate deviation from CLAUDE.md's "initialization lives in
  `internal/`":** the reference brain `cmd/jarvis-demo` uses only
  `pkg/`, exactly like an external author would. It is executable
  documentation; if it needs private wiring, the pkg API is defective —
  fix the API. (Logged in `LOG.md`.)

## Package layout (slice 1; grow only when a slice demands it)

```
pkg/brain/         graph as runtime values: Node, Graph, Run; chat trigger;
                   prompt-template, model-call, reply nodes
pkg/model/         model roles (fast/smart/cheap...) + provider clients
                   (openai-go, anthropic-sdk-go) selected by deployment config
pkg/serve/         Serve(brain): OpenAI-compatible chat completions +
                   streaming + /models
internal/openai/   wire types + SSE — authors never see these
cmd/jarvis-demo/   reference brain; pkg-only; ~30 lines
```

Naming per Effective Go: short lower-case package names, no stutter —
`brain.Graph`, `model.Role`, `serve.Serve` (call sites read well).

This box is the slice-1 snapshot, kept for history — later slices grew
`pkg/` well past it. Current layout, and why each package exists:

```
pkg/brain/    graph runtime: Node, Run, Brain, control flow, model calls,
              memory/background nodes. Composes model/memory/job/notify;
              owns none of their types. Carries no trigger concept beyond
              Chat and named Pipelines — no Webhooks/Crons fields; see
              pkg/serve below for how triggers actually get wired.
pkg/model/    Role indirection + Model interface + OpenAI client. Leaf:
              no deps on any other pkg/ package.
pkg/memory/   Memory interface + two implementations (OpenFile: recency;
              OpenLLM: one model call judges relevance over the full log).
              Leaf except for OpenLLM's dependency on pkg/model — the one
              interface→implementation edge that isn't a pure leaf,
              because judging relevance requires a model call.
pkg/job/      Store interface (durable, at-least-once) + Job (envelope +
              RunAt readiness gate + a free-form Source provenance tag).
              Leaf: no deps.
pkg/notify/   Channel interface (notify.go) + one file per implementation
              (webhook.go; Log lives with the interface as the always-
              available fallback). Leaf: no deps.
pkg/cron/     Cron declaration + Next(now) — pure schedule math. A plain
              utility function, not part of any interface; brain authors
              call it directly when composing their own scheduled
              triggers (see pkg/serve). Leaf: no deps, and no longer
              imported by pkg/brain or pkg/serve — only by whatever brain
              code chooses to use it.
pkg/serve/    HTTP serving, config loading, job runner. Exposes Enqueue —
              the one primitive every trigger, of any kind, ultimately
              calls — via two composable options: WithEndpoint (adds a
              route to the shared server) and WithBackground (runs a
              func at startup with Enqueue in hand). Webhook- and
              cron-shaped triggers are brain-author code built from
              these two primitives plus pkg/cron, not engine-owned
              trigger kinds — pkg/serve has no concept of either. The
              only pkg/ package that reaches into internal/ (wire
              formats, config, logging, telemetry) and the only one that
              picks concrete implementations (OpenFile, Webhook, OpenAI)
              from env config.
internal/     openai, anthropic (wire formats + SSE), config, logging,
              telemetry — invisible to brain authors, reachable only from
              pkg/serve and internal/app.
cmd/jarvis-demo/  reference brain; pkg-only.
cmd/cli/          thin entrypoint bridging internal/app to the OS.
```

No cycles: every edge points from `brain`/`serve` down to a leaf, never
back up, and `internal/` never imports `pkg/`. `pkg/brain` composing
model/memory/job/notify — and nothing composing `brain` except `serve` —
is the deliberate shape: infrastructure stays ignorant of pipelines,
business logic (`brain`) stays ignorant of HTTP and config, and only
`serve` is allowed to know about both. `pkg/cron` sits outside this
composition entirely now — it's a utility any brain author's own code may
import, not a dependency of the engine.

## Build order (decided in PRODUCT.md)

Vertical slices in story order: 1 → 4 → 2+3 → 5 → 6+7 → 8+10. Each slice
is done when its story passes end to end against a real off-the-shelf
client. Never build a layer (all nodes, all triggers) ahead of the slice
that needs it.

**Slice-1 definition of done:** `cmd/jarvis-demo` compiles from `pkg/`
alone; `curl` (or any OpenAI SDK) against it returns a streamed persona
reply; `/models` lists the one brain; `gofmt` clean, `go vet` clean,
tests green.

**Write the author code first.** Each slice starts by writing (or
extending) `cmd/jarvis-demo` as the spec the engine must satisfy, then
building the pkg surface until it compiles and the story passes.

## Requirements carried from product decisions

- **Graphs are first-class runtime values** and registration is not
  restricted to startup (keeps dynamism level 4 possible). Node bodies
  are author-supplied Go functions; the engine never interprets a data
  format.
- **Chat is just a trigger.** The run loop must not special-case the
  HTTP request; `reply` is a node that streams to the caller, and the
  pipeline may continue after it fires. Design the run model for this in
  slice 1 even though continuation ships in slice 4.
- **Model roles are indirection**: nodes name a role; deployment config
  (env) binds roles to provider+model. No provider names in brain code.
- **Protocol fidelity**: sampling params are accepted, never an error,
  and surfaced to the brain as context; caller tools and `<think>`
  blocks pass through untouched — honoring them is brain code's choice.
- **State behind interfaces** (memory, installed triggers, job store):
  engine-owned pluggable interfaces with a zero-setup default backing.
  Promises: memory and installed triggers survive restarts; jobs are
  durable intent (at-least-once, re-run from start). Arrives in slices
  3–5; nothing in earlier slices may assume in-process-only state.

## Repo rules that bind every package (from CLAUDE.md)

- `effective.go` in every package: comments only, what the package is
  and why Effective Go justifies it.
- Interfaces ≤3 methods (≤2 active, ≤1 marker), defined where used;
  every package exporting an interface ships `mock.go`.
- Sentinel errors per package (`var ErrX = errors.New(...)`), wrap with
  `fmt.Errorf("%w: %w", ErrX, err)`, context added at every layer.
- `logrus` for logs; `viper` for config; all environment-dependent
  values from env vars prefixed `BIG_BRAIN_` (12-factor). Role→model
  binding is the first such config.
- OTel metrics: noop/stdout locally, OTLP in production, selected by
  env; metric-bearing interfaces get `MonitoredX` wrappers in the
  package's `telemetry.go`, returned by `New...` when telemetry is on.
- Tests: happy, unhappy, edge, every branch — quality over coverage.
- Every session appends to `LOG.md`.

## Non-goals (do not build)

Voice/vision/realtime endpoints; Anthropic messages API before slice 2
is done; multi-tenancy or provider features; durable mid-pipeline
resumption; a graph file format; a node-type registry "for later."
