// Package config loads all runtime configuration from environment
// variables (12-factor, prefix BIG_BRAIN_) via viper and exposes it as a
// plain Config value behind the Loader interface.
//
// What it is: the single source of truth for environment-derived settings.
//
// What it does: reads env vars, applies defaults, validates, and returns
// an immutable Config struct. Nothing else in the repo touches viper.
//
// Effective Go justification: "packages should be small and single
// purpose" and named for what the client says at the call site —
// config.Load reads naturally and does not stutter. Isolating the only
// side-effectful configuration read keeps every other package a pure
// function of its inputs, which is the implicit-interface, useful-zero-
// value style Effective Go prescribes.
package config
