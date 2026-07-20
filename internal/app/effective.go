// Package app is pure wiring: it connects config, logging and telemetry
// interfaces into a running process. It contains no business logic and no
// infrastructure code of its own — only composition of the interfaces the
// other internal packages export.
//
// What it is: the program's starting point, called by cmd/cli's main.
//
// What it does: load config, initialize logging, start telemetry, and (in
// later steps) start the API server; shut everything down on context
// cancellation.
//
// Effective Go justification: main packages should stay thin; keeping
// composition here lets it be tested with mocks (implicit interfaces make
// injection free) while cmd/cli/main.go only bridges the OS to Run.
package app
