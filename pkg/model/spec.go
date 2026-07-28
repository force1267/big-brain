package model

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// ErrNoModelName is returned by Build when a Spec has no name set.
var ErrNoModelName = errors.New("model: no name set")

// Spec is a model configuration built fluently: WithName/WithThink/
// WithTemprature. It is a value type with value receivers, so every With call
// returns an independent copy — a Spec seeded from the registry can be
// overridden without mutating the registered one. Spec is what the bb facade
// exposes as bb.Model; the runtime Model (Stream) is produced by Build.
type Spec struct {
	name     string
	provider Provider
	think    bool
	thinkSet bool
	temp     float64
	tempSet  bool
	err      error // a recorded construction error (e.g. unknown tag); surfaced at Build
	built    Model // a pre-bound model (Bound); when set, Build returns it directly
}

// Provider selects which consuming client Build resolves a Spec to. The zero
// value is OpenAIProvider, so existing Specs (and the OpenAI-compatible
// default) are unaffected.
type Provider int

const (
	OpenAIProvider Provider = iota
	AnthropicProvider
)

// Bound returns a Spec whose Build yields the given model directly, bypassing
// provider resolution. It exists so callers (and tests) can inject a specific
// or fake Model where a Spec is expected.
func Bound(m Model) Spec { return Spec{built: m} }

// WithName sets the backing provider model name (e.g. "gpt-4o-mini").
func (s Spec) WithName(name string) Spec { s.name = name; return s }

// WithThink toggles the model's reasoning/thinking mode.
func (s Spec) WithThink(on bool) Spec { s.think, s.thinkSet = on, true; return s }

// WithProvider selects which consuming client Build resolves the name
// against. Unset (the zero value) is OpenAIProvider, so a Spec that never
// calls this behaves exactly as it always has.
func (s Spec) WithProvider(p Provider) Spec { s.provider = p; return s }

// WithTemprature sets the sampling temperature. (Spelling matches the API the
// goal-post main.go uses.)
func (s Spec) WithTemprature(t float64) Spec { s.temp, s.tempSet = t, true; return s }

// Name reports the configured provider model name.
func (s Spec) Name() string { return s.name }

// IsSet reports whether the Spec has anything configured (a name, a bound
// model, or a recorded error) — i.e. whether the author meant to give the agent
// a model. A zero Spec (IsSet false) means "no model", which is valid for an
// agent that only routes and never asks.
func (s Spec) IsSet() bool { return s.name != "" || s.built != nil || s.err != nil }

// Think reports the thinking toggle and whether it was set.
func (s Spec) Think() (on, set bool) { return s.think, s.thinkSet }

// Err reports a recorded construction error (e.g. NewModel of an unknown tag),
// which Build also returns. Builders never fail mid-chain; the error waits here
// until Build/Serve reads it.
func (s Spec) Err() error { return s.err }

// Params translates the sampling settings into provider Params. Fields not set
// on the Spec are left nil ("not sent").
func (s Spec) Params() Params {
	var p Params
	if s.tempSet {
		t := s.temp
		p.Temperature = &t
	}
	if s.thinkSet {
		t := s.think
		p.Think = &t
	}
	return p
}

// buildKey identifies a (provider, endpoint, credential, name) tuple: every
// unbound Spec resolving to the same tuple can share one built Model.
type buildKey struct {
	provider Provider
	baseURL  string
	apiKey   string
	name     string
}

// builtModels memoizes Build's result per buildKey, so an agent re-resolving
// its Spec on every Ask (internal/agent/chat.go's Asker.Ask) reuses the same
// client and OTel instruments instead of constructing them per call — see
// docs/design-metrics.md's finding on per-Ask client/instrument churn.
var builtModels sync.Map // buildKey -> Model

// Build resolves the Spec to a runtime Model. It surfaces the recorded error
// first, then requires a name; otherwise it returns an OpenAI-compatible model
// pointed at the configured provider (BIG_BRAIN_BASE_URL / BIG_BRAIN_API_KEY),
// memoized by (provider, endpoint, credential, name) so repeated Builds of an
// equivalent Spec reuse one client rather than constructing a fresh one.
func (s Spec) Build() (Model, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.built != nil {
		return s.built, nil
	}
	if s.name == "" {
		return nil, ErrNoModelName
	}
	baseURL, apiKey := os.Getenv("BIG_BRAIN_BASE_URL"), os.Getenv("BIG_BRAIN_API_KEY")
	key := buildKey{provider: s.provider, baseURL: baseURL, apiKey: apiKey, name: s.name}
	if m, ok := builtModels.Load(key); ok {
		return m.(Model), nil
	}
	var m Model
	if s.provider == AnthropicProvider {
		m = Anthropic(baseURL, apiKey, s.name)
	} else {
		m = OpenAI(baseURL, apiKey, s.name)
	}
	actual, _ := builtModels.LoadOrStore(key, m)
	return actual.(Model), nil
}

// withErr returns a copy carrying err (used by the registry for an unknown-tag
// lookup, so the failure surfaces at Build rather than as a silent blank spec).
func (s Spec) withErr(err error) Spec { s.err = err; return s }

// invalidTags builds the unknown-tag error a missing lookup records.
func invalidTags(tags []string) error {
	return fmt.Errorf("%w: %v", ErrUnknownModelTags, tags)
}
