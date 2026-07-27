package agent

import "context"

// Metadata is out-of-band data alongside the payload — HTTP headers for a
// webhook, or whatever a non-HTTP trigger seeds — carried the same way
// payload is: raw JSON on ctx, surviving scheduling/replay. The typed getter
// is bb.Metadata[T] (a free function, same reason as bb.Payload[T]). Kept as
// its own ctx key rather than merged into Payload's T: merging by field-name
// match across two sources (body vs headers) risks a field colliding by
// accident and silently pulling from the wrong source (next.md #7); a
// separate channel makes that impossible by construction.
type metadataKey struct{}

// WithMetadata attaches raw metadata bytes to ctx (the flow sets this from a
// trigger's seed, or a webhook's flattened request headers).
func WithMetadata(ctx context.Context, raw []byte) context.Context {
	if len(raw) == 0 {
		return ctx
	}
	return context.WithValue(ctx, metadataKey{}, raw)
}

// MetadataFrom reads the raw metadata off ctx (nil if none).
func MetadataFrom(ctx context.Context) []byte {
	b, _ := ctx.Value(metadataKey{}).([]byte)
	return b
}

// Metadata returns this turn's raw out-of-band metadata, decoded by
// bb.Metadata[T].
func (t *Turn) Metadata() []byte { return MetadataFrom(t.ctx) }
