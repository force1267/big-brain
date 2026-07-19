// Package model defines the provider-neutral chat types, the Model
// interface a backing provider satisfies, and the Role indirection that
// keeps brain code portable: nodes name a role, deployment config binds it.
//
// Effective Go justification: a small, single-purpose package named for the
// client's call site (model.Role, model.OpenAI — no stutter); a one-purpose
// interface with a single method, satisfied implicitly by provider
// implementations; errors are sentinel values wrapped with %w; the streamed
// result is delivered over a channel ("share memory by communicating") whose
// producing goroutine exits on context cancellation.
package model
