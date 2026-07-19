# Project log

Append-only. Every LLM session records what it did here so later sessions
can understand the project's history. Newest entry last.

## 2026-07-19 — Project initialization (session 1)

Input: `init.md`. Goal: initialize an LLM-friendly Go project skeleton — no
product source files yet, only agent rules, docs, cross-cutting concerns.

Done:

- `go mod init github.com/itsmjasadi/wrapper` (Go 1.26). Module path was a
  placeholder — renamed in session 2, see below.
- Researched tech stack, wrote `docs/research.md`. Choices: stdlib `net/http`
  for serving, `coder/websocket` for realtime, official `openai/openai-go` +
  `anthropics/anthropic-sdk-go` for consuming providers and for wire-compatible
  request/response types when *serving* those APIs, logrus + viper + otel for
  cross-cutting. All permissive licenses.
- Downloaded Effective Go to `docs/effective_go.html`; distilled it into the
  enforceable rule set `docs/effective-go-rules.md`.
- Wrote `CLAUDE.md`: all agent rules from init.md (logging in this file,
  markdown artifacts, effective.go per package, ≤3-method interfaces,
  mock.go per exported interface, error wrapping style, MonitoredX telemetry
  wrapper convention, thin main, internal/ vs pkg/).
- Read `~/Desktop/projects/gateway/src` for style influence (cerr sentinel
  errors, viper env prefix, otel graceful shutdown). This project improves on
  it: no global config state (Loader returns a value), providers behind
  interfaces with mocks, errors double-wrapped (`%w: %w`).
- Created packages, each with `effective.go`, `mock.go`, full-branch tests:
  - `internal/config` — viper env loader (`WRAPPER_` prefix, 12-factor),
    `Loader` interface, validation of `WRAPPER_ENV` (local|production).
  - `internal/logging` — logrus level/format init, `Initializer` interface.
  - `internal/telemetry` — `Provider` interface; noop when disabled (local),
    OTLP gRPC metrics when enabled (production). Documents the MonitoredX
    convention for future metric-bearing packages.
  - `internal/app` — pure wiring (`Runner`): config → logging → telemetry →
    block until signal; telemetry shutdown deferred, shutdown errors logged
    not returned.
  - `cmd/wrapper` — thin `main`: flags, signal context, `app.New().Run(ctx)`.
- `gofmt`, `go vet`, `go build`, `go test ./...` all pass.

Environment quirk: the shell's `GOPROXY` resolves to an empty list; go
commands that fetch modules need `GOPROXY=https://proxy.golang.org,direct`
prefixed. Not persisted with `go env -w` on purpose (user's machine config).

Deliberate deferrals (marked with `ponytail:` comments in code):

- No HTTP server / routes yet — product features are the next step.
- `cmd/wrapper` has a single implicit `serve` command; add a command switch
  when a second command exists.
- `pkg/` is empty; the embeddable library surface gets designed with the
  product.

Next step (per init.md): discuss features, discover the product, then build.

## 2026-07-19 — GitHub publish (session 2)

- User created https://github.com/force1267/big-brain — the project's home.
- Renamed module `github.com/itsmjasadi/wrapper` → `github.com/force1267/big-brain`
  (go.mod + all imports); build and tests still green.
- `git init` (branch `main`), remote `origin` → the repo, initial commit pushed.
- Local directory is still named `wrapper/`; the repo name is `big-brain`.

Next: product discovery (unchanged).

## 2026-07-19 — Product discovery (session 3)

Discussed `discussion.md` with the user; decisions captured in `PRODUCT.md`:

- Core framing: an agent disguised as a model, behind OpenAI/Anthropic APIs.
- Brains are authored **library-first as Go programs** against `pkg/`; graph
  is a runtime object. File-format brains and remote "small-brain" topology
  are deferred, expressible later as loaders/node types.
- **One process serves one brain** (vLLM, not OpenAI). Multi-user = speaker
  identity within one brain; being a provider is out of scope.
- Reference brain: **home assistant** (exercises memory + initiative with
  the fewest dependencies).

Next: choose the first building blocks from what the home-assistant brain
needs from `pkg/`.

## 2026-07-19 — Building blocks & dynamism (session 3, continued)

- Wrote 10 home-assistant functionality stories covering all v1 blocks
  (discussion.md); PRODUCT.md summarizes them.
- Decided the block taxonomy: **triggers** (chat/webhook/cron, brains can
  install their own), **nodes** (prompt template, structured output with
  validate-then-repair, tool call, conditionals, fan-out/join, explicit
  reply and notify), **context & effects** (memory, speaker identity,
  time/system, model roles, channels). Model roles are first-class.
- Decided the dynamism ladder: (1) dynamic data, (2) dynamic construction,
  (3) self-installed triggers, (4) self-modifying structure. 1–3 in v1;
  4 deferred pending persistence/audit/rollback discussion. Engine keeps
  it possible: graphs are first-class values, registration not limited to
  startup.

Next: rank which building blocks get built first.

## 2026-07-19 — Pre-build double-check (session 3, continued)

Re-walked the ten stories for hidden assumptions; five decisions recorded
in PRODUCT.md (transcripts vs memory, caller tools/`<think>` as brain
developer's concern, background-failure notification as guidance not rule,
outgoing-webhook channel open to extension, exact v1 API surface). One
question deliberately left open and under discussion: **persistence** —
what engine-owned state survives restarts and what the product promises.

Next: settle persistence, then rank building blocks.

## 2026-07-19 — Persistence settled (session 3, continued)

Decision recorded in PRODUCT.md: memory and self-installed triggers always
survive restarts; background jobs survive as intent (at-least-once re-run,
no mid-pipeline resumption in v1); storage pluggable behind engine-owned
interfaces with a zero-setup default — which also enables the
provider/stateless-brain deployment.

**Next topic (agreed, written down so no session loses it): ranking which
building blocks get built first — the gate between product discovery and
building.**

## 2026-07-19 — Build order settled; IMPLEMENTATION.md created (session 4)

- Clarified the authoring model against a late worry: there is one
  binary; big-brain is a library, the author's program is the
  executable, node bodies are Go closures — no hidden static graph, no
  inter-process protocol (that only appears in the deferred remote-node
  variant). Recorded in discussion.md.
- Decided build order: vertical slices in story order 1 → 4 → 2+3 → 5 →
  6+7 → 8+10; Anthropic API after slice 2. Recorded in PRODUCT.md
  (closes the last open product question).
- Decided pkg/ vs internal/ split: everything author-facing in pkg/
  (external modules can't import internal/); internal/openai first for
  wire types/SSE. Deliberate deviation from the "initialization lives in
  internal/" rule: cmd/homeassistant uses only pkg/, since it is
  executable documentation for external authors.
- Created IMPLEMENTATION.md — the bridge between PRODUCT.md and code:
  layout, slice plan with slice-1 definition of done, author-code-first
  workflow, requirements carried from product decisions, binding repo
  rules, non-goals.

Next: write cmd/homeassistant for slice 1 (the spec), then build the
pkg/ surface until story 1 passes.

## 2026-07-19 — Slice 1 built: story 1 passes end to end (session 4, continued)

Author code written first (`cmd/homeassistant`, pkg-only, ~35 lines), then
the surface to satisfy it:

- `pkg/model` — provider-neutral Message/Params/Chunk, Role indirection,
  `Model` interface (1 method), OpenAI-compatible backing via
  openai-go/v3 (new direct dep, per CLAUDE.md tech choices), `Mock`.
- `pkg/brain` — `Brain`, `Run`, `Node` (1 method) + `Func` adapter
  (http.HandlerFunc-style), `Execute`, built-in nodes `Prompt`
  (text/template), `Call(role)`, `Reply` (streams to Emit; pipeline may
  continue after it).
- `pkg/serve` — `Run` (config+logging+role binding+graceful server) and
  exported `Handler` for tests/embedding; streaming (SSE) and
  non-streaming chat completions, `/v1/models`, OpenAI-shaped errors.
- `internal/openai` — wire types, SSE encoding; no mock (pure encoding).
- `internal/config` — added `WRAPPER_MODELS` (role=model pairs),
  `WRAPPER_UPSTREAM_BASE_URL/API_KEY`, sentinel `ErrInvalidModels`.

Verified: `go build`/`vet`/`gofmt` clean, all package tests green (happy/
unhappy/edge per node and handler), and a live smoke test — the
homeassistant binary against a fake OpenAI upstream answered `/v1/models`,
non-streaming, and SSE streaming via plain curl.

Deviations, deliberate: reference brain's init lives in main via
`serve.Run`, not `internal/` (it documents the external-author path);
no `telemetry.go` wrappers yet — no interface is metric-bearing in slice 1,
first candidate is `model.Model` when OTel wiring reaches pkg/.

Next: slice 2 (story 4) — structured output, tool node, conditionals.
