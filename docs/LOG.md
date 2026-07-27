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

## 2026-07-20 — Speaker concept fully removed from pkg/ (session 8)

Follow-up to the previous session's Speaker cleanup: removing the ability
to customize speaker resolution wasn't enough — the concept itself
(Run.Speaker, Brain.ResolveSpeaker, memory.Fact.Speaker, job.Job.Speaker,
notify.Message.Speaker) still lived in pkg/, meaning every brain paid for
it whether or not "speaker" made sense for its domain. Removed all of it:

- pkg/brain: Run has no Speaker field; Situation no longer mentions who's
  talking; RecallFacts lists Content/At only (no per-fact "who", no
  "current speaker" line); Memorize stores plain Fact{Content, At}; Go/
  GoAt/Notify no longer touch a Speaker field anywhere.
- pkg/memory.Fact, pkg/job.Job, pkg/notify.Message: Speaker field dropped
  from all three. Attribution, if a brain wants it, is the brain's
  encoding inside Content/Text — not an engine concept.
- pkg/serve: replaced Brain.ResolveSpeaker with a fully generic
  serve.WithPrepare(func(*http.Request, *brain.Run)) Run option — the
  engine calls it once per chat/messages request with the raw request and
  nothing else; it carries no notion of identity, just lets the author
  inject arbitrary context into Run.Vars via the same SetVar every node
  already uses.

cmd/jarvis-demo now owns the entire speaker concept: resolveSpeaker()
(Prepare hook, parses JARVIS_DEMO_SPEAKERS, calls run.SetVar("speaker",
name)), speakerOf() helper, announceSpeaker (replaces the old built-in
"You are talking to X" line), and a local memorize() that reimplements
brain.Memorize but tags fact Content with "[speaker] ..." — the household
household-attribution behavior now lives only here. registerGuest carries
speaker through job payload → Run.Vars, the same generic mechanism any
background job would use for any per-run value.

Updated docs/authoring-guide.md throughout (Brain/Run structs, Context &
effects, Notify reference row, memory + speaker-identity recipes, config
section) per CLAUDE.md rule 5. `grep -rn Speaker pkg/` now returns nothing.
Full suite green under gofmt/vet/test.

## 2026-07-20 — Dependency-graph audit acted on: 5 findings fixed (session 9)

Analyzed pkg/'s import graph and reasoned through five specific concerns
raised about package ownership and interface shape (not deferring to
IMPLEMENTATION.md — updated it where it was stale instead). Agreed and
implemented four; pushed back on a fifth with a lighter alternative:

1. **pkg/cron extracted.** `Cron{Every,Daily,Pipeline}` and the next-fire
   algorithm (`nextCron`, previously split across pkg/brain's type and
   pkg/serve's logic) now live together in a new leaf package, `pkg/cron`
   (`Cron`, `Next`). `Brain.Crons []cron.Cron` — brain declares crons the
   way it declares Webhooks, doesn't own the scheduling type.
2. **brain.Situation removed.** PRODUCT.md always classified time/system
   awareness as ambient *context*, not a *node* — the Node implementation
   was already a taxonomy mismatch, and its static `notes ...string` could
   never carry per-request dynamic content (the exact bug behind last
   session's speaker workaround). time.Now() needs no engine help; deleted
   the node, documented the five-line brain.Func recipe instead.
   cmd/jarvis-demo's `situation` function replaces it, folded together
   with the (already-custom) speaker announcement.
3. **job.Job.Source added** (partial agreement): rejected a `Trigger`
   interface as premature — every current trigger reduces to "run now" or
   "run at time T," and an interface with one implementation is exactly
   the speculative-generality trap. Added a plain `Source string`
   provenance tag instead ("cron", "webhook:door", "self") for
   logs/debugging, set at each enqueue site, no scheduling role.
4. **pkg/memory redesigned around a second implementation.** `Memory`'s
   doc no longer mandates "most recent, newest last" (that policy moved to
   OpenFile's own doc); `Recall` gained a `query string` param. Built
   `OpenLLM(path, model.Model)` — same append-only JSONL log as OpenFile,
   but Recall hands the whole log plus query to one model call that
   decides relevance (JSON array of fact indices, tolerant of
   prose-wrapped output like brain.Extract). This is the first pkg/
   leaf-to-leaf edge (memory → model) — justified because judging
   relevance genuinely requires a model call, unlike the other leaves.
   RecallFacts now passes the latest message's content as query.
5. **pkg/notify split.** notify.go keeps Message/Channel/ErrSend/Log;
   webhook.go holds the Webhook implementation — mirrors the file.go split
   pkg/memory and pkg/job already used, so multiple implementation files
   next to a slim interface file signal "extensible" on sight.

IMPLEMENTATION.md's package-layout section (frozen at slice 1) got a
"current layout" addendum instead of being rewritten — it's meant as
history, but was silently missing every package added since, which risked
misleading a reader about pkg/'s actual shape.

Verified after each change: no import cycles (internal/ never imports
pkg/; leaves never import each other except the new memory→model edge);
full suite green under go vet + go test -race; gofmt clean.

## 2026-07-20 — memory.Recall's limit moved out of the interface (session 9, continued)

Follow-up correction: `limit` had been added to `Memory.Recall` in the
previous session's redesign, but it's an implementation detail (how many
facts a given backing keeps around) masquerading as an interface contract
— OpenFile and OpenLLM both just capped a slice with it, nothing about
"limit" is generic across arbitrary future backings the way `query` is.

- `Memory.Recall(ctx, query string) ([]Fact, error)` — no limit.
- `OpenFile(path string, limit int)` and `OpenLLM(path string, m
  model.Model, limit int)` — each implementation takes its own cap at
  construction time instead of per call.
- `brain.RecallFacts(notes ...string) Node` — dropped its `limit int`
  first argument; nothing left to pass through.
- `internal/config`: new `Memory.Limit` (env `BIG_BRAIN_MEMORY_LIMIT`,
  default 50), threaded into `serve.Run`'s `memory.OpenFile` call — the
  zero-setup default's cap is now real deployment config, not a
  hardcoded pipeline argument repeated at every `RecallFacts` call site.
- cmd/jarvis-demo, README, docs/authoring-guide.md updated to match.

Full suite green under go vet + go test -race; gofmt clean.

## 2026-07-20 — Memory backend selection wired into serve.Run (session 9, continued)

Renaming BIG_BRAIN_MEMORY_LIMIT surfaced a real gap: OpenLLM had no way to
ever be selected by serve.Run, so a bare "LLM_LIMIT" env var would have
been dead config. Wired proper backend selection instead:

- `BIG_BRAIN_MEMORY_BACKEND` (`file` default, or `llm`) picks the memory
  implementation serve.Run opens.
- `BIG_BRAIN_MEMORY_FILE_LIMIT` / `BIG_BRAIN_MEMORY_LLM_LIMIT` (both
  default 50) replace the old single `BIG_BRAIN_MEMORY_LIMIT` — each
  backend's cap is independently configurable.
- `BIG_BRAIN_MEMORY_LLM_ROLE` (default `fast`) names which bound model
  `OpenLLM` calls to judge relevance; unknown role now fails startup
  clearly (new `serve.ErrNoMemoryModel`) instead of silently doing
  nothing.
- `internal/config`: new `Memory.Backend`/`FileLimit`/`LLMLimit`/`LLMRole`
  fields, `MemoryBackendFile`/`MemoryBackendLLM` constants, validated
  against `ErrInvalidMemoryBackend`.

Updated docs/authoring-guide.md's config table and memory recipe. Full
suite green (added config tests for the llm-backend path and invalid
backend rejection).

## 2026-07-20 — Live test against local gemma-4-e4b via LM Studio (session 10)

Ran cmd/jarvis-demo against a real local model (LM Studio, google/gemma-4-e4b,
http://localhost:1234) to smoke-test everything built/refactored this
session. All ten reference stories passed, both protocols verified:

- Story 1 (persona chat): passed, stream and non-stream.
- Story 2 (ambient memory): "we're vegetarian now" → memorized, tagged
  [dad] → later "what's for dinner" shaped a vegetarian answer unprompted.
- Story 3 (speaker identity): dad and kid each set a dentist appointment;
  each recalled only their own on asking, no cross-contamination.
- Story 4+5 (intent → tool call, finish later): "add John" → instant "on
  it" reply, background job (Source: "self"), door-camera call, fact
  recorded, completion notification delivered.
- Story 6 (reacting to the world): POST /triggers/door for a listed guest
  → "Door opened"; for a stranger → "Alert" — verdict correctly grounded
  in recalled guest-list facts.
- Story 7 (acting on schedule): self-installed reminder fired on schedule
  (Source: "self"); daily cron wiring confirmed structurally (not
  live-waited — 21:00 daily).
- Story 8 (time/situation awareness): correct real-world time reported,
  reasoned about quiet hours via the demo's own situation func.
- Story 9 (model roles): implicit throughout — "fast" role bound to gemma,
  no provider name in brain code.
- Story 10 (parallel fan-out): weather + RSVP checks ran concurrently,
  wove into one reply.
- Both protocols: /v1/chat/completions and /v1/messages, stream and
  non-stream, correct SSE event sequences on both.

One environmental quirk reproduced (already documented, not a bug):
gemma spends a small max_tokens budget entirely on hidden reasoning over
the Anthropic path, returning empty content — resolved by raising
max_tokens; sampling params pass through faithfully by design.

Follow-up from live-testing friction: the demo needed two hand-rolled
Python HTTP servers (door camera, notify relay) to exercise stories 4-6
meaningfully. Fixed by making cmd/jarvis-demo fully self-contained:
startDummyWorld serves both stand-ins on a second port
(JARVIS_DEMO_DUMMY_ADDR, default :8090), logged inline in the brain's own
log; JARVIS_DOOR_URL and BIG_BRAIN_NOTIFY_URL default to it unless a
deployer overrides them with real endpoints. `go run ./cmd/jarvis-demo`
now exercises every story with nothing else to stand up. Verified live
again against gemma after the change: same story-4/5 flow worked with
zero manual servers, both dummy hits logged inline.

## 2026-07-20 — Triggers decomposed into composable primitives, no Trigger interface (session 11)

Multi-round design discussion (full narrative in discussion.md's new
"Post-build: dependency-graph audit and the trigger redesign" section).
Two Trigger-interface designs were proposed and rejected in review — one
for putting ctx-blocking responsibility on implementers with no
enforcement, the other for forcing an empty no-op Start on webhook
triggers (a design smell: the interface's primary method had no business
logic for that shape). Landed instead on: there's no need for a Trigger
interface at all. "Start a pipeline" was already a primitive (Enqueue),
just never exposed outside serve.Run.

- `pkg/brain`: removed `Brain.Webhooks map[string]string` and `Brain.Crons
  []cron.Cron`. `Brain` no longer imports pkg/cron at all — it carries no
  trigger concept beyond `Chat` and named `Pipelines`.
- `pkg/serve`: exported `Enqueue` as a named type; added `WithBackground(fn
  func(ctx, enqueue))` (runs fn once at startup, for any non-HTTP trigger
  source) and `WithEndpoint(pattern, build func(enqueue) http.HandlerFunc)`
  (adds a route to the shared server, handler built once Enqueue exists).
  Removed `startCrons` and the dedicated `/triggers/{name}` handler —
  pkg/serve now has zero concept of "webhook" or "cron" as trigger kinds.
- `pkg/cron` untouched (still a pure `Cron` + `Next`, zero deps) but no
  longer imported by the engine anywhere — confirmed via `go list`: only
  `cmd/jarvis-demo` imports it now.
- `cmd/jarvis-demo`: door-camera webhook and nightly-review cron rebuilt
  as `doorWebhook`/`nightlyReview` — a few lines of the brain's own code
  composing `Enqueue` with an HTTP route or `cron.Next`, passed to
  `serve.Run` via `WithEndpoint`/`WithBackground`. Live-verified against
  gemma-4-e4b again: door webhook trigger still fires the unknown-face
  pipeline correctly end to end.
- Separately: resolved the "is notify.Webhook misnamed" question by
  checking how Stripe/GitHub/Slack actually use the term (an outgoing
  event-POST to a subscriber URL is the textbook use, not the atypical
  one) — decided to keep the name and disambiguate in prose only
  ("incoming"/"outgoing"), the same move Slack made for its own Incoming/
  Outgoing Webhooks. PRODUCT.md already did this correctly;
  docs/authoring-guide.md's trigger sections rewritten to match and to
  drop all Brain.Webhooks/Brain.Crons references.
- A third primitive — a node pausing mid-pipeline for an inbound HTTP
  callback (`Run.Await`) — surfaced but deliberately deferred to its own
  design pass; it's a different problem (dynamic route registration/
  demuxing against a static `http.ServeMux`), not a trigger variant.

Updated IMPLEMENTATION.md's "current layout" addendum (pkg/serve's
description, pkg/cron's now-zero engine dependents) and
docs/authoring-guide.md's Triggers section, Brain struct, and story 6/7
recipes. Full suite green under gofmt/vet/test -race; live door-webhook
trigger re-verified against a real model.

## 2026-07-22 — Architecture reset: IMPLEMENTATION.md rewritten (functions, not node graphs)

Input: CRITIQUE.md (hard pass over pkg/ + jarvis-demo) and new-arch.md.
Conclusion of the discussion: the node-graph DSL ([]Node, brain.If, Seq,
Parallel, Vars map[string]any) is the accidental DSL PRODUCT.md rejected;
scrapped in place. IMPLEMENTATION.md fully rewritten around one decision:
a pipeline is a plain Go function the engine calls, not data it
interprets. Key settled points:

- Two handle types: *bb.Turn (chat; has Reply) vs *bb.Job (background;
  no Reply — compile error replaces the old Replied flag).
- Durable work via bb.Task registration → typed TaskRef; Later/At take
  function refs with one JSON-able payload arg, killing stringly
  pipeline names and map[string]any.
- Sessions: NEW opt-in primitive — durable KV per author-chosen key,
  backed by persistors; transcripts still belong to the client.
- ctx.Value only for cross-cutting request values (speaker identity);
  business data rides typed args, never ctx (rejected new-arch.md's
  ctx-as-data-bus on Go-doc grounds).
- Triggers: bb.Every (crontab syntax, engine-owned loop — closes the
  WithBackground ctx footgun), bb.OnHTTP, supervised bb.Go escape hatch
  (joined on shutdown).
- pkg/persist: one durability substrate under jobs/sessions/schedules;
  memory/file default, redis later. pkg/brain, pkg/serve, pkg/job,
  pkg/cron slated for deletion; internal/ wire code salvaged (with the
  role-coercion/tool-call fidelity bugs fixed).
- Build order unchanged: same ten stories, vertical slices, demo first.

Product decisions untouched. Code not yet changed — next session starts
slice 1 against the new surface. authoring-guide.md will be rewritten
as the code lands (docs move with code).

## 2026-07-22 — jarvis-demo rewritten as pseudocode spec; prior-art survey

- Rewrote cmd/jarvis-demo/main.go against the new pkg/bb surface from the
  rewritten IMPLEMENTATION.md. It is deliberately non-compiling pseudocode
  (pkg/bb does not exist yet) — the spec slice work must satisfy. It
  forced concrete API decisions: bb.New + registration methods (not
  ctor options), Brain.Later for handlers outside Turn/Job, t.System /
  t.LastMessage / memory.Format / bb.Messages helpers, Prepare returning
  ctx, Every(crontab, ref, payload).
- User still not fully happy with the shape; requested a survey of prior
  art. Written to docs/prior-art.md with verbatim interface examples:
  Open WebUI Pipes + Letta (same product idea), Genkit Go + Pydantic AI +
  DSPy + OpenAI Agents SDK (plain-function camp), Eino + Haystack +
  Mastra + LangGraph/LlamaIndex Workflows (graph-DSL camp), Inngest +
  Temporal + Restate (durable execution). Conclusions: plain-function bet
  is mainstream; DSL⇄durability is the real axis; our facade+faculties+
  guarantees combination is unoccupied; concrete steals noted (Letta
  memory tiering, Inngest step-run upgrade path, Restate virtual objects
  ≈ Session).

## 2026-07-23 — front-end research: drag-and-drop brain builder

- Wrote docs/research-graph-ui.md: survey of node-editor libraries for a
  no-code graph builder/visualizer for brains.
- Recommendation: React + Vite + React Flow (@xyflow/react), dagre (ELK
  later for nested subgraphs), Zustand (+zundo for undo), zod-validated
  graph JSON owned by us (not the library's shape), CodeMirror 6 for code
  fields, JSON-Schema-driven inspector forms, shadcn/ui.
- Rejected: Rete.js (ships a runtime we don't need — Go executes graphs),
  LiteGraph (canvas-drawn nodes make internals painful), Blockly (wrong
  metaphor), JointJS/GoJS (licensing/weight).
- Debug/replay: Go side must emit structured run events from day one; the
  debugger is a scrubber over that event list rendered as an overlay on the
  same canvas. SSE for live runs.
- Deferred: multiplayer (yjs), component registry versioning, run diffing.
- Prior art to copy: n8n (per-node data inspector), Langflow (typed port
  colours), ComfyUI (groups/subgraphs), Rivet (execution recording).

## 2026-07-23 — Third architecture: durable savepoint engine (pkg/engine)

Scrapped the second design (plain-Go flows with *durable intent*, whole-job
re-run) after a design conversation (`conversation-*.txt`, `new-arch.md`). The
author's top priority was **opt-in, per-step, resume-from-savepoint
reliability plus tracing** — a game save point, not durable intent.

Built `pkg/engine` from scratch:
- `Store` (2-method KV): `MemStore` default, `FileStore` (atomic rename).
- `Tracer` (1 method): `NoTrace` default, `JSONLTracer`. `StepRecord` carries
  run/flow/step/attempt/cached/start/dur/in/out/err — one record per savepoint.
- `Step`/`Do`/`Sleep`: memoized savepoints keyed `step/<run>/<name>`; result
  JSON-stored on first success, replayed (`Cached:true`) on resume. `Sleep`
  stores its deadline and `panic`s a private `yield` recovered by the engine
  to requeue the run (frees the worker; durable wait). Retry opts `Retries`,
  `Forever`, `Backoff`. Duplicate step name = `ErrDupStep`.
- Run loop: queue is concrete code over `Store` (not an interface). Runs
  persisted before Enqueue returns; claimed-run stays in store until acked
  (at-least-once via lease-by-omission); `New` reloads pending on boot. Sorted
  dispatcher + N workers = parallelism. ponytail notes on O(n) insert and
  single-process lock left for the redis/multi-process upgrade.
- Tests green: savepoint-resume (side effect runs once across a simulated
  crash), retry-to-success, retry-exhaustion, sleep-yield-then-resume,
  dup-step, enqueue-and-run e2e, filestore reload across restart.

Deleted the old DSL: `pkg/brain`, `pkg/serve`, `pkg/job`, `pkg/cron`,
`internal/app`, `cmd/cli`. Carried `pkg/model`, `pkg/memory`, `pkg/notify`,
`internal/{openai,anthropic,config,logging,telemetry}`.

Rewrote `cmd/jarvis-demo` as the doorbell flow — runs with no API key, prints
a jsonl trace showing `classify` served from cache after a `Sleep` resume and
`notify` retried until success. `go build/vet/test ./...` all green.

Rewrote `PRODUCT.md`, `IMPLEMENTATION.md`, `docs/authoring-guide.md` to match.
IMPLEMENTATION.md lists the pending slices (serving layer, config, cron,
runtime-data KV, OTel tracer, notify durability) — engine substrate is done,
those layer on top.

## 2026-07-23 (cont.) — Serving layer, cron, runtime data; reference brain complete

Continued the third architecture until the product surface is complete.

- `pkg/serve`: OpenAI + Anthropic chat endpoints (both streaming) + `/models`
  over the engine. `Brain` (New/OnChat/Mux/Handler/Serve) and `Turn`
  (Messages, Params, Model(role), System, Reply, Later). Reply streams in the
  caller's protocol via an `internal/openai`+`internal/anthropic` seam;
  protocol difference is one switch in reply.go. Tests: both protocols,
  streaming, system-prepend, Later-enqueues, no-handler guard. All green.
- `pkg/engine` additions: `EnqueueID` (idempotent singleton runs);
  `SetData`/`GetData` (run-scoped durable KV, data.go); `Every` + a 5-field
  crontab parser (cron.go) implemented as a self-rescheduling durable run —
  cron rides the same queue, no timer subsystem. Fixed an Enqueue
  double-marshal. Tests: cron parse-errors/next/step-list/fire, plus existing.
- `cmd/jarvis-demo` rewritten as the full home assistant: serves chat with
  memory recall + persona, routes "remember X" to a durable background flow
  (Later → Do(...,Forever)), runs a nightly cron review, jsonl trace of every
  savepoint. Runs with no API key (scripted model) or a real provider via
  BIG_BRAIN_API_KEY/_BASE_URL/_MODEL; BIG_BRAIN_DATA=<dir> for restart-durable
  storage. Smoke-tested with curl: OpenAI non-stream, /models, Anthropic
  stream, and the background remember flow all confirmed in the trace.

Deliberately NOT built, with reasons (see IMPLEMENTATION.md "Pending"):
OTel Tracer backend (JSONL already covers tracing; clean one-type swap, skip
the trace SDK until spans are needed); Config→Brain boot helper (author wires
a Go program, that is the config surface; moving internal/config to pkg/ waits
for a second brain); notify durability as a subsystem (it's Do(...,Forever) /
Later — a separate send queue would duplicate the engine's guarantee).

`go build/vet/test ./...` all green.

## 2026-07-23 (cont.) — Schedule introspection: Scheduled() + Cancel()

Added cron/timer listing and cancellation (dynamism ladder level 3/4).
- `Every` now returns its ticker handle ID (was error-only).
- `Engine.Scheduled() []Schedule` — snapshot of pending runs (ID, Flow, Wake,
  and Cron=spec for tickers). Meant to be formatted for a model to choose
  cancellations, or driven by author logic.
- `Engine.Cancel(ctx, id)` — removes the pending run, writes a tombstone under
  cancelled/<id> so a ticker mid-fire won't re-arm, acks the record, nudges the
  dispatcher, and traces a <cancel> record for audit. Unknown/already-done ID =
  no-op. ponytail note: tombstones aren't pruned.
- Cron ticker checks e.cancelled(id) at the top and does nothing if tombstoned.
- Kept it unguarded (no per-handle allowlist) per the "keep it simple" call;
  documented in the authoring guide that callers should filter Scheduled()
  before exposing handles to a model. HTTP routes left as-is (stdlib ServeMux
  is write-only) per user's decision.
Tests: TestScheduledLists, TestCancelStopsCron (incl. re-arm-after-cancel
guard). go build/vet/test ./... green.

## 2026-07-24 — bb framework implemented (slices 1–6), all green

Implemented the `bb` design from cmd/marvis-demo/main.go (the goal post),
package-by-package, leaves first, each fully tested (many under -race) before
the next. Architecture in BB.md.

- Slice 1 model: pkg/model Spec builder (WithName/Think/Temprature, value-
  immutable), tag Registry (RegisterModel/Lookup/Resolve), Bound (inject a
  Model), Message builder (NewMessage/As). bb facade: Model, NewModel(tags…),
  RegisterModel, Message, NewMessage, Role.
- Slice 2 agent: internal/agent Agent (build-time, WithModel/Role/Schema/
  Selects/OnMessage) + Turn (runtime: Add/Last/Ask/AskWith/Reply/Select) +
  Reply (ReadAll/Read/Stream/Media). Schema-mismatch owned by Ask. bb.Extract[T]
  is a FREE function (Go forbids generic methods — the one goal-post divergence:
  reply.Extract[intent]() → bb.Extract[intent](reply)).
- Slice 3 flow: internal/flow Flow (sealed iface), Basic (WithId/WithAgent),
  seq via Next, Select group (runtime unknown-id = loud error), Respond, trace
  seam.
- Slice 4 concurrency: multi-agent flows run concurrently (fixed slice 3's
  sequential run); divergent concurrent Select = ErrSelectConflict; All/One/
  Group; Checkpoint/Wait/Reached.
- Slice 5 serve: internal/serve OpenAI+Anthropic HTTP + /models + diagnostics;
  flow.Validate (startup: modelless default agents, unbuildable models, declared
  Select exits ⊄ group); Addr/Workers/Trace opts; Handler+Serve.
- Slice 6 durability+trace: flow checkpointing over a Store (engine.Store),
  keyed by (run id from X-Run-Id header, structural path); flow.cached resume;
  Event timings + JSONL tracer; Notify prebuilt flow. bb: Store/MemStore/
  FileStore/Notify/JSONL.

Verified: a throwaway copy of main.go WITH the bb import builds; only remaining
errors are reply.Extract (impossible-as-method, flagged) and a `return flow`
missing `, nil` (a bug in the goal-post's own code). API conforms.

Old superseded code (pkg/brain-era already gone; pkg/serve old Brain, old
cmd/jarvis-demo) can be deleted next — bb.Serve replaces them.

## 2026-07-24 (cont.) — Deleted old code, rewrote jarvis on bb, updated all docs

- Deleted superseded packages: pkg/serve (old Brain, replaced by internal/serve
  + bb.Serve), pkg/memory (bb has no memory primitive — memory is author state),
  pkg/notify (replaced by bb.Notify + author send func), and the dead
  internal/config + internal/logging + internal/telemetry cluster (0 importers).
  Repo builds/vets/tests green after removal.
- Added bb.FixedModel(reply) — a canned model so a brain runs with no API key.
- Rewrote cmd/jarvis-demo on bb as a smart-home brain (NOT a marvis copy):
  keyword router → Select(talk, remember, recall, house, briefing) → Respond →
  Notify. Self-contained dummy world HTTP server (:8090) with sensors/devices/
  notify sink; house agent reads sensors + sets devices; briefing reads sensors
  concurrently; memory kept as author state and woven into persona; durability
  via bb.Store; jsonl trace. Smoke-tested end to end — every capability works,
  world side-effects (🏠/🔔) fire, memory persists across requests.
- Docs: rewrote IMPLEMENTATION.md for the bb architecture (folded in the old
  BB.md, then deleted BB.md); updated PRODUCT.md (authoring model, faculties,
  persistence→durable-execution, parallelism, reference brains, config);
  rewrote docs/authoring-guide.md completely for bb; rewrote README.md (was the
  long-dead node-graph design importing pkg/brain+pkg/serve).
- Did NOT touch cmd/marvis-demo/main.go (the goal post; user fixed its errors).

## 2026-07-24 (cont.) — Cleared the ponytail debt ledger (7 fixes)

Fixed all 7 deliberate shortcuts; no ponytail: markers remain.
- engine pending queue: O(n) sorted-slice insert → container/heap min-heap
  (runHeap); dispatch pops the earliest in O(log n).
- engine cancel tombstones: now written only for cron-ticker ids (a finite,
  reused set), never for one-off cancels — bounds growth to distinct tickers.
- engine FileStore: 256-way subdirectory fan-out by hash prefix + sharded
  lock (256), so large key sets don't pile into one dir and unrelated keys
  don't contend. No new dependency.
- engine cron: added Catchup() option — fires the target once per missed tick
  (ticker payload carries its scheduled time); default stays fire-once-late.
- engine Step retry: backoff >= 30s now yields the worker (persists the attempt
  counter, requeues, resumes) instead of holding a goroutine; short backoffs
  stay inline.
- bb Schema: `enum:"a,b,c"` struct tag → JSON-schema enum (plus existing doc).
- flow Group: real live shared chat — members run over one agent.SharedChat
  with write-through replies and live reads, so a member sees another's reply
  as it lands (was: same-starting-chat merge). New internal/agent SharedChat +
  NewSharedTurn; flow carries it on ctx.
Tests added for every fix (Group live visibility via a checkpoint, cron
catch-up count, bounded tombstones, step yield-on-long-backoff, schema enum);
all green under -race. go mod tidy dropped deps orphaned by the earlier
package deletions.

## 2026-07-25 — Model inheritance ladder (WithModel/WithDefaultModel)

Goal (from marvis-demo main.go): no agent is model-less. An agent inherits its
model from the flow, and a flow from a default model.

Done:
- pkg/model registry: added a process default. First `Register` sets it;
  `SetDefault` overrides; `Default()` reads it. `ResetRegistry` clears it.
- pkg/bb: replaced `RegisterModel(m, tags...)` with fluent `WithModel(m)`
  returning a `RegisterModel` handle, `.WithTag(tags...)` to bind lookup tags.
  First `WithModel` is the implicit default. Added `WithDefaultModel(m)` — sets
  the default without tagging.
- internal/flow Basic: added `WithModel` (flow default model) and resolves each
  agent's model along the ladder — agent → flow → default — before running,
  via `resolved()`/`modelFor()`. Validate uses the same resolution.
- jarvis-demo: chat model now `WithModel(...).WithTag("chat")`; talk flow sets
  the model on the flow, its agent inherits it.
- docs/authoring-guide.md: documented the 4-rung ladder and WithModel/WithTag.
- Tests: flow inheritance ladder (agent/flow/default precedence), registry
  default (first-register + SetDefault override). All green.

## 2026-07-25 — Multi-flow serving (WithFlow/As/default precedence)

Goal (from marvis-demo main.go): one brain serves several flows, selected by
the request's model name, with one default flow for unnamed/unknown models.

Done:
- internal/serve: process-global flow registry (registry.go) mirroring the
  model registry. Entries carry a rank; default resolved by precedence
  (Serve-arg > WithDefaultFlow > unnamed WithFlow > named fallback), last wins
  within a rank. `Handle`/`AddUnnamed`/`AddDefault`/`SetName`/`resolveRegistry`.
- internal/serve server: now holds named map + default flow; `resolve(model)`
  routes per request (named match else default), echoing the served id back.
  Handler validates every distinct flow; `ErrNoFlow` when nothing to serve.
  `bb.Serve(ctx, nil, ...)` serves only registered flows.
- internal/openai WriteModels: variadic names → /models lists every served id.
- pkg/bb: `WithFlow(f) RegisterFlow`, `.As(name) RegisterNamedFlow`,
  `WithDefaultFlow(f)`, chainable `.WithFlow`/`.Serve`. `As` twice and a second
  default in a chain are compile errors by type construction.
- docs/authoring-guide.md: "Serving several flows" section + precedence list.
- serve: duplicate flow name warns (logrus, like an id-less Select member).
- Tests: registry precedence, last-within-rank, per-request routing, duplicate
  name warning. All green.

## 2026-07-25 — PRODUCT doc reconciled with multi-flow + WithModel

Docs only (no code). Closed the drift the PRODUCT audit (next.md) found:
- PRODUCT §Configuration: `RegisterModel(m, tags)` → `WithModel(m).WithTag(...)`,
  plus the model inheritance ladder (agent→flow→WithDefaultModel→first model).
- PRODUCT §"One brain per process" reframed as "One deployment, one owner — but
  several flows": documents multi-flow serving (named flows picked by model id,
  default precedence, /models lists all) while explicitly ruling out first-class
  multi-tenancy (no tenant boundary/auth/isolation; separate processes or an
  externalized tenant-keyed store remain the deployment's job). Speaker identity
  noted as author logic, not a framework primitive.
- README.md and docs/IMPLEMENTATION.md: `RegisterModel` → `WithModel().WithTag()`;
  IMPLEMENTATION serve entry notes the flow Registry + default precedence.

## 2026-07-25 — Clarify request params reach flows as context (docs)

Docs only. Specified the intended semantics for client sampling params:
- PRODUCT §Serving: request params (model, temperature, max_tokens, …) are
  accepted and **reach the flow as request context**, read by the handler to
  honor/clamp/ignore/branch — never a silent override of the agent's own model
  config.
- authoring-guide: added `turn.Request()` (request params as context) + a
  handler example (low max_tokens → terser persona), stressing that `Ask` still
  uses the agent's WithModel config, not the request's.
- next.md audit: reframed the sampling-params item — doc now correct, code is
  the gap; the fix must expose params as read-only context, not auto-apply them.

## 2026-07-25 — Implement request params as flow context (turn.Request)

Wired the client's sampling params through to handlers as read-only context.

Done:
- internal/agent/request.go: `Request` struct (Model, Temperature, MaxTokens),
  `WithRequest(ctx)` / `requestFrom`, and `Turn.Request()`. Read-only context —
  never applied to Ask (the agent's own WithModel config still wins).
- internal/flow: `State.Req agent.Request`; `flow.Run` sets it on ctx once per
  run (constant for the chain) via agent.WithRequest.
- internal/serve: both openai/anthropic handlers build agent.Request from the
  parsed wire params and pass it into run → flow.State.Req.
- pkg/bb: exposed `bb.Request` (alias of agent.Request).
- Tests: serve end-to-end (params reach a handler via turn.Request), agent unit
  (zero value outside a request; carried value with WithRequest). All green,
  vet clean.
- next.md updated: moved from Partial/gap to Kept (implemented).

## 2026-07-25 — Streaming (terminal-only, durability-safe)

Implemented true token streaming. Design decided with the user first: live
streaming cannot cross a flow boundary (the checkpoint between flows is a
complete message), so it is confined to the terminal boundary; State always
carries whole messages, the client stream is a parallel tee.

Interface:
- reply.Stream() is now LIVE, backed by a record-replay streamBuf so
  reply.ReadAll()/Extract coexist with it (agent/stream.go, agent/reply.go).
- turn.Stream() (chan<- string, ok): claim-once client sink; ok=false for
  non-terminal / concurrent-group / non-streaming, whereupon the author
  turn.Reply()s. The framework tees to the client and records the whole message
  into State; a default (no-handler) agent auto-streams (flow/flow.go runAgent).
- reply.Err() carries a mid-stream model error.

Plumbing:
- agent.Sink on ctx (agent/sink.go); Serve installs it per streaming request.
- seq.run strips the sink from every step except the terminal one (before the
  first Respond, else the last step) — terminalStep().
- Turn.Ask returns a live Reply when a sink is present and no schema; buffers +
  validates otherwise (agent/turn.go).
- runOneAgent calls turn.AwaitStream() on every path so a streaming goroutine
  never races a later client write (fixed a real -race failure on the error
  path).
- Mid-stream errors: openai/anthropic WriteStreamError (SSE error frame); Serve
  emits it instead of a 500 once bytes are on the wire.

Also fixed a latent panic: Handler deduped served flows via a map keyed by Flow,
but a seq (chained flow) is unhashable — surfaced the first time jarvis served a
chained flow. Removed the dedup (Validate is idempotent).

Demo: jarvis talk agent now streams (terminal, before Respond). Verified end to
end with curl (SSE deltas + DONE for streaming; plain JSON otherwise).

Docs: PRODUCT (streaming non-promise rewritten), IMPLEMENTATION (agent/serve/
flow entries + a Streaming mechanism section), authoring-guide (Streaming to the
client section with the tee pattern), next.md (#1 marked done). Tests cover
streamBuf, Turn.Stream, terminalStep, serve live/error/buffered; all -race clean.

## 2026-07-25 — Triggers & durability, phases A–D (the "everything is a flow" surface)

Implemented the design worked out across the trigger/durability discussions
(docs/discussion.md, "Everything is a flow…"; plan in next.md #2). Built in four
green phases (build + vet + -race clean at each).

**Phase A — WithId/WithModel on the Flow interface.** Moved WithId/WithModel off
Basic onto the Flow interface, so every flow kind (Basic, seq, Select, All/One/
Group, respond, notify) implements them. Composites carry id/model via a shared
`decorated` wrapper (internal/flow/decorate.go); Basic carries its own. WithModel
on a group is the default model for its agents → model resolution is now lexical
scope over the tree (ctx-carried "nearest flow model", innermost wins), one
shared resolveModel used by run (scope from ctx) and Validate (scope threaded
through the static walk, which now unwraps `decorated`). Consequence: WithId/
WithModel return interface types, so they come after Basic's WithAgent — reordered
~25 call sites across jarvis + tests to .WithAgent().WithId().

**Phase B — loud, typed, opt-in durability.** WithId() → NamedFlow, NamedFlow.
Durable() → DurableFlow (durable-but-anonymous is a compile error). Serve now puts
the store on ctx (WithStore) but checkpoints nothing by itself; a Durable flow
calls activateDurable to turn on checkpointing for its subtree, so a flow without
.Durable() never persists even with a store. Added a structure-version guard
(discard a checkpoint whose graph changed, unless ForwardCompatible) and the
DurableOpt set (ForwardCompatible/ResumeOnReregister/Retries/TTL). jarvis's
remember flow marked .Durable() to keep its advertised durability honest.

**Phase C — triggers as flows + engine wiring.** Every(spec)/Once(t) are flow
nodes; reaching one splits the chain — seq.run hands the rest to a flow.Scheduler
(seam kept engine-free) as a deferred body and stops. serve's engineScheduler
adapts pkg/engine (register body once by id; engine.Every for cron, Enqueue for
one-shot) and Serve runs engine.Run as a worker. Trigger(opts) heads a startup
chain (self-registers; Serve runs it at boot to schedule). Mid-request Once works
the same way (scheduler on the request ctx), capturing chat + request params as
payload. Bodies run durably at job granularity; an unnamed body is warned and
skipped. Also fixed a latent Handler panic (deduped flows via a map keyed by the
unhashable seq).

**Phase D — turn data model.** bb.Payload[T](turn): arbitrary trigger data, the
open-ended companion to turn.Request(). Rides ctx as raw JSON (agent.WithPayload),
seeded by WithSeedPayload, captured into a scheduled body and replayed on fire, so
the same accessor reads it in a request, a startup chain, or a fired body.

Deferred (recorded in IMPLEMENTATION.md, not accidentally unfinished): a Webhook
inbound-HTTP trigger, Respond-as-sink-finalizer cleanup, finer bb-flow durability
inside a triggered body (job-level today), and honoring Retries/TTL via engine
options.

Docs updated with the code: IMPLEMENTATION (durability rewritten to opt-in +
triggers mechanism + trimmed planned section), authoring-guide (naming/models on
any flow, opt-in Durability, Initiative/triggers + Payload), discussion.md (the
design reasoning), marvis-demo goal spec (illustrative comments for the new
surface). Tests added for every phase; all -race clean.

## 2026-07-25 — jarvis-demo rewritten as a real assistant

Replaced `cmd/jarvis-demo` entirely (marvis-demo untouched — it stays the
annotated spec). jarvis is now the working, opinionated version of the same
idea: a home assistant you could actually run.

- `main.go` — wiring and capabilities. A schema router (small "fast" model,
  `bb.Schema[intent]` with an `enum` over the ids) picks one of eight
  capabilities in a `bb.Select`; every model-driven capability has a keyword
  fallback, so the whole brain works with no `BIG_BRAIN_API_KEY` at all.
  Capabilities: talk (persona, house + memory woven into a system note,
  streams when terminal), remember/forget (durable), recall (answers from
  facts in words), lists (durable), house control (structured
  device/sensor command, reports what the house actually reports back),
  briefing (a `bb.Group`: a reader posts raw state, a narrator `bb.Wait`s on a
  checkpoint and speaks it), remind. After `bb.Respond`, `bb.Notify` speaks
  the answer into the house.
- Initiative: a boot `bb.Trigger`, a one-minute `bb.Every` sweep that fires due
  reminders (cheap sweep beats a timer per reminder, and it survives restarts),
  a 07:00 morning briefing, and a 22:30 goodnight routine whose device plan
  travels as `bb.WithSeedPayload` / `bb.Payload[goodnightPlan]`. Every deferred
  body is named + `.Durable()`.
- `memory.go` — facts, named lists, reminders as one JSON doc in the bb store
  (`Get`/`Put`), so `BIG_BRAIN_DATA` makes memory outlive the process. `due()`
  marks and returns in one locked step: a reminder cannot fire twice.
- `world.go` — the dummy house, enhanced: six sensors derived from the clock
  and the device states (heater warms, fan cools, daylight/motion follow the
  hour), six devices incl. a thermostat, `GET /house` snapshot, `/notify` sink,
  and a `BIG_BRAIN_DEMO_TEMP_OFFSET` calibration knob.
- `main_test.go` — table tests for every keyword fallback and time parser, a
  memory reload/fire-once test, and an end-to-end request through `bb.Handler`
  asserting both the reply and that the house actually changed. All pass.
- `effective.go` added per the package rule.

No `pkg/` change, so the authoring guide's surface stays correct; refreshed the
one line describing what jarvis-demo demonstrates.

## 2026-07-26 — Tool-use design captured (docs only, no code)

Design session only; no `pkg/` or `internal/` change, so nothing to verify but
the prose.

- **`docs/discussion.md`** — appended "Tool use: two boundaries, three types, and
  the turn/chat split". Records the reasoning, not just the API: why the two tool
  boundaries (client-facing passthrough vs. inner Go-run tools) are different
  problems sharing one prerequisite; why the original `WithTool` + auto-loop
  sketch lost (a flow has several models, small ones with tiny context — the dev
  runs the loop); why the handler splits into `turn` (client-facing) + `chat`
  (model-facing); why `Tool`/`ToolCall`/`ToolResult` are three types and not one;
  the linked-or-stub counterpart accessors and their per-flow locality; the
  keystone resolution rule; the two coalescing invariants; and the reversal that
  made a bare agent a *full* transparent proxy.
- **`next.md`** — #3 rewritten from a sketch into a build spec: types, staged
  builders, direction table, the four rules, the default proxy, and a six-step
  build order. Steps 1–2 (tool-aware `model.Stream` + wire adapters parsing
  `tools`/`tool_choice`) land first because they alone close the PRODUCT
  "caller tools pass through untouched" promise. Header, suggested order, and the
  PRODUCT-audit slotting note updated to match.
- **`docs/authoring-guide.md` deliberately not updated** — the surface isn't real
  yet and the handler signature change (a second parameter) would make the guide
  wrong in a different direction. It gets updated as step 6, with the code, per
  the docs-move-with-code rule.

Next: step 1 — make the model layer tool-aware.

## 2026-07-26 (2) — Tool sugar designed: `OnCall` + `Resolve` (docs only)

Follow-up design pass on top of the same day's tool surface. Still no code.

- **`docs/discussion.md`** — appended "Sugar over the manual loop: `OnCall` +
  `Resolve`". The motivating problem is not only boilerplate: dispatching by
  `switch call.Name` duplicates the tool's name as a magic string with nothing
  checking the two agree. Records what was accepted (an optional handler on the
  `Tool`, local-only by construction) and what was rejected from the first sketch
  and why: `Ask` must never run a handler (a run-but-don't-send middle state means
  `Ask` silently turned on the heater), and `UsingTools` beside `WithTools` is a
  coin-flip pair that reintroduces the overloaded-noun problem the turn/chat split
  removed — so the mode lives on the **verb** (`Resolve`), which is also the
  design's existing word for a call that has a matching result. Also records why
  the proposed "client tools get a default client-facing OnCall that auto-tags for
  `turn.Call`" was cut: the keystone rule already makes unresolved calls fall out
  of the reply, and an auto-tag would make a `chat`-facing method cause a
  `turn`-facing effect. Open (mild, flagged): whether losing the explicit
  `WithSchema` stage on handler-carrying tools is worth the drift guarantee.
- **`next.md`** — #3 gains an "Optional local handlers" subsection with the four
  semantics to implement (handler error → `IsError` result, not an abort; round
  cap `.WithMaxRounds(n)` default 8 — the runaway-cycle guard from #2's tail
  arriving early; `Resolve` opt-in so the bare-agent proxy is untouched; durable
  re-runs re-run side effects). Direction table updated; build order is now seven
  steps, with the sugar deliberately **last** — it can only be judged once the
  manual surface is real and the demo has written the loop by hand.
- **`cmd/marvis-demo/main.go`** — `flowHouse` keeps the manual loop as live code
  (the low-level surface is the thing that must stay honest) and carries the
  `Resolve` version, the `bb.OnCall` tool definitions, and the mixed
  server-tools/client-tools relay as comments beside it.

Unchanged: the build order still starts at the tool-aware model layer, and the
authoring guide is still deliberately untouched until the code lands.

## 2026-07-26 (3) — `OnCall` revised: copy + schema check, not fusion (docs only)

Iteration on the sugar from entry (2), after a proposal to stage it
(`bb.OnCall(tool).Does(fn)`). Outcome: two of the three ideas adopted, staging
rejected, and the earlier "handler replaces `WithSchema`" claim reversed.

- **`bb.OnCall(tool, fn)` returns a COPY** and never mutates. The reason this
  matters beyond hygiene: one bare definition can carry many bindings — real vs.
  stub, or forwarded bare in one agent and handled locally in another — which is
  the project's mock-for-test-injection rule falling out for free. Fusing the
  handler into the constructor forecloses it.
- **`WithSchema` stays on every tool.** `OnCall` now *checks* instead of deriving:
  `bb.Schema[T]()` vs. the tool's recorded schema, mismatch recorded as a wiring
  error surfacing at `Serve` beside unknown model names and bad `Selects` ids. The
  guarantee weakens from "unrepresentable" to "cannot ship, fails at boot" and buys
  back **one `Tool` construction shape for every tool**, including wire-parsed ones
  that never had a handler stage. `Tool` can't be generic (heterogeneous
  `WithTools`, wire-parsed values), so boot is the earliest honest point anyway.
- **Staging rejected**, for a mechanical and a stylistic reason: a method can't
  introduce a type parameter, so `OnCall(tool).Does(fn)` can't infer `T` (it would
  be named at the stage *and* in the closure); and every staged builder here adds a
  differently-named required field per stage, while `OnCall` adds exactly one thing.
  `OnCall` belongs to the `Schema[T]`/`Extract[T]` family of free generic functions.
- **Invariant restated:** "a forwarded tool never gains a handler by itself." An
  explicit `bb.OnCall(turn.Request().Tools()[0], fn)` is allowed — deliberately
  serving a caller's capability. "Never execute the caller's tools" means never
  *implicitly*.

Updated in place: the `discussion.md` sugar section (records the reversal and why),
`next.md` #3's handler subsection, and the marvis comments (bare tools stay live
code; the bound copies and `Resolve` remain commented alternatives).

## 2026-07-26 (4) — Tools implemented, end to end (next.md #3, all seven steps)

Built the whole tool surface designed in entries (1)–(3) in one pass. Everything
green, `-race` clean, both demos build. Steps in the order `next.md` listed them.

**1. Tool-aware model layer** (`pkg/model/tool.go`, new). `Tool`/`ToolCall`/
`ToolResult` as plain structs with staged builders (`NewTool().As().Is().
WithSchema()`), linked-or-stub counterpart accessors, `.Message()` adapters, and
`Unresolved(msgs)` — the keystone rule as one function. `Message` gained
`Calls []ToolCall` / `Results []ToolResult` (plural: providers put several in one
message alongside text), so reading a payload is a `len` check, never an
assertion. Tools travel in on `Params`, calls come back on `Chunk.Call`, so the
one-method `Model` interface was not widened. `SameSchema` compares
**structurally** (drops `description`, sorts `required`) exactly as flagged —
byte comparison would have failed on map ordering alone. Side effect worth
knowing: `Message` is no longer comparable with `==` (it holds slices).

**2. Wire adapters.** `internal/openai` + `internal/anthropic` now parse
`tools`/`tool_choice` inbound and emit calls outbound, each in its own framing —
OpenAI puts a result in its own `role:"tool"` message and arguments in a JSON
*string*; Anthropic nests `tool_use`/`tool_result` inside content *blocks* and
wants input as an object. Both normalize `tool_choice`'s two spellings to one
neutral value. New `convert.go` in each. **This closes the PRODUCT promise
"caller tools pass through untouched"**, which had been silently dropping the
field.

**3. The turn/chat split.** `internal/agent/chat.go` (new) holds `ModelChat` and
`Asker`; `Turn` lost `Add`/`Ask`/`AskWith` and gained `Call`/`ToolResults`.
`NewTurn` now returns both handles. Every handler in the repo migrated, plus
both demos. `turn.Call` coalesces a turn's calls onto its last reply, so text and
calls leave as one message (what parallel tool use requires).

**4. Serve.** `run` returns text *plus* `model.Unresolved(out.Chat)`; the two
handlers emit `finish_reason: tool_calls` / `stop_reason: tool_use` accordingly.
The stateless loop needed no state: a client re-sending a transcript with its
result just re-runs the flow, and the answered call is filtered out by the same
rule that emitted it.

**5. `OnCall` + `Resolve`.** As designed — copy-not-mutate, schema *checked* not
derived, `Ask` never runs a handler, mode on the verb.

**Three deviations from the design, all recorded in `next.md` "As built":**
- the schema mismatch surfaces at `Ask` (`ErrTool`), not at `Serve` — a tool is a
  per-ask runtime value, so startup has nothing to inspect;
- `Resolve` treats a round as **all-or-nothing** (a mixed batch runs nothing and
  is handed back whole) — a partly-answered round is not a legal transcript for
  either provider, and this was caught by a test that had passed for the wrong
  reason: the first implementation silently dropped the client's call;
- `bb.Chat(ctx, m)` replaces `Model.Chat()` — `pkg/model` cannot import the agent
  package the handle lives in.

`bb.Extract[T]` now also decodes a `ToolCall`'s arguments (two type parameters,
source inferred), so `bb.Extract[args](call)` reads as the design intended.

**Tests** (all new, all `-race` clean): builders/linking/stub-resolution/
structural schema equality; the OpenAI provider assembling interleaved, split
tool-call deltas; both wire formats decoding a mid-loop transcript and emitting
calls; `Ask` sending tools but never running handlers, per-ask (not sticky)
tools, `ForwardTools` stacking and carrying the choice; the `Resolve` loop,
one-message coalescing, error-as-result, the round cap, cancellation, and the
all-or-nothing rule; `turn.Call` coalescing; and end-to-end through `serve` —
a **bare agent is tool-transparent** on both wires, an internally-resolved call
never reaches the client, and a re-sent transcript closes the loop statelessly.

**Docs.** `docs/authoring-guide.md` updated in the same change per the
docs-move-with-code rule: the two-handle model with a direction table, a full
Tools section (both boundaries, manual and `Resolve` paths, the resolution-rule
table, per-flow stub resolution, the bare-agent proxy), a standalone-chat note,
and two new entries in THE RULES. `effective.go` updated for `pkg/model`,
`internal/agent`, `internal/openai`, `internal/anthropic` — each explaining why
the tool types live where they do.

Next: #4 (native Anthropic consume), or the Tier-3 cleanups.

## 2026-07-26 (5) — Native Anthropic consume (next.md #4)

Added `github.com/anthropics/anthropic-sdk-go` (MIT, pre-approved) and
`pkg/model/anthropic.go`: a second `Model` implementation, sibling to
`openai.go`, translating the same neutral `Message`/`Params`/`Chunk` shapes
into Anthropic's own wire framing instead of going through its
OpenAI-compatibility shim.

Framing differences from OpenAI, each handled at the translation boundary:
- **System is a top-level field**, not a message role — `Stream` peels
  `Role: "system"` messages off into `body.System` instead of the transcript.
- **No dedicated "tool" role.** OpenAI answers a call with its own
  `role:"tool"` message; Anthropic has no such role, so a neutral message
  carrying `Results` renders as `tool_result` content blocks inside an
  ordinary **user** turn (`anthropicMessage`). A message carrying `Calls`
  renders as `tool_use` blocks in an **assistant** turn. Anthropic never
  splits one neutral message into several wire messages the way OpenAI's
  per-call tool messages do — blocks nest inside one turn either way.
- **Tool-call arguments stream as `input_json_delta` pieces keyed by content
  block index**, with id/name arriving on a separate `content_block_start`
  event rather than interleaved with the first argument piece (OpenAI's
  shape). `anthropicCallBuf` is the sibling of `callBuf`, keyed to this
  provider's event fields; same buffer-whole-and-emit-complete v1 stance.
  Text arrives as `text_delta` on the same `content_block_delta` event type,
  discriminated by `Delta.Type`.
- **`ToolInputSchemaParam` has no single raw-JSON-Schema field** — it wants
  `Properties`/`Required` split out, so `anthropicTool` reads those two keys
  out of `Tool.Schema` (which `bb.Schema[T]()` always produces) rather than
  handing the map through whole the way OpenAI's `shared.FunctionParameters`
  does.
- **`max_tokens` is required**, unlike OpenAI where it's optional; a Spec with
  none configured gets a default (`defaultMaxTokens = 4096`) rather than
  sending a request Anthropic would reject.

**Selecting it.** `Spec` gained `Provider` (`OpenAIProvider`/`AnthropicProvider`,
zero value OpenAI so every existing Spec is unaffected) and `WithProvider`;
`Build` branches on it. Aliased into `bb` as `bb.Provider`/`bb.OpenAIProvider`/
`bb.AnthropicProvider`, matching how every other author-facing model type
(`bb.Model`, `bb.Tool`, …) is a bare alias over `pkg/model`. This is orthogonal
to `bb.Serve`, which already speaks both wire protocols to *callers*
regardless of which provider a brain *consumes* — the PRODUCT gap closed here
is one-directional (bb.Serve → real Anthropic), not the reverse.

**A snag mid-build, worth recording:** the Anthropic SDK's SSE stream
dispatch routes on the SSE `event:` line name, not the JSON body's `"type"`
field — a test frame with only a `data:` line and no matching `event:` line is
silently skipped (`Next()` loops past it), which read as an empty response
with no error. Both fake-upstream test helpers now emit `event: <type>`
derived from each frame's own JSON, so this can't drift back out of sync.

Tests (`pkg/model/anthropic_test.go`, mirroring `openai_test.go`): streamed
text, upstream failure, context cancellation, split/interleaved tool-call
argument reassembly (out-of-order block indices), message rendering (calls →
assistant+tool_use, results → user+tool_result, plain → user+text), tool
choice mapping. All `-race` clean; full suite green.

Docs: `pkg/model/effective.go` and `docs/authoring-guide.md` (`WithProvider`
under Models) updated in the same change.

Next: Tier-3 cleanups (`bb.Workers` no-op, unwired `WithThink`, unused
`Prompt`/Template, `groups.go` duplication, `Reply.Media` stub), or #2's tail
(Webhook inbound, in-body durability, group-scheduler commit rule).

## 2026-07-26 (6) — Tier-3 cleanups: all five closed

Worked the Tier-3 punch list from `next.md` in one pass.

1. **`bb.Workers`.** The doc comment was stale, not the code — it still said
   "reserved; requests served per-connection" from before triggers/#2 shipped,
   but `workers` has fed `engine.Run(ctx, workers)` (the durable-job worker
   pool for `Trigger`/`Every`/`Once` bodies) since #2 landed. Reworded the
   comment on both `internal/serve.Workers` and `bb.Workers` to say what it
   actually controls; no code change, the wiring was already correct.
2. **`WithThink` wired.** `model.Params` gained `Think *bool`; `Spec.Params()`
   sets it from `thinkSet`. The Anthropic provider is the only consumer —
   `p.Think != nil && *p.Think` sends `Thinking: ThinkingConfigParamOfEnabled`
   with a fixed `defaultThinkBudget` (1024 tokens); OpenAI's provider doesn't
   read the field, so it's silently a no-op there, same "nil/unset means not
   sent" convention the rest of `Params` already uses. Tests: `Spec.Params()`
   round-trips think (existing immutability/unset tests extended), a new
   `TestAnthropicThinking` in `pkg/model/anthropic_test.go` asserts the wire
   body carries `thinking:{type:enabled,budget_tokens:1024}` when set and
   omits the field entirely when unset. `docs/authoring-guide.md`'s
   `WithProvider` paragraph corrected — it previously implied `WithThink`
   applied "either way" across providers, which stopped being true the moment
   it does something on one of them. `cmd/marvis-demo/main.go` got a one-line
   comment example next to the existing `WithProvider` note showing
   `.WithProvider(bb.AnthropicProvider).WithThink(true)`.
3. **`bb.Prompt`/`Template` deleted.** `pkg/bb/prompt.go` and its test removed
   — unused by both demos, and the discussion with the user concluded its one
   real differentiator over `text/template` (idempotent partial-fill: a
   `{name}` placeholder survives `Render` untouched until every agent in a
   flow has had a turn filling its own slice) isn't needed by anything today.
   Re-add when a real multi-agent-prompt-composition need shows up, not
   speculatively. `pkg/bb/effective.go`'s package doc no longer cites it as an
   example of a value type bb implements directly.
4. **`groups.go` duplication extracted.** `fanOut` (backing `All`/`One`) and
   `groupGroup.run` (backing `Group`) had byte-for-byte identical inline
   bookkeeping for two things: first-error-wins-and-cancels, and select-
   conflict accumulation across contributing members. Pulled both into small
   mutex-guarded types, `firstErr` and `selMerge`, that both call sites now
   use instead of repeating the logic. Left the two run loops themselves
   unmerged — their chat-sharing strategy (private clone-and-merge vs. one
   live `SharedChat`) and `One`'s take-the-first-success-and-cancel path are
   genuinely different, not incidental duplication, and forcing them into one
   generic function would've traded readable duplication for a callback maze.
   `go vet`/`gofmt` clean, `go test -race ./internal/flow/...` green.
5. **`Reply.Media`/`ListMedia`.** Already flagged in `next.md`'s Tier-3 list
   ("Fine as a stub, but note it's a promise not yet kept") and in the
   PRODUCT audit's Missing section. Nothing to add — left as-is.

`go build/vet/test ./...` green after all five. `next.md`'s Tier-3 section
marked done inline; `bb.Workers`' entry there corrected the same way (it was
never actually a no-op post-#2, only the doc lied).

Next: #2's tail (Webhook inbound HTTP, in-body durability, group-scheduler
commit rule, cycle-guard observability) — nothing else queued.

## 2026-07-26 (7) — Serve-side think: the request half of the same gap

Follow-up found immediately after closing the Tier-3 `WithThink` item: that
work only wired the *consume* direction (bb asking its own upstream Anthropic
model to think). The *serve* direction — a caller hitting `bb.Serve` and
asking bb itself to think — was untouched, same shape as the caller-tools gap
`#3` closed before Tools shipped: the field was silently parsed into nothing.

- `agent.Request` gained `Think *bool`; `NewRequest` takes it as a new
  parameter (all three call sites — `internal/serve/serve.go` ×2,
  `internal/agent/chat_test.go` — updated).
- `internal/anthropic/wire.go`: `MessagesRequest.Thinking *ThinkParam`
  (`{"type":"enabled"|"disabled","budget_tokens":N}`, matching the real
  Anthropic wire) plus a `Think() *bool` accessor — nil when the field is
  absent, `true`/`false` from `Type`.
- `internal/openai/wire.go`: `ChatRequest.ReasoningEffort string` (OpenAI's
  actual field is an effort string, not a boolean; bb's `Think` is bare on/off
  per the outbound design, so any non-empty value maps to `true`) plus a
  matching `Think() *bool` accessor.
- Both `internal/serve/serve.go` handlers pass `req.Think()` straight through
  to `agent.NewRequest` — same "read-only context, never auto-applied" pattern
  as `Temperature`/`MaxTokens`/`Tools`. A handler decides whether to build a
  model with `.WithThink(true)` in response; nothing forwards implicitly.
- Tests: wire-level `Think()` accessors in both `internal/openai/wire_test.go`
  and `internal/anthropic/wire_test.go` (nil-when-absent, true/false mapping);
  end-to-end `TestRequestParamsReachHandler`/
  `TestRequestThinkReachesHandlerAnthropic` in `internal/serve/serve_test.go`
  assert a real request body's think field lands on `turn.Request().Think`
  through both protocols, and stays nil when omitted.
- Docs: `docs/authoring-guide.md`'s Request-parameters section and worked
  example mention `req.Think` alongside the existing params.

`go build/vet/test -race ./...` green.

Next: #2's tail — nothing else queued.

## 2026-07-26 (8) — Request params audit: a real bug fixed, top_p/stop added

Asked "what other standard request options aren't exposed" and checked both
`ChatCompletionNewParams` (openai-go) and `MessageNewParams`
(anthropic-sdk-go) field-by-field against `agent.Request`. Found one actual
bug and two same-shape gaps worth closing; everything else (seed, penalties,
logit_bias, service_tier, metadata, user/safety identifiers, `response_format`
/`output_config`, audio/modalities/web_search) is billing/infra/a bigger
design question, left alone as YAGNI.

- **Bug fixed:** OpenAI's `max_tokens` is deprecated in favor of
  `max_completion_tokens` and rejected outright by o-series reasoning
  models — exactly the clients also likely to send `reasoning_effort`, which
  we'd just wired. `ChatRequest` only ever parsed `max_tokens`, so a modern
  client's cap silently vanished. Added `MaxCompletionTokens` alongside it and
  a `MaxOutputTokens() *int64` accessor that prefers the current field,
  falling back to the legacy one — `internal/serve/serve.go`'s OpenAI handler
  now calls it instead of reading `MaxTokens` directly.
- **`top_p` added** on both wires (`ChatRequest.TopP`, `MessagesRequest.TopP`)
  — same "sampling knob as read-only context" shape as `Temperature`.
- **Stop sequences added** — OpenAI's `stop` (`Stop`, a dual string-or-array
  type mirroring the existing `ToolChoice` decode pattern) and Anthropic's
  `stop_sequences` (already a plain `[]string` on that wire). Both land on
  `agent.Request.Stop []string`.
- **Anthropic's `top_k` added** (`MessagesRequest.TopK`) — Anthropic-only, so
  `agent.Request.TopK` is nil on every OpenAI-served request; documented as
  such rather than pretending it's cross-provider.
- **`agent.NewRequest`'s signature changed shape**, not just grown again: this
  was its third round of additions (temperature/maxTokens → +think → now
  +topP/topK/stop), and continuing to add positional pointer params was
  becoming a footgun (nine same-typed args in a row). Switched to
  `NewRequest(r Request, tools []model.Tool, choice string) Request` — the
  public sampling fields go in as a struct literal (self-documenting at each
  call site via field names), tools/choice stay a separate pair because
  they're genuinely a different kind of thing (unexported, accessor-gated).
  All three call sites (`internal/serve/serve.go` ×2,
  `internal/agent/chat_test.go`) updated to the new shape.
- Tests: wire-level decode tests for `top_p`/`stop`/`stop_sequences`/`top_k`
  in both `wire_test.go` files, a dedicated `MaxOutputTokens` precedence test,
  and end-to-end serve-layer tests (`TestRequestParamsReachHandler` extended,
  new `TestRequestLegacyMaxTokensStillWorks`) confirming a real request body's
  values land on `turn.Request()` through both protocols.
- Docs: `docs/authoring-guide.md`'s Request-parameters section and worked
  example updated for the new fields and the `MaxOutputTokens` resolution.

`go build/vet/test -race ./...` green.

Next: #2's tail — nothing else queued.

## 2026-07-26 — Expose `bb.DefaultFlowName`, tests for default-flow relabeling

Prompted by noticing `internal/serve.Workers` had no caller anywhere; while
auditing sibling options found `internal/serve.Name` was likewise wired but
never exposed through the public `bb` facade (unlike `Addr`/`Workers`/
`Trace`/`Store`, which all have `bb` wrappers).

Decision: keep `Name`. It's the only knob for the reported model id of a
flow served *without* going through the named-flow registry (bare
`Serve(ctx, f)`/`Handler(f, ...)`, or `WithDefaultFlow`) — `WithFlow(f).As()`
already covers named flows, but has no equivalent for those three unnamed
paths, and using `.As` on them would demote their default-selection rank
(see `internal/serve/registry.go`'s precedence table), which isn't what
relabeling should do.

Exposed it as `bb.DefaultFlowName` rather than `bb.Name` — the bare word was
ambiguous against `WithFlow(f).As(name)`'s naming vocabulary and against the
provider-model sense of "name" used elsewhere in `bb` (`Model`, `WithModel`,
`NewModel`). Internal `serve.Name` left unrenamed; only the facade changed.

- `pkg/bb/serve.go`: added `DefaultFlowName(n string) Option { return
  serve.Name(n) }`, doc comment cross-references `WithFlow(f).As`.
- `internal/serve/serve_test.go`: two new tests going through
  `Handler`/`build` (not just constructing `*server` directly, which
  wouldn't catch a regression in option wiring or precedence):
  - `TestNameOptionRelabelsWithoutChangingRouting` — `Name` changes what
    `/v1/models` and response bodies report for the default, while a
    separately `WithFlow(...).As("sidekick")`-registered flow still routes
    and reports correctly, unaffected.
  - `TestNameOptionDoesNotChangePrecedence` — an explicit `Serve`/`Handler`
    arg still outranks a registered default even when renamed via `Name`.
  Verified both tests actually catch regressions by temporarily reintroducing
  two bugs (option not wired to `c.name`; option a no-op) and confirming
  failures, then reverting.

`go build ./... && go test ./internal/serve/... ./pkg/bb/...` green.

## 2026-07-26 - Add `bb.Run`

- Added `bb.Run(ctx, opts...)` (backed by `serve.Run` in
  `internal/serve/engine.go`): drives registered triggers and the durable job
  engine with no HTTP listener at all, for a brain that only reacts to
  crons/timers/internal events. Requires `bb.Store(...)` (`ErrNoStore`
  otherwise); refactored the trigger-wiring block out of `build()` into a
  shared `wireScheduler` helper used by both `Serve`/`Handler` and `Run`.
  Updated `docs/authoring-guide.md`'s triggers section accordingly.

## 2026-07-26 - Trigger cycle guard (next.md #2 tail, item #4)

Verified the four remaining items in #2's tail against the actual code before
touching anything (grepped for cycle-guard logic in `internal/flow` and
`pkg/engine` — zero hits — confirming "no guard/observability yet" was still
true). Built the cycle guard, the cheapest of the four:

- `internal/flow/trigger.go`: a per-lineage depth counter carried through
  `triggerPayload.Depth` (context key `triggerDepthKey`, helpers
  `withTriggerDepth`/`triggerDepthFrom`). `deferBody` increments it each time a
  fired body's own flow reaches *another* trigger node — not on a plain
  recurring `Every`/`Once` tick, which the engine re-fires the same registered
  body directly without passing back through `deferBody`. Past
  `maxTriggerDepth` (8, matching the order of magnitude of tools'
  `Resolve`/`WithMaxRounds`), scheduling is refused and `flow.ErrTriggerCycle`
  (new sentinel, `internal/flow/errors.go`) is returned and logged via
  `logrus.Error` with the body id and depth.
- **Found and fixed a real gap while wiring the test for this**: the depth
  check was unreachable in production. `Serve` (`internal/serve/serve.go`)
  ran the engine worker (`s.sched.run(ctx, c.workers)`) on the bare request
  ctx, with no `Scheduler` installed — so a fired body's nested trigger hit
  `schedulerFrom(ctx) == nil` and silently no-op'd, same early-return path as
  "no engine configured." The cycle guard sits after that check, so it never
  ran. Fixed by wrapping the worker ctx with `flow.WithScheduler(ctx,
  s.sched)` before handing it to `s.sched.run`, mirroring the per-request path
  (`internal/serve/serve.go`'s `server.run`) that already did this. Without
  this fix, the old next.md claim "cycles as re-triggers — supported in
  principle" wasn't actually true: a nested trigger inside a fired body just
  silently dropped.
- Tests in `internal/flow/trigger_test.go`: `TestTriggerCycleGuard` (depth
  math directly, via `deferBody` with a fabricated depth on ctx — allowed
  through depth 8, refused at 9); `TestTriggerDepthThreadsThroughFire` (an
  actual outer→mid→inner trigger chain, verifying the depth survives the JSON
  round-trip through a real fire and that the fired body's ctx needs
  `WithScheduler` to reach the nested trigger at all — a bare
  `context.Background()` silently no-ops instead, which is what caught the
  `serve.go` gap above).
- `docs/authoring-guide.md`'s re-trigger note updated: mentions the 8-level
  cap, `flow.ErrTriggerCycle`, and that a plain recurring ticker never counts
  against it.
- `next.md` updated: item #4 marked SHIPPED with the same detail as above;
  three items remain in #2's tail (Webhook inbound HTTP, in-body durability,
  group-scheduler commit rule).

`go build ./... && go vet ./... && go test ./...` green, `-race` included via
the existing `go test ./internal/flow/...` suite.

Next: Webhook inbound HTTP (next.md #1) — the only remaining tail item with
no design prerequisite.

## 2026-07-26 - Default Store to in-memory instead of disabling triggers

The `if c.store != nil { wireScheduler(...) }` guard in `build()`
(`internal/serve/serve.go`) was wrong: its comment claimed triggers "require"
a store, but the actual effect of no `bb.Store(...)` was that they *silently
no-op* — `s.sched` stayed nil, the worker never started, and `deferBody`
(`internal/flow/trigger.go`) saw `schedulerFrom(ctx) == nil` and dropped every
`Every`/`Once`/`Trigger` schedule with no error. `pkg/engine` already has a
real default for this (`engine.MemStore`, what `engine.New` itself falls back
to on a nil store), already exposed at the `bb` layer as `bb.MemStore()`.

- `defaults()` (`internal/serve/serve.go`) now seeds `config.store` with
  `engine.NewMemStore()`, so `c.store` is never nil after option application.
  `build()`'s guard is gone — `wireScheduler` always runs.
- `Run` (`internal/serve/engine.go`) no longer requires an explicit `Store`:
  removed the `ErrNoStore` sentinel and its check. `Run` now works with the
  same in-memory default as `Serve`/`Handler`.
- Net behavior change: `Trigger`/`Every`/`Once` chains and `.Durable()` flows
  now work out of the box with zero config (in-process only — a restart
  loses everything, same caveat `bb.MemStore()` already carried). Previously
  they were dead without an explicit `bb.Store(...)`.
- Updated doc comments on `Store`, `Run` (both `internal/serve/engine.go` and
  `pkg/bb/serve.go`) to state the in-memory default and recommend
  `bb.FileStore(dir)` for anything that must survive a restart.
- `docs/authoring-guide.md`: Serving section notes the in-memory default;
  Durability section no longer implies a store is only present when
  configured explicitly; Triggers section's "requires Store" bullet rewritten
  to describe the default/opt-out-of-ephemeral behavior, same note added to
  the `bb.Run` bullet since a store-less headless process has the least to
  show for an in-memory-only default.

`go build ./... && go vet ./... && go test ./...` green (existing tests
already ran without `bb.Store(...)` in several places, and still pass).

## 2026-07-26 — Webhook inbound HTTP (next.md #1), design + build

Discussed the design in conversation first (recorded in next.md #1), then
built it in the same session.

Design decisions, settled in discussion before writing code:

- `bb.Webhook(endpointID string) Flow` — a third trigger-node variant beside
  `Every`/`Once`, taking its own explicit id (the `POST /v1/hooks/{id}` route
  slug), deliberately decoupled from the body's `WithId`: a public URL a
  third party hardcodes vs. an internal Durable/Select identity are different
  concerns, and coupling them (resolving the route from `body.id()`) would
  also collide with the pre-existing `seq.id()==""` ambiguity bug (next.md
  #6, not fixed here).
- No `Store` required for the base case — firing a webhook is a normal
  synchronous `flow.Run`, structurally the same as any other served request,
  not a durable schedule with a time gap to survive a crash across.
  `Durable()` nested inside still no-ops without a store, unchanged.
- Response depends on whether the body reaches a top-level `Respond` (reusing
  the same shallow scan `terminalStep` already does): with one, wait and
  reply 200 with the resulting chat's last message; without one, reply 202
  immediately and run the body in the background (detached
  `context.WithoutCancel`, since `r.Context()` is cancelled the instant the
  handler returns after `WriteHeader`) — a webhook is often a long job, the
  caller shouldn't block on it.
- Data propagation must not special-case webhook: reuses the exact
  `agent.WithPayload`/`PayloadFrom` ctx-threading `Every`/`Once` already use,
  confirmed by tracing that a payload set via `WithPayload` already survives
  into a nested trigger's own `deferBody` call today (verified, not just
  assumed) and that `pkg/engine`'s `Every` closes over its payload once and
  replays it identically on every tick. Webhook's own twist: Chat/Req
  accumulated up to the `Webhook` node (e.g. a `Trigger`'s `WithSeedChat`)
  plays the same role Every/Once's captured state does (frozen, replayed
  every fire); Data is the incoming POST body instead, fresh per fire — an
  external caller supplying its own event data on each call is the whole
  point of a webhook, not a violation of the "data doesn't change" rule.
- No auth, rate limiting, or body-size cap in code. Checked: `net/http` has
  no stdlib default body-size cap to expose (only `MaxHeaderBytes`, headers
  only). Documented instead: not this package's job, put a reverse
  proxy/gateway in front, the endpoint id is not a secret.
- Considered and rejected a `bb`-specific serializable interface for trigger
  payloads: `encoding/json.Marshaler`/`Unmarshaler` on the payload type
  itself already gives an author that hook for free.

Built:

- `internal/flow/trigger.go`: `triggerNode.webhook` field, `Webhook(id)`,
  `Webhooks` interface (`Register(endpointID, WebhookHandler)`) + its ctx
  seam (`WithWebhooks`/`webhooksFrom`), `registerWebhook` (the webhook branch
  of `deferBody` — same cycle-guard depth check as Once/Every, reused
  unchanged), `containsRespond` (the shallow top-level scan).
- `internal/serve/webhook.go`: `webhookRegistry` (plain mutex-guarded map,
  implements `flow.Webhooks`, `ErrDupWebhook` sentinel on a collision) and
  `server.webhook`, the `POST /v1/hooks/{id}` handler (404 unknown id, 400
  bad body read, 200-sync or 202-async per `HasReply`).
- `internal/serve/engine.go`: `wireScheduler` now also builds the
  `webhookRegistry` and threads `flow.WithWebhooks` into the startup ctx
  alongside the existing `Scheduler`/`Store`; both `build()` and `Run()`
  updated for the new return value.
- `internal/serve/serve.go`: `server.hooks` field, `s.run` and the new
  handler share a `triggerCtx` helper (previously inlined in `s.run`) that
  wires `Store`/`Scheduler`/`Webhooks` uniformly regardless of which route is
  running the flow.
- `pkg/bb/flow.go`: `bb.Webhook(endpointID)` exported.
- Tests: `internal/flow/trigger_test.go` (`mockWebhooks` +
  `TestWebhookRegistersUnderEndpointID`, `TestWebhookHasReply`,
  `TestWebhookPayloadAndSeedReplay`, `TestWebhookCycleGuard`);
  `internal/serve/webhook_test.go` (sync-with-Respond returns 200 with
  content, no-Respond returns 202 and runs in the background, unknown
  endpoint 404s — all through `build()`'s real mux, not the bare handler
  method, so `r.PathValue("id")` is exercised for real).
- `docs/authoring-guide.md`'s Initiative section: `Webhook` added to the
  trigger list, its id-decoupling rationale, the Respond-gated
  sync/async reply rule, the Store-not-required note, the
  `bb.Handler`-suffices/`bb.Run`-can't-reach-it split, and the
  payload/seed-replay clarification.
- next.md updated during the discussion phase: #1 filled in with the settled
  design; #5 (`Respond` invisible through `Select`/`One`/`All`/`Group`) and #6
  (a multi-step trigger body silently resolves to no id) added as bugs found
  while designing this, deliberately left unfixed; #7 (headers via
  `bb.Payload[T]`) added as a deferred design question.

`go build ./... && go vet ./... && go test ./... -race` green.

Next: the three items left in next.md's tail — #5, #6, #3 (group-scheduler
commit rule) — plus #2 (in-body durability) and the newly deferred #7
(header access), in no particular forced order; #6 is the cheapest and
unblocks correct behavior for anyone chaining multiple named flows after a
trigger today.

## 2026-07-27 — Fix next.md #2, #3, #5, #6 (tests-first)

For each bug, wrote a failing test capturing the expected behaviour first,
confirmed it failed against the current code, then fixed it. All four are
independent bugs in the trigger/durability/grouping surface; fixed together
since they share files.

**#6 — trigger body resolves to one id, or errors.** Root cause was `seq.id()`
in `internal/flow/flow.go` unconditionally returning `""`, not just for
`deferBody`'s bodyID lookup but for every caller of a seq's `id()` (also hit
`Select`, which silently drops idless members — a `Select` member built as
`A.WithId("x").Next(Respond)` was being ignored). Fixed at the root: `seq.id()`
now resolves to the id of the single id-bearing top-level step among
`s.steps` (new `idsOf` helper), `""` when that's zero or ambiguous. `deferBody`
now calls `idsOf(rest)` directly (needs to tell "zero" from "ambiguous" apart
for its error message, which collapsing to `seq.id()`'s `""` would lose) and
returns the new `flow.ErrTriggerBodyID` sentinel — loud, not `logrus.Warn` —
when it isn't exactly one.
- Tests: `TestTriggerUnnamedBodyErrors` (replaces
  `TestTriggerUnnamedBodySkipped`, which encoded the old silent-skip
  behaviour), `TestTriggerBodyAmbiguousIdErrors`,
  `TestTriggerBodyResolvesSingleIdAmongMany`.

**#5 — `Respond` invisible inside `Select`/`One`/`All`/`Group`.** Added
`reachesRespond`/`flowReachesRespond` (`internal/flow/trigger.go`), a
recursive walk replacing the shallow `containsRespond` that `registerWebhook`
used for `HasReply`. Design call: any member of a `Select`/`One`/`All`/`Group`
reaching `Respond` counts, uniformly — at registration time there's no way to
know a `Select`'s runtime pick or a `One`'s eventual winner, so treating any
reachable path as "may reply" is the conservative-correct choice (a false
"has reply" just costs one synchronous wait; a false "no reply" silently
drops a response the caller was waiting on). `terminalStep` (the separate,
narrower "which step streams" scan in `flow.go`, used for the normal request
path) is untouched — out of scope, different consumer, different question.
- Test: `TestWebhookHasReplyThroughGroups` (all four group kinds, plus a
  none-of-them-negative case).

**#3 — scheduler-inside-concurrent-group commit rule.** `fanOut`'s `KNOWN GAP`
comment (`internal/flow/groups.go`) described the fix as "a two-phase
`Scheduler.Defer`, gated on won/winner being settled, applied only when
`first` is true" — built exactly that. New `pendingCommit`
(`internal/flow/trigger.go`): `fanOut` installs one per member on that
member's ctx only when `first=true` (`One`); `deferBody` sees it and queues
the `sch.Defer` call (a closure) instead of committing immediately. After
`wg.Wait()`, `fanOut` runs only the winner's queued calls; a loser's are just
dropped (its `*pendingCommit` goes out of scope, GC'd). `All`/`Group` install
nothing, so their members still commit immediately — correct as-is per the
original gap analysis, since every member's contribution is kept there.
Webhook registration is untouched (still commits immediately regardless of
group) — next.md's own open question ("seeing if Webhook is effected too")
is left open; `Webhooks.Register` erroring on a duplicate id is a different
failure shape than `Scheduler.Defer`, and no concrete case has hit it yet.
- Test: `TestOneDiscardsLosingMemberTrigger` — a fast member and a
  (deterministically) slow member both reach `Once`; only the fast one's
  `bodyID` shows up in the mock scheduler's calls.

**#2 — finer durability inside a triggered body.** Root cause: `Serve` wires
`flow.WithScheduler` onto the engine worker's ctx (for the cycle guard, #4)
but never `flow.WithStore` — so a `Durable()` flow nested in a fired
trigger body had no store to checkpoint into, silently. Fixed at
`engineScheduler.Defer`'s registration wrapper
(`internal/serve/engine.go`): before calling the trigger's `run` closure, it
now calls the new `engine.RunID(ctx)` (`pkg/engine/step.go`, exposes the
already-tracked-internally `rt.run.ID` — stable across a resume of the same
firing, fresh per new firing) and, if present, wires
`flow.WithStore(ctx, s.store, id)`. `engineScheduler` gained a `store` field
to have something to wire (previously it only kept the `*engine.Engine`,
discarding the `flow.Store` it was constructed with).
Scoped down from next.md's fuller ask: `Retries`/`TTL` on `Durable()` are
still inert on this path — `pkg/engine` has no job-level retry concept to map
them onto (`engine.Retries` is a per-`Step` option, not per-`Run`; a failed
`Run` today just acks/drops, doesn't retry). Inventing that concept wasn't
part of the well-specified bug (run-id threading) and would be its own
design pass — left for when a concrete need shows up, not built speculatively.
- Test: `TestTriggeredDurableFlowCheckpoints` (`internal/serve/engine_test.go`)
  — a `recordingStore` wrapping `engine.MemStore` proves a `flow/`-prefixed
  checkpoint key actually got written through the real store passed to
  `newEngineScheduler`, firing a `Durable` body through the real
  `engineScheduler`/`Engine` machinery (not a mock).

`go build ./... && go vet ./... && go test ./... -race` green.

Next: next.md's remaining open items are #7 (header access via
`bb.Payload[T]`, deferred design question) and the Retries/TTL-on-triggered-
Durable gap called out above; neither is blocking anything.

## 2026-07-27 — `bb.Metadata[T]` (next.md #7), design + build

Design settled over a discussion (recorded in next.md #7) before building:
field-name-match merging headers into `bb.Payload[T]`'s `T` was rejected (a
JSON body field colliding by accident with a header name would silently pull
from the wrong source) in favor of a wholly separate channel, generalized
past HTTP — not every trigger is HTTP (`Every`/`Once`/a custom entry point
have no headers), but every trigger already had a payload channel
(`bb.WithSeedPayload`), so metadata became that channel's sibling rather than
an HTTP-specific add-on. Scoped to trigger-fired runs only (webhook +
seeded `Every`/`Once`/custom) — deliberately **not** wired into plain served
chat-completions calls (`s.openai`/`s.anthropic`): `Payload`'s own docstring
already scopes it as "trigger-specific data," and a plain call already has
`turn.Request()` for its envelope — headers there are a transport/auth
concern, not something `OnMessage` logic should reach into. Revisit only if a
concrete need shows up; the plumbing (`r.Header` is already in scope at both
call sites) would be a small, symmetric add later.

**Built**, mirroring `Payload` exactly:
- `internal/agent/metadata.go` — new file, `metadataKey`/`WithMetadata`/
  `MetadataFrom`/`Turn.Metadata()`, same shape as `payload.go`.
- `pkg/bb/agent.go` — `bb.Metadata[T](turn)`, same shape as `bb.Payload[T]`.
- `internal/flow/trigger.go` — `triggerPayload` gained a `Meta []byte` field
  (`json:"meta,omitempty"`) alongside `Data`, captured/replayed the same way
  in `deferBody`'s `run` closure and `TriggerChain.RunAtStartup`. New
  `TriggerChain.seedMetadata` field + `WithSeedMetadata(v any) TriggerOpt`,
  mirroring `WithSeedPayload`. `WebhookHandler.Run`'s signature grew a `meta
  []byte` parameter (`func(ctx, payload, meta []byte) (State, error)`) —
  `registerWebhook`'s `run` closure now also calls
  `agent.WithMetadata(rctx, meta)`.
- `pkg/bb/flow.go` — exported `bb.WithSeedMetadata`.
- `internal/serve/webhook.go` — new `flattenHeaders(http.Header) []byte`:
  canonicalizes keys (`http.CanonicalHeaderKey`) and keeps only the first
  value of a repeated header (`http.Header.Get`'s own convention), producing
  `map[string]string` JSON — not `http.Header` verbatim, since `bb.Metadata`
  is deliberately not HTTP-specific and shouldn't carry HTTP's multi-value
  shape into a generic channel. Both `s.webhook` call sites (sync-reply and
  background) now pass the flattened headers through to `h.Run`.
- Replayability: `Meta` rides inside the same `triggerPayload` blob as `Data`/
  `Chat`/`Req`, which already crosses the durable-engine boundary as opaque
  `json.RawMessage` (`internal/serve/engine.go`'s `Defer`) — so metadata
  needed no new engine-side wiring to survive a `Durable()` checkpoint/resume
  or a cron refire; it gets the exact same promise `Data` already had.
- `docs/authoring-guide.md`'s trigger section gained a paragraph on
  `bb.Metadata[T]`/`WithSeedMetadata` alongside the existing `Payload` one.

Tests: `TestMetadataSeedAndReplay` (`internal/flow/trigger_test.go` — seed via
`WithSeedMetadata`, round-trips through the captured `triggerPayload` bytes
the scheduler would persist, replays on fire, mirroring
`TestPayloadSeedAndReplay`); `TestWebhookPayloadAndSeedReplay` extended to
also assert metadata across repeated fires; `TestWebhookHeadersReachMetadata`
and `TestFlattenHeaders` (`internal/serve/webhook_test.go` — a real
`httptest` request's header reaches `turn.Metadata()` end-to-end; canonical-
key/first-value-wins flattening in isolation).

`go build ./... && go vet ./... && go test ./...` green.

Next: next.md #7 is now fully shipped. Only the Retries/TTL-on-triggered-
Durable gap (#2's log entry above) remains open, not blocking anything.

## 2026-07-27 — Audit pass over next.md's SHIPPED claims (#1-#7): closed two
test gaps, found one open bug (#8)

next.md claimed #1-#7 all shipped and "nothing left to decide." Ran an
independent verification pass to check the claims against the actual code
and tests rather than take the doc's word — two parallel review passes (one
over #1/#2/#3, one over #4/#5/#6/#7), each reading the implementation and the
named tests directly, not just confirming `go test` passes.

**Confirmed correct, no changes:** #1 (webhook sync/async split,
`context.WithoutCancel` background detachment, `Durable()` no-op without
`Store`), #4 (cycle guard depth math, `WithScheduler` worker-ctx wiring), #5
(`reachesRespond` recursion through `seq`/`*decorated`/`Select`/`One`/`All`/
`Group`), #6 (`seq.id()`/`idsOf` ambiguous-id detection).

**Test-coverage gaps closed** (implementation was already correct; the
*proof* wasn't there):

- #2: `TestTriggeredDurableFlowCheckpoints` only asserted a checkpoint write
  happened, never that a second firing of the same run actually skips the
  completed step. Added `TestTriggeredDurableFlowResumeSkipsCompletedStep`
  (`internal/serve/engine_test.go`) — fires the same `engine.RunID` twice via
  `eng.EnqueueID` (since `Defer` always mints a fresh uuid, which wouldn't
  share a checkpoint scope) and asserts the agent runs once, not twice.
  Verified the test actually catches a regression: commenting out the
  `flow.WithStore` wiring it exercises makes it fail.
- #7: the metadata tests only round-tripped the raw `triggerPayload` JSON
  directly, never through a real `Durable()` checkpoint/resume over the
  actual `engineScheduler`. Added
  `TestTriggeredDurableFlowMetadataSurvivesResume` (same file) — same
  same-`RunID`-twice trick, agent reads `turn.Metadata()`, asserts it's seen
  once with the right value across both firings. Verified it actually
  catches a regression: commenting out the `agent.WithMetadata` wiring it
  exercises makes it fail with an empty metadata read.

**Bug found, not previously documented — next.md #8, left OPEN:** nested
`One`-in-`One` breaks #3's commit-gating. `fanOut`'s commit-execution point
(`internal/flow/groups.go:240-246`) fires the winner's queued
`Scheduler.Defer` calls unconditionally once its own group resolves — it
never checks whether the ctx it was itself entered with already carries an
*outer* `pendingCommit` (from an enclosing `One`) that it should queue into
instead. Concretely: in `One(One(a.Next(Once(t)).Next(bodyA), b), c)`, if the
inner `One` picks `a`, `a`'s trigger fires for real the instant the inner
group resolves — regardless of whether the outer `One` later picks `c`
instead, at which point `a`'s branch was actually the overall loser. A
single level of `One` (the case `TestOneDiscardsLosingMemberTrigger` covers)
is unaffected; only nesting breaks it. Documented in detail, including the
fix shape (`fanOut` should check for and queue into an outer
`pendingCommitFrom(ctx)` before executing), in next.md #8. Not fixed this
session — no concrete brain has hit it, it requires deliberately nesting
`One` inside `One` — but flagged as a correctness bug (a loser's trigger can
fire) rather than a missing-feature gap, worth fixing before anyone actually
does that nesting.

Note for future sessions: while verifying the metadata resume test, an
in-session `git checkout -- internal/serve/engine_test.go` (meant to revert a
throwaway sanity-check mutation) discarded the new tests entirely rather than
just the mutation, since they hadn't been committed yet — `git checkout --`
reverts to the last commit, not to "a moment ago." Re-applied from the
already-verified content; no data lost, but a reminder to stash/diff instead
of blind-reverting uncommitted work mid-task.

`go build ./... && go vet ./... && go test ./... -race` green (12 test files
across the module, including the 2 new tests above).

Next: next.md #8 (nested `One`-in-`One`) is the one open item with a known
fix shape not yet built. The Retries/TTL-on-triggered-Durable gap (#2) is
still open too, lower priority (missing feature, not a correctness bug).

## 2026-07-27 — Nested `One`-in-`One` trigger leak fixed (next.md #8)

Built the fix shape next.md #8 already scoped: `fanOut`'s commit-execution
point (`internal/flow/groups.go`, right before running `winnerPC.calls`) now
checks `pendingCommitFrom(ctx)` against the ctx it was itself *entered* with
(not the per-member ctx it derives) — if an outer `pendingCommit` exists
(meaning this `fanOut` is itself running as a member of an enclosing `One`),
the winner's queued `Scheduler.Defer` calls are appended into that outer
`pendingCommit` instead of executed, deferring the decision to whichever
`One` resolves last. A top-level `One`'s ctx never carries one, so the
existing single-level behavior (`TestOneDiscardsLosingMemberTrigger`) is
unchanged; `One(All(...), c)` was already correct and untouched, since `All`
never installs its own `pendingCommit` and just passes the ambient one
through to `deferBody` directly.

Contained diff — one function, ~10 lines, no new types, no signature changes,
reuses `pendingCommit`/`pendingCommitFrom` from trigger.go as-is.

Test: `TestNestedOneDiscardsLosingMemberTrigger`
(`internal/flow/groups_test.go`) — `One(One(a, b), c)`, inner race resolves
with `a` reaching a trigger, outer race picks sibling `c` instead; asserts
`a`'s trigger never committed. Verified it actually catches the bug: reverted
just `groups.go` and reran — test fails against the old code, passes against
the fix.

`go build ./... && go vet ./... && go test ./internal/flow/... -race` green.

Next: no open correctness bugs. Remaining open item is the
Retries/TTL-on-triggered-Durable gap (#2), a missing feature, not blocking
anything.

## 2026-07-27 — Full-codebase review: two new bugs found in `pkg/engine`

Independent read-through review (not tied to a specific reported issue),
covering `internal/flow`, `pkg/engine`, `internal/serve`, `internal/agent`,
and the OpenAI/Anthropic wire conversion code. Overall verdict: code quality
is good (consistent error-wrapping, deliberate doc comments, correct
concurrency bookkeeping in the `One`/`Group`/`All` fan-out logic). Two
correctness bugs found and verified against source, both in
`pkg/engine/engine.go`'s persistence path — written up in `next.md` #1 and
#2 with proposed fixes, not yet built:

- **#1**: `engineScheduler.Defer`'s one-shot (`Once`) branch calls
  `Engine.Enqueue` (random UUID every call) instead of `EnqueueID` with a
  deterministic key, unlike the cron (`Every`) branch. Since triggers
  re-register on every process boot (`wireScheduler` → `RunAtStartup`), a
  `Once` trigger enqueues a fresh pending run on every restart — the same
  "fires more than its contract promises" bug class as #8 above, in the
  one-shot path #8's fix didn't touch.
- **#2**: `Engine.persist` failures during a Sleep/retry requeue are dropped
  with no trace (unlike the unknown-flow branch a few lines above it, which
  traces loudly for a less severe case); and `persist`'s two separate writes
  (run record, then index entry) aren't atomic, so a crash between them
  orphans the run — `load()` only finds runs via the index.

Also flagged, not confirmed as live bugs: nondeterministic reply ordering in
`All`/`Group`/multi-agent fan-out (completion order, not declaration order —
`internal/flow/concurrent.go`, `groups.go`), the structural-signature hash
used for checkpoint staleness (`internal/flow/durable_config.go`) having no
case for trigger nodes, and `checkpoint.save`/`load` not persisting
`State.Req`. Written up in `next.md` #3 (3a/3b/3c) with reachability
questions to answer before promoting any of them to a real bug fix.

Next: build next.md #1 and #2's proposed fixes; investigate #3a/3b/3c.

## 2026-07-27 — Fix #1: `Once` triggers re-firing across restarts

Validated against source before fixing: `next.md` #1's own proposed one-liner
(`EnqueueID(bodyID, bodyID, ...)`, copying the cron branch's pattern) turned
out to be incomplete. Cron's `EnqueueID` dedup works because a cron ticker
re-arms itself under the same ID before it acks, so there is always a live
pending entry for `EnqueueID`'s in-memory check to find. A `Once` trigger
does not re-arm: `engine.ack` deletes both its index entry and its run
record permanently once it fires, with no tombstone (`Cancel`'s own comment
in `pkg/engine/engine.go` says as much — "one-off runs never re-arm, so
cancelling one needs no tombstone at all", which was true before this fix
made one-shot IDs deterministic). So the one-liner only dedups the
"restarted before `Once` fired" case; a restart *after* it already fired and
completed would still look like a fresh schedule and re-fire.

Fix (`internal/serve/engine.go`, `engineScheduler.Defer`'s one-shot branch):
added a permanent tombstone key `once-fired/<bodyID>@<at>` written via the
scheduler's own `flow.Store` (already in hand, no new dependency) right
after a successful `EnqueueID`; checked before scheduling, so a fired-and-
acked `Once` is skipped on every later restart's `Defer` call. Keyed by
`(bodyID, at)`, not `bodyID` alone, so changing the `Once` time in source and
redeploying is treated as a new schedule instead of being silently
swallowed by a stale tombstone from the old time.

Contained: one function, no `pkg/engine` changes, no `flow.Scheduler`
interface change, additive-only store key namespace nothing else reads.
Residual (documented, not fixed): a crash in the narrow window between
`EnqueueID` succeeding and the tombstone `Put` could still allow one re-fire
— same class of non-atomic-write gap as #2 below, judged not worth a
transactional write for a one-shot trigger.

Test: `TestOnceTriggerDoesNotRefireAcrossRestarts`
(`internal/serve/engine_test.go`) — two `Defer` calls before `at` fires
(restart-before-fire), then a third `Defer` call after it already fired
(restart-after-fire); asserts the body runs exactly once total. Verified it
actually catches the bug: ran red against the pre-fix `Enqueue`-based code
("body fired twice for two restart-before-fire Defer calls"), green after
the fix.

`go build ./... && go test ./...` green across all packages.

## 2026-07-27 — Fix #2: `Engine.persist` swallow + non-atomic write (session)

Confirmed both bugs from next.md #2 by tracing the actual code (not just
review notes) before touching anything, then wrote failing tests first.

**2a** (`pkg/engine/engine.go`, `exec`'s requeue branch): a `persist` failure
while requeuing a Sleep/retry yield returned silently — no trace, no log.
The run is already popped off the in-memory heap by `dispatch` at that
point, so this was the only place the failure could ever be observed before
a restart. Fixed by tracing a `StepRecord{Step: "<persist>", Err: ...}`
before returning, matching the unknown-flow branch's existing loud-failure
posture a few lines above.

**2b** (`pkg/engine.go`, `persist`): wrote the run record (`run/<id>`) before
the index entry (`runs/index`). `load()` only discovers runs via the index,
so a crash/store-error between the two writes left a run record that could
never be found again — a permanent, silent orphan. Fixed by flipping the
write order (index first, then run record): the crash-mid-write survivor is
now a stray index entry pointing at a missing run, which `load()` already
treats as stale-and-skippable (pre-existing `if err != nil || !ok || r.ID ==
"" { continue }` guard) instead of a leaked, undiscoverable run record.

Tests added (`pkg/engine/engine_test.go`):
- `TestExecTracesPersistFailureOnRequeue` — `MockStore` fails only the
  `run/<id>` Put during a Sleep-yield requeue; asserts a `<persist>` trace
  record appears. Ran red against pre-fix `exec` (no trace emitted), green
  after.
- `TestPersistDoesNotOrphanRunOnPartialWrite` — `MockStore` fails only the
  `runs/index` Put; asserts no `run/<id>` record survives on disk. Ran red
  against pre-fix write order (run record was written despite the index
  write failing), green after reordering.

Contained: both changes are inside `pkg/engine/engine.go` (`exec` +1 line,
`persist` reordered), no signature or interface changes, no other callers
affected. No `docs/authoring-guide.md` update needed per rule 5 (no `pkg/`
interface/signature touched). `go build ./... && go test ./...` green
across all packages.

## 2026-07-27 — Audit of last ~20 LOG.md entries against actual tests: one gap closed

Went through the last 20+ session entries and, for each described bug fix,
checked the codebase (not git history) for the test it claims exists. All of
them checked out — the named test function is present and passes:
`TestChatRequestMaxOutputTokens`/`TestRequestLegacyMaxTokensStillWorks` (#8's
`max_tokens`/`max_completion_tokens` bug), `TestTriggerDepthThreadsThroughFire`
(cycle-guard worker-ctx gap), `TestTriggerUnnamedBodyErrors`/
`TestTriggerBodyAmbiguousIdErrors`/`TestTriggerBodyResolvesSingleIdAmongMany`/
`TestWebhookHasReplyThroughGroups`/`TestOneDiscardsLosingMemberTrigger`/
`TestTriggeredDurableFlowCheckpoints` (next.md #2/#3/#5/#6),
`TestNestedOneDiscardsLosingMemberTrigger` (#8), and
`TestOnceTriggerDoesNotRefireAcrossRestarts`/
`TestExecTracesPersistFailureOnRequeue`/`TestPersistDoesNotOrphanRunOnPartialWrite`
(the two most recent `pkg/engine` persistence bugs).

**One real gap found:** the 2026-07-26 "Default Store to in-memory instead of
silently disabling triggers" fix (`defaults()` seeding `c.store`, removing the
`if c.store != nil` guard in `build()` and `ErrNoStore` from `Run`) had no test
exercising the actual behavior it fixed — every trigger-related test in
`internal/serve` passes an explicit `Store(engine.NewMemStore())`, and
`internal/serve.Run` had no test calling it at all. A regression (re-adding
the old nil-store guard, or `Run` requiring a store again) would have passed
the full suite silently.

Added `TestRunFiresTriggerWithoutExplicitStore`
(`internal/serve/engine_test.go`) — calls `Run(ctx)` with zero options and
asserts a registered `Trigger().Next(Once(...))` body still fires, proving the
in-memory default actually wires the scheduler end to end. Verified it catches
the regression: reverting `defaults()` to the pre-fix `config{addr: ":8080",
name: "brain", workers: 4}` (no `store: engine.NewMemStore()`) makes it fail
("trigger did not fire" within the timeout), reverted after confirming.

`go build ./... && go vet ./... && go test ./... -race` green across all
packages.

Next: investigate next.md #3a/3b/3c (reachability checks, not fixes yet).
