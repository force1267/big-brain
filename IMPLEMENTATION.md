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
  `internal/`":** the reference brain `cmd/homeassistant` uses only
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
cmd/homeassistant/ reference brain; pkg-only; ~30 lines
```

Naming per Effective Go: short lower-case package names, no stutter —
`brain.Graph`, `model.Role`, `serve.Serve` (call sites read well).

## Build order (decided in PRODUCT.md)

Vertical slices in story order: 1 → 4 → 2+3 → 5 → 6+7 → 8+10. Each slice
is done when its story passes end to end against a real off-the-shelf
client. Never build a layer (all nodes, all triggers) ahead of the slice
that needs it.

**Slice-1 definition of done:** `cmd/homeassistant` compiles from `pkg/`
alone; `curl` (or any OpenAI SDK) against it returns a streamed persona
reply; `/models` lists the one brain; `gofmt` clean, `go vet` clean,
tests green.

**Write the author code first.** Each slice starts by writing (or
extending) `cmd/homeassistant` as the spec the engine must satisfy, then
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
  values from env vars prefixed `WRAPPER_` (12-factor). Role→model
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
