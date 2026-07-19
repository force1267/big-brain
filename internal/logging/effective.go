// Package logging configures the process-wide logrus logger from Config:
// level and output format (text locally, json in production).
//
// What it is: the only place that touches logrus global configuration.
//
// What it does: exposes an Initializer that applies log settings once at
// startup; every other package just calls logrus and inherits them.
//
// Effective Go justification: a small, single-purpose package whose name
// reads naturally at the call site (logging.New().Init(cfg)). Keeping the
// single mutation of global logger state in one package preserves the
// "useful zero value" of logrus everywhere else and keeps side effects
// out of init() functions, as Effective Go advises.
package logging
