// Package openai holds the OpenAI chat-completions wire format: request
// decoding, response and SSE-chunk encoding, /models and error bodies.
// Brain authors never see it — it lives in internal/ so the protocol
// handling can change without touching the embeddable pkg/ surface.
//
// Effective Go justification: a small package with one responsibility whose
// design is driven by its single importer (pkg/serve); composite literals
// for wire structs; no interfaces exported because none are needed — it is
// pure encoding, so it also carries no mock.
package openai
