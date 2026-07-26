// Package anthropic holds the Anthropic messages wire format: request
// decoding (string-or-blocks content), non-streaming responses, the
// message_start/delta/stop SSE event sequence, and error bodies. Like its
// sibling internal/openai, it lives in internal/ so protocol handling can
// change without touching the embeddable pkg/ surface.
//
// Tool use is part of that translation and stays here: this format nests
// tool_use and tool_result inside a message's CONTENT BLOCKS, so decoding
// content is where tool interactions are found — a different shape from the
// neutral form and from the other wire format, which is the whole reason the
// conversion lives at the edge rather than in the core.
//
// Effective Go justification: one responsibility, design driven by its
// single importer (pkg/serve); UnmarshalJSON on Content keeps the dual
// wire form at the boundary instead of leaking a union type inward; pure
// encoding, so no interfaces and no mock.
package anthropic
