// Package anthropic holds the Anthropic messages wire format: request
// decoding (string-or-blocks content), non-streaming responses, the
// message_start/delta/stop SSE event sequence, and error bodies. Like its
// sibling internal/openai, it lives in internal/ so protocol handling can
// change without touching the embeddable pkg/ surface.
//
// Effective Go justification: one responsibility, design driven by its
// single importer (pkg/serve); UnmarshalJSON on Content keeps the dual
// wire form at the boundary instead of leaking a union type inward; pure
// encoding, so no interfaces and no mock.
package anthropic
