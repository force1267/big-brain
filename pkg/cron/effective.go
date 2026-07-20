// Package cron is a recurring-schedule primitive: the Cron declaration and
// the pure function that computes its next firing time. It has no
// knowledge of pipelines, jobs, or HTTP — a brain declares Crons, and
// whatever runs the schedule (pkg/serve today) calls Next to find out
// when to fire next.
//
// Effective Go justification: a small, single-purpose package split out
// once its algorithm (interval and daily-of-day math) outgrew being a
// helper function embedded in an unrelated package; a pure function is
// preferred over a stateful type since Cron itself carries no behavior,
// only declaration.
package cron
