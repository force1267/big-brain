package agent

import "context"

// The payload is arbitrary trigger-specific data carried on the run — a webhook
// body, a cron's seed, whatever a custom entry point provides. It rides the
// context as raw JSON so it survives being scheduled to a worker and replayed;
// the typed getter is bb.Payload[T] (a free function, since Go methods can't be
// generic — the Extract/Schema precedent). turn.Request is the protocol envelope;
// this is the open-ended companion.
type payloadKey struct{}

// WithPayload attaches raw payload bytes to ctx (the flow sets this from a
// trigger seed or a fired body's captured payload).
func WithPayload(ctx context.Context, raw []byte) context.Context {
	if len(raw) == 0 {
		return ctx
	}
	return context.WithValue(ctx, payloadKey{}, raw)
}

// PayloadFrom reads the raw payload off ctx (nil if none).
func PayloadFrom(ctx context.Context) []byte {
	b, _ := ctx.Value(payloadKey{}).([]byte)
	return b
}

// Payload returns this turn's raw trigger payload, decoded by bb.Payload[T].
func (t *Turn) Payload() []byte { return PayloadFrom(t.ctx) }
