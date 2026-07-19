// Package notify carries the brain's initiative outward: the Channel
// interface for outgoing notifications, the v1 built-in outgoing-webhook
// channel, and a logging fallback so an unconfigured channel never drops a
// message silently. Channels are an extensible family per PRODUCT.md.
//
// Effective Go justification: a one-method interface defined where it is
// used and satisfied implicitly by webhook, log, and future channels; the
// package name reads at the call site (notify.Webhook, notify.Message);
// sentinel errors wrapped with %w; the webhook request honors context
// cancellation.
package notify
