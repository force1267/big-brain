// Package telemetry owns OpenTelemetry setup: a noop provider for local
// runs and an OTLP gRPC metric exporter for production, selected by
// configuration. It also documents the repo-wide MonitoredX convention:
// any package with a metric-bearing interface adds a telemetry.go that
// wraps its interface (NewMonitoredX(x X) X) and returns the wrapped
// value from its New when telemetry is enabled.
//
// What it is: the single place that constructs and shuts down OTel
// providers.
//
// What it does: exposes Provider (Start/Shutdown) so main-adjacent wiring
// can bring metrics up and tear them down gracefully.
//
// Effective Go justification: single-responsibility package, implicit
// small interface (2 active methods), deferred cleanup via Shutdown
// mirroring the acquire/defer-release idiom, and a useful noop zero
// implementation so local development needs no infrastructure.
package telemetry
