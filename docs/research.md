# Technology research

Goal: Go wrapper around LLM/Voice/Vision models, exposing OpenAI/Anthropic
compatible APIs and embeddable as a library. Constraints: open source,
permissive license, not gated behind a paid vendor.

## Consuming model providers (SDK clients)

| Option | License | Verdict |
|---|---|---|
| `github.com/openai/openai-go` | Apache-2.0 | **Chosen.** Official, generated from the live OpenAPI spec, covers chat, streaming (SSE), audio (voice), images/vision, realtime. |
| `github.com/sashabaranov/go-openai` | MIT | Community, mature, but lags behind API surface; official SDK superseded it. |
| `github.com/anthropics/anthropic-sdk-go` | MIT | **Chosen.** Official Anthropic SDK: messages, streaming, tools, vision. |

Both official SDKs allow overriding `BaseURL`, so they double as clients for
any *compatible* upstream (Ollama, vLLM, OpenRouter…) — one SDK, many vendors.

## Producing compatible APIs (server side)

There is no battle-tested "OpenAI server SDK" for Go worth depending on
(existing ones are small/unmaintained). The chosen approach — proven by
Ollama, LocalAI and LiteLLM — is to serve the endpoints ourselves and reuse
the official SDKs' request/response **types** for wire compatibility
(`openai-go` and `anthropic-sdk-go` param/response structs marshal to the
exact wire format). Endpoints are plain HTTP + SSE streaming.

## HTTP / net / WebSocket

| Option | License | Verdict |
|---|---|---|
| stdlib `net/http` | BSD-3 | **Chosen.** Go 1.22+ has method+wildcard routing; SSE is trivial; this is what the ecosystem serves model APIs with. Zero dependency, maximally battle-tested. |
| `gin`, `echo`, `chi` | MIT | Not needed; routing/middleware needs are met by stdlib. Revisit only if middleware sprawl hurts. |
| `github.com/coder/websocket` (ex-nhooyr) | ISC | **Chosen** for realtime/voice websockets. Minimal, idiomatic, context-aware, actively maintained. |
| `gorilla/websocket` | BSD-2 | Fine too; coder/websocket has the cleaner context-based API. |

## Cross-cutting

- Logs: `sirupsen/logrus` (MIT) — required by project rules, matches gateway.
- Config: `spf13/viper` (MIT) — required; env-first, 12-factor, prefix `BIG_BRAIN_`.
- Metrics/traces: `go.opentelemetry.io/otel` (Apache-2.0) — noop locally, OTLP gRPC in production.

All choices are permissive (MIT/Apache-2.0/ISC/BSD) with no paid tier behind them.
