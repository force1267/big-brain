# Effective Go — distilled rules (ABSOLUTE, follow with utmost care)

Source: `docs/effective_go.html` (https://go.dev/doc/effective_go). This file
is the machine-readable rule set every LLM session must obey when writing Go
in this repo.

## Formatting & style

- `gofmt` formats everything; never hand-format. Tabs, no line-length dogma.
- `go vet` must pass.

## Naming

- Package names: short, lower-case, single-word, no under_scores or mixedCaps
  (`config`, `telemetry` — never `configUtils`).
- The package name is part of the API: `config.Load`, not `config.LoadConfig`.
- Getters drop `Get`: `obj.Owner()`, setter `obj.SetOwner(...)`.
- One-method interfaces end in `-er` (`Reader`, `Loader`).
- MixedCaps / mixedCaps, never underscores. Exported = Capitalized.

## Commentary

- Every exported name has a doc comment starting with that name.
- Package comment precedes the package clause (here: lives in `effective.go`).

## Control structures & idioms

- No unnecessary `else` after `return`; early-return error handling.
- `if err != nil { return ... }` — errors handled immediately, success path
  stays unindented.
- Use `for range`, `switch` (no fallthrough by default), type switches.
- Short variable declarations `:=` inside functions; `var` for zero values.

## Functions & methods

- Multiple return values instead of out-params; named results only when they
  clarify.
- `defer` for cleanup next to acquisition (files, locks, spans).
- Value vs pointer receivers: be consistent per type; pointer when mutating
  or large.

## Data

- `new(T)` vs `make`: `make` only for slices, maps, channels.
- Design zero values to be useful ("give your structs a useful zero value").
- Composite literals over field-by-field construction.
- Slices carry length+capacity; append idioms; copy when escaping ownership.

## Initialization

- Constants with `iota` where enumerating.
- `init()` only for setup that cannot be expressed as declarations —
  this repo restricts it further: only when genuinely needed.

## Interfaces

- Interfaces are satisfied implicitly; define them where they are *used*,
  keep them small (this repo: ≤3 methods, ≤2 with active logic, ≤1 marker).
- Accept interfaces, return concrete types (except constructors that
  deliberately return the package interface).
- Interface checks like `var _ Loader = (*envLoader)(nil)` when the compiler
  can't catch it.

## Errors

- Errors are values; `error` last return value.
- Error strings: lower-case, no trailing punctuation.
- Wrap with `%w`; sentinel errors as package `var ErrX = errors.New(...)`.
- `panic` only for truly unrecoverable programmer errors; libraries never
  panic across their API; `recover` only at goroutine boundaries you own.

## Concurrency

- "Share memory by communicating": channels to transfer ownership.
- Goroutines need a known exit path (context cancellation); never leak.
- `sync` primitives for simple shared state; channels for orchestration.

## Packages & project shape

- Small, single-purpose packages; the importer's view drives the design.
- `internal/` for private code, `pkg/` (by this repo's convention) for the
  embeddable library surface.
- Avoid stutter: `wrapper.Wrapper` is a smell; name for the client's call site.
