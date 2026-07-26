// Package openai holds the OpenAI chat-completions wire format: request
// decoding, response and SSE-chunk encoding, /models and error bodies.
// Brain authors never see it — it lives in internal/ so the protocol
// handling can change without touching the embeddable pkg/ surface.
//
// Tool use is part of that translation and stays here: this format frames a
// tool result as its own role:"tool" message and a call's arguments as a JSON
// string, while the neutral form hangs both off a Message as typed payloads.
// Converting between the two is exactly this package's job, so an author never
// meets a provider's framing.
//
// Effective Go justification: a small package with one responsibility whose
// design is driven by its single importer (pkg/serve); composite literals
// for wire structs; no interfaces exported because none are needed — it is
// pure encoding, so it also carries no mock.
package openai
