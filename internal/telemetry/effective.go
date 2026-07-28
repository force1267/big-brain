// Package telemetry owns the one piece of process-wide state every
// Monitored wrapper in the codebase depends on but none of them should
// configure: the global OTel MeterProvider. pkg/model, internal/agent, and
// internal/serve all build their instruments against otel.Meter(...)
// unconditionally, at construction — by design, per CLAUDE.md's metrics
// rule — so the wrapping itself must never know whether telemetry is
// enabled. Something still has to decide that, once, and this package is it.
//
// Start reads BIG_BRAIN_TELEMETRY (unset/anything else -> noop, "stdout" ->
// a local stdout exporter, "otlp" -> BIG_BRAIN_OTLP_ENDPOINT) and installs
// the matching MeterProvider globally. serve.Serve calls it once at startup
// and defers the returned shutdown — the only two places this package is
// ever touched.
//
// Effective Go justification: a small, single-purpose package (one exported
// function) that owns exactly the global, process-wide state its job
// requires and nothing else; no interface, because there is exactly one
// implementation and no seam anyone needs — Start either installs a real
// SDK MeterProvider or leaves the existing one in place, decided by a plain
// os.Getenv switch, matching the rest of the codebase's BIG_BRAIN_* config
// convention (pkg/model/spec.go, cmd/jarvis-demo/main.go) rather than
// introducing viper for a single value nothing else in the repo actually
// uses.
package telemetry
