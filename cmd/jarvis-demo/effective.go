// Command jarvis-demo is an executable, not a library: it only bridges the OS
// (signals, env) to a brain assembled from pkg/bb. Effective Go justifies a
// main package that is thin wiring — no exported surface, no interfaces of its
// own, nothing importable. Everything reusable lives in pkg/bb; what lives here
// is one house's worth of policy: which capabilities exist, which routines run
// at which hour, and a dummy world to act on so the demo needs nothing external.
package main
