// Package bb is the brain-builder: the one package a brain author imports. It
// is a thin facade — interfaces, type aliases, and constructors — that wires
// together the real concerns living in their own packages (model, agent, flow,
// trace, serve, engine). See BB.md for the architecture and cmd/marvis-demo for
// the authoring surface it exposes.
//
// Why this package exists (Effective Go): a library's public surface is itself
// a single responsibility — naming and composing the pieces ergonomically —
// kept separate from the implementations so the surface can stay small and the
// implementations can change behind it. bb holds no business logic; every call
// here delegates. The small value types that are pure data with no separate
// concern to justify a package of their own (Prompt templates, typed Schema)
// are the only things bb implements directly.
package bb
