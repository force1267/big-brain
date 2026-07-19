# big-brain

A Go wrapper around LLM, Voice, and Vision models that serves
OpenAI- and Anthropic-compatible APIs, and can be embedded as a library
(`pkg/`).

Status: skeleton — cross-cutting concerns (config, logging, telemetry) are
in place; product features are being discovered. See `LOG.md` for project
history, `CLAUDE.md` for the rules AI agents follow here, and
`docs/research.md` for the technology choices.

## Build & test

```sh
go build ./...
go test ./...
go run ./cmd/wrapper serve
```

Configuration is env-only (12-factor), prefix `WRAPPER_` — e.g.
`WRAPPER_ENV=production`, `WRAPPER_LOG_FORMAT=json`,
`WRAPPER_TELEMETRY_ENABLED=true`.
