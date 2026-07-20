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

## 2026-07-19 — Slice 2 built: story 4 passes end to end (session 4, continued)

- `pkg/brain`: `Run.Vars`/`SetVar` + generic `Var[T]` (per-run state —
  nodes are shared by concurrent runs, so state must live on the Run,
  never in closed-over variables); `If(cond, then, els)`; `Seq(...)`;
  `Extract[T](role, instruction, key)` — structured output that sends a
  zero-value shape hint, strictly decodes (unknown fields rejected,
  tolerates prose/fences around the object), and makes exactly one
  repair round-trip only on mismatch (per PRODUCT.md). Extraction
  deliberately ignores caller sampling params.
- `pkg/model`: `Collect(stream)` helper; Mock gains `Script`/`Calls`
  for multi-call sequences.
- `cmd/homeassistant`: story-4 pipeline — Prompt → Extract intent →
  If(add_guest → addGuest tool) → Call → Reply. The tool is a plain Go
  closure POSTing to JARVIS_DOOR_URL and appending the tool result as a
  system message; no tool framework, code-first as decided.
- Verified: all tests green (happy/repair/failed-repair/prose-wrapped/
  unbound-role/branching); live end to end against LM Studio gemma
  (localhost:1234): "add my friend John…" → door endpoint received
  {"name":"John"}, Jarvis confirmed in persona; plain chat unaffected.
- Note: earlier LAN-IP timeouts were environmental (sandbox blocks LAN
  dials, allows loopback); localhost upstream works.

Next: slice 3 (stories 2+3) — memory interface with zero-setup default,
speaker identity from the API credential.

## 2026-07-19 — Slice 3 built: stories 2+3 pass end to end (session 4, continued)

- `pkg/memory` — the first persistence promise: two-method `Memory`
  interface (Remember/Recall), `Fact{Speaker,Content,At}`, zero-setup
  default `OpenFile` (append-only JSONL, fsync per fact, loaded on open;
  recall = most-recent-N, model judges relevance — vector store is a
  future second implementation). Mock included.
- `pkg/brain` — `Run.Speaker` and `Run.Memory` (ambient context per the
  taxonomy); `RecallFacts(limit)` injects tagged facts as a system
  message; `Memorize(role)` — ambient memory: the pipeline decides what
  is worth keeping (Extract under the hood), stores for the current
  speaker. Reference brain places Memorize after Reply.
- `pkg/serve` — Handler now takes memory + speakers; speaker resolved
  from Authorization bearer key via WRAPPER_SPEAKERS (unknown/missing
  key = anonymous, never an error). serve.Run opens the file store at
  WRAPPER_MEMORY_PATH (default memory.jsonl).
- `internal/config` — Memory.Path, Speakers; parseModels generalized to
  parsePairs.
- Verified: all tests green (persistence across reopen, corrupt file,
  limits, speaker filtering, memorize decide/skip/fail paths, bearer
  resolution); live against gemma-4-e4b: vegetarian fact remembered
  ambiently and shaping dinner after a process restart; kid's dentist
  appointment recalled per speaker.
- Known limitation observed live: the e4b model skipped memorizing one
  fact and blurred cross-speaker attribution once; wording of the
  memorize/recall prompts tightened in response. Real deployments should
  bind these stages to a stronger role (that is what roles are for).

Next: slice 4 (story 5) — post-reply continuation surviving the HTTP
response, outgoing-webhook channel, durable-intent job store.

## 2026-07-19 — Slice 4 built: story 5 passes end to end (session 4, continued)

The hardest engine slice: initiative made real.

- `pkg/job` — durable intent: `Job{ID, Pipeline, Speaker, Payload, At}`
  names a registered pipeline plus a serializable payload; two-method
  `Store` (Enqueue persists before acking; Sweep runs every pending job
  and marks it done even on failure — the attempt is what at-least-once
  promises, retry policy belongs to the brain). Zero-setup default:
  append-only JSONL add/done log; pending = adds without done, re-run on
  startup.
- `pkg/notify` — one-method `Channel`; v1 built-in `Webhook(url)` (HTTP
  POST of {speaker,text}); `Log()` fallback so an unconfigured channel
  never drops silently.
- `pkg/brain` — `Brain.Pipelines` (named pipelines: how durable jobs
  reference code-built graphs); `Go(pipeline, payload)` node persists
  intent; `Notify(tmpl)` node renders and sends; `Reply` now sets
  `Run.Replied` and refuses to run with no caller (`ErrNoReply`).
- `pkg/serve` — deps consolidated in `Deps`; the handler executes chat
  node-by-node and **closes the HTTP response the moment Reply fires**,
  detaching the remaining nodes (context.WithoutCancel) — "background"
  is literally the pipeline continuing after the reply. Job runner:
  startup sweep (crash recovery) + wake-on-enqueue; job failures logged,
  never engine-notified (PRODUCT.md: the brain chooses).
- Config: WRAPPER_JOBS_PATH (default jobs.jsonl), WRAPPER_NOTIFY_URL
  (empty = log channel).
- Reference brain: add-guest is now story 5 — chat replies "on it, I'll
  text you" after `Go("register-guest", …)` persists the intent; the
  background pipeline calls the door camera and notifies the outcome,
  including on failure (this brain's choice).
- Verified: all tests green (enqueue/sweep/reopen-recovery/failed-once,
  webhook channel statuses, Go/Notify nodes, detached post-reply nodes,
  runJob + startJobs recovery); live against gemma: "add Sarah…" →
  instant persona reply promising a text, jobs.jsonl add+done records,
  door camera got {"name":"Sarah"}, notify webhook got the completion
  text addressed to dad.

Next: slice 5 (stories 6+7) — webhook and cron triggers, self-installed
triggers (durable, per the persistence promise).

## 2026-07-19 — Slice 5 built: stories 6+7 pass end to end (session 4, continued)

Design move: every trigger firing enqueues a durable job — webhooks,
cron ticks, and self-installed future runs all reuse the slice-4 runner,
so durability comes free and one mechanism serves all triggers.

- `pkg/job` — `Job.RunAt` (zero = now; future = a self-installed
  trigger), `Job.Due`; `Sweep` now runs only due jobs, keeps the rest
  pending, and returns the earliest future due time. The runner arms a
  timer accordingly (deferred jobs fire with no external nudge) and
  deferred jobs survive reopen — self-installed triggers persist, per
  the PRODUCT.md promise, with no new store.
- `pkg/brain` — `Brain.Webhooks` (trigger name → pipeline),
  `Brain.Crons` (`Every` interval or `Daily "15:04"`; a cron-expression
  lib slots in later if ever needed), and `GoAt(when, pipeline,
  payload)` — the brain installing a trigger for itself.
- `pkg/serve` — `POST /triggers/{name}` verifies the trigger, decodes
  the JSON event, enqueues it (202; crash after accept still runs it);
  `startCrons` goroutines enqueue on schedule (config-defined crons need
  no durability — they reappear from brain code).
- Reference brain — story 6: webhook "door" → pipeline "unknown-face":
  recall facts, describe the camera event, Extract an open/alert
  verdict, Notify either way; register-guest now Remembers "X is on the
  door guest list" so the verdict has facts to stand on. Story 7:
  "party" intent → GoAt one-shot "party-prep" (JARVIS_PARTY_DELAY
  shortens for demos) + daily 21:00 "nightly-review" cron.
- Verified: all tests green (due/not-due sweeps, deferred-job reopen
  survival, timer-driven deferred execution, webhook route statuses,
  nextCron math incl. invalid spec); live against gemma: stranger at
  the door → alert notification; "add Leo" → registered + remembered →
  door sees Leo → "Door opened: Leo is explicitly listed…"; party
  message → self-installed reminder fired 10s later (run_at honored in
  jobs.jsonl).

Next: slice 6 (stories 8+10) — time/system context injection and
fan-out/join; then v1 is functionally complete minus the Anthropic
messages API.

## 2026-07-20 — Slice 6 + Anthropic API: all ten stories pass; v1 surface complete (session 4, continued)

- `pkg/brain` — story 8: `Situation(notes...)` node injects current
  date/time/weekday/timezone, who is speaking, and standing brain notes
  (quiet hours) as a system message — no per-request prompt plumbing.
  Story 10: `Parallel(nodes...)` fans out concurrently, joins, and
  errors.Join()s branch failures; branches write results via SetVar,
  which (with Var) is now mutex-guarded — the race detector validated
  this, and also caught job.Mock needing the same lock.
- `internal/anthropic` — messages wire format: string-or-blocks Content
  (UnmarshalJSON at the boundary), non-streaming response, the
  message_start/content_block_delta/message_stop SSE sequence, error
  bodies. `pkg/serve` routes POST /v1/messages over the same brain; the
  chat loop is factored into executeChat shared by both protocols;
  speakers resolve from x-api-key (Anthropic) or bearer (OpenAI).
- Bug found by live testing and fixed: a mid-stream pipeline failure
  wrote an error body onto an already-started SSE stream (superfluous
  WriteHeader); writeErr now no-ops once streaming has begun — the
  stream just truncates, on both protocols.
- Live quirk documented: with max_tokens≈100, LM Studio's gemma spends
  the entire budget on hidden reasoning and streams zero content tokens
  on BOTH protocols; the engine passes sampling params through
  faithfully by design, so this is upstream behavior, not engine loss.
- Verified: full suite green under -race; live against gemma — story 8:
  "is it too late to run the dishwasher?" at 23:57 answered "save that
  cycle for when the house wakes up" (quiet hours + clock, injected);
  story 10: party reply weaving parallel weather + RSVP results into
  one streamed answer; Anthropic /v1/messages streaming the correct
  event sequence with speaker from x-api-key.

All ten reference stories now pass end to end. v1 API surface (chat
completions + messages + streaming + /models) is complete.

## 2026-07-20 — Telemetry wrappers (session 4, continued)

Fulfilled the CLAUDE.md telemetry rule with the lazy-correct design: the
existing internal/telemetry Provider sets the *global* OTel meter
provider (noop when WRAPPER_TELEMETRY_ENABLED=false, OTLP gRPC when
true), so `Monitored` wrappers can be applied unconditionally in each
package's constructor — inert until telemetry is enabled, no config
plumbed into pkg/.

- Metric-bearing interfaces wrapped, each in its package's telemetry.go:
  - model.Monitored(m, name): model.calls (by outcome incl. rejected),
    model.call.seconds, model.chunks — tagged with the backing model.
    Applied in model.OpenAI.
  - memory.Monitored: memory.remembered, memory.recalls (by outcome).
    Applied in memory.OpenFile.
  - job.Monitored: job.enqueued, job.ran (by pipeline and outcome).
    Applied in job.OpenFile.
  - notify.Monitored: notify.sent (by outcome). Applied in
    notify.Webhook (Log() stays bare).
- serve.Run now owns the telemetry lifecycle: Start after logging,
  graceful Shutdown on exit.
- Instrument-creation failure falls back to the unwrapped value —
  metrics must never break the model path.
- Tests: delegation and error-propagation per wrapper; a ManualReader
  test on the model wrapper asserts all three instruments record; test
  cleanup restores a noop global provider (nil panics — learned by
  test). Full suite green under -race; live smoke with telemetry
  disabled unchanged.

## 2026-07-20 — Rename cleanup: jarvis-demo, cmd/cli (session 5)

Cosmetic cleanup, no behavior change: `cmd/homeassistant` → `cmd/jarvis-demo`
(it's a proof-of-concept reference brain, name should say so) and
`cmd/wrapper` → `cmd/cli` (generic entrypoint name). Updated all references
in code comments, README, IMPLEMENTATION.md, discussion.md. `go build ./...`
clean. Historical LOG.md entries above keep the old names as written — log is
append-only history, not live documentation.

## 2026-07-20 — Authoring guide + README overhaul (session 5, continued)

- `docs/authoring-guide.md` — the developer-facing manual for brain authors:
  mental model, quickstart, concepts (Brain, triggers, nodes/Run, ad-hoc
  Func nodes, ambient context, model roles, dynamism ladder), a full node
  reference table, one worked recipe per reference story, the WRAPPER_ env
  var table, testing guidance (mocks, direct Run construction, Handler +
  httptest), and a pointer to `cmd/jarvis-demo` as the end-to-end example.
- `CLAUDE.md` — added absolute rule 5: any change to a `pkg/` interface,
  exported signature, or core concept must update the authoring guide in
  the same change, not a follow-up.
- `README.md` — rewritten: product framing, a runnable 60-second demo
  (persona + ambient memory brain, curl against it, memory surviving across
  sessions), a faculties summary, build/run commands including both
  binaries, and a documentation map linking the new guide, PRODUCT.md,
  IMPLEMENTATION.md, LOG.md, CLAUDE.md, docs/research.md.

## 2026-07-20 — Env prefix WRAPPER_ → BIG_BRAIN_ (session 5, continued)

Renamed the 12-factor env prefix everywhere: `internal/config/config.go`
(`v.SetEnvPrefix`, comments), default `telemetry.service_name` from
"wrapper" to "big-brain", `config_test.go` (Setenv calls + default
assertion), and all doc references (README, CLAUDE.md, IMPLEMENTATION.md,
docs/authoring-guide.md, docs/research.md). Historical LOG.md entries above
keep the old name — append-only history. Full suite green.

## 2026-07-20 — Speaker binding moved out of engine config (session 6)

`internal/config` no longer parses BIG_BRAIN_SPEAKERS — speaker identity was
demo-specific, not an engine concern. `brain.Brain` gained a `Speakers
map[string]string` field (API key → speaker name); `serve.Run` reads it
from `b.Speakers` instead of config. `cmd/jarvis-demo` now populates it
itself via a local `speakers()` helper reading `JARVIS_DEMO_SPEAKERS` with
plain `os.Getenv` (no config package involvement, per its own env prefix).
Updated docs/authoring-guide.md (Brain struct, speaker-identity recipe,
config table). Full suite green.

## 2026-07-20 — Removed household-specific policy from pkg/ (session 7)

Audited pkg/ for library code that baked in home-assistant-specific
opinions rather than staying a general primitive. Found three and fixed
all: `Memorize` had a hardcoded "household rules" prompt const — now takes
`instruction string` like `Extract` does. `RecallFacts` had hardcoded
"household" wording and a fixed guidance sentence — now defaults to a
neutral "shared" label and takes `notes ...string` for domain guidance
instead of forcing any. `Brain.Speakers`/`Deps.Speakers` forced a specific
bearer/x-api-key + flat-map resolution scheme on every brain — replaced
with `Brain.ResolveSpeaker func(*http.Request) string`, a hook the engine
just calls; the credential scheme and identity source are entirely the
author's choice.

All the removed household wording (`memorizeInstruction`, `recallNote`) and
the bearer/x-api-key + env-var map resolution now live in
`cmd/jarvis-demo/main.go` only. Updated docs/authoring-guide.md (Brain
struct, node reference, memory + speaker-identity recipes, config section)
per CLAUDE.md rule 5. Full suite green.
