package model

// Usage reports provider-billed token counts for one model call, normalized
// to the Anthropic (disjoint) convention regardless of which provider
// answered: Input counts only UNCACHED input tokens; CacheRead and
// CacheWrite are their own counts, disjoint from Input — so
// Input+CacheRead+CacheWrite is the true total input. Reasoning is a
// breakdown OF Output (a subset of it, per OpenAI/Anthropic's own framing of
// billed reasoning/thinking tokens), never an addition to it — Output
// already includes it.
//
// OpenAI's wire reports the opposite convention (cached_tokens is a SUBSET of
// prompt_tokens), so pkg/model/openai.go computes
// Input = prompt_tokens - cached_tokens - cache_write_tokens at the adapter,
// not here — Usage itself is always already in the disjoint shape.
//
// The zero Usage means "no provider usage was reported" (see
// model.usage.missing), not "zero tokens were spent" — bb never estimates a
// missing count.
type Usage struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Reasoning  int64
}

// Add returns the element-wise sum of two Usages — the only legal way to
// combine them, since every field is a count that sums across model calls,
// sequential chains, and concurrent groups (see docs/design-metrics.md's
// aggregation table).
func (u Usage) Add(o Usage) Usage {
	return Usage{
		Input:      u.Input + o.Input,
		Output:     u.Output + o.Output,
		CacheRead:  u.CacheRead + o.CacheRead,
		CacheWrite: u.CacheWrite + o.CacheWrite,
		Reasoning:  u.Reasoning + o.Reasoning,
	}
}

// Total reports Input+CacheRead+CacheWrite+Output — the total tokens a call
// was billed for. Reasoning is excluded: it is already counted inside Output.
func (u Usage) Total() int64 {
	return u.Input + u.CacheRead + u.CacheWrite + u.Output
}
