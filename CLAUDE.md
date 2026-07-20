# wrapper — rules for AI agents

Go project: a wrapper around LLM/Voice/Vision models that serves OpenAI- and
Anthropic-compatible APIs and is also embeddable as a library.

## Absolute rules (read before writing any code)

1. **Log everything you do** in `LOG.md` (append a dated session entry). Other
   LLM sessions rely on it to understand history.
2. **All textual artifacts are local markdown** (plans, research, logs).
3. **Effective Go is law.** The article is saved at `docs/effective_go.html`;
   the distilled, enforceable rules are in `docs/effective-go-rules.md`.
   Follow them with utmost care.
4. **Every package has an `effective.go`** file: no imports, comments only,
   explaining what the package is, does, and how Effective Go justifies its
   existence.
5. **Docs move with the code.** Any change to a `pkg/` interface, exported
   function/type signature, or core concept (trigger, node, brain, dynamism
   ladder, persistence promise) must update `docs/authoring-guide.md` in the
   same change — not a follow-up. If the guide would be wrong after your
   diff, the diff isn't done.

## Architecture rules

- Everything is an implementation of an interface.
- Interfaces: max 3 methods — at most 2 with active logic, and 1 may be a
  marker/compat method for type-system tricks.
- Packages are small, single-responsibility (Effective Go). Business-logic
  packages are pure wiring of interfaces; infrastructure is separate.
- Every package exporting an interface has a `mock.go` with a mock
  implementation for test injection.
- Tests cover happy, unhappy, edge cases and every branch. Test quality over
  coverage numbers.
- Cross-cutting: `logrus` for logs, `viper` for config, 12-factor — anything
  environment-dependent comes from env vars (prefix `BIG_BRAIN_`).
- Errors: each package defines sentinel errors (`var ErrX = errors.New(...)`)
  and wraps causes: `fmt.Errorf("%w: %w", ErrX, err)`. Every layer adds its
  own context on the way up.
- OTel for metrics: noop/stdout locally, OTLP in production, selected by env.
- Metric-bearing interfaces get a wrapper in the package's `telemetry.go`
  (`MonitoredX` wrapping `X`); the package's `New...` returns the wrapped one
  when telemetry is enabled.
- `init()` only when genuinely needed.
- `cmd/` builds executables; `main()` only bridges to the OS (flags/args) and
  calls entry points in `internal/`. All initialization lives in `internal/`.
- `internal/` = this project's private concerns; `pkg/` = the embeddable
  library surface.
- Style influence: `~/Desktop/projects/gateway/src` (logs, errors, metrics DX)
  — be inspired, but be better; not necessarily the same interfaces.

## Tech choices (see docs/research.md for rationale)

- HTTP/WebSocket serving: stdlib `net/http` (Go 1.22+ routing) + `coder/websocket` (ISC).
- Consuming providers: `openai/openai-go` (Apache-2.0), `anthropics/anthropic-sdk-go` (MIT).
- Logs `sirupsen/logrus` (MIT), config `spf13/viper` (MIT), metrics `go.opentelemetry.io/otel` (Apache-2.0).
