package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Environments the app recognizes. Anything else fails validation.
const (
	EnvLocal      = "local"
	EnvProduction = "production"
)

var (
	// ErrLoad wraps any failure while reading configuration.
	ErrLoad = errors.New("config load failed")
	// ErrInvalidEnv is returned when BIG_BRAIN_ENV is not a known environment.
	ErrInvalidEnv = errors.New("invalid environment")
	// ErrInvalidModels is returned when BIG_BRAIN_MODELS is not role=model pairs.
	ErrInvalidModels = errors.New("invalid models binding")
	// ErrInvalidSpeakers is returned when BIG_BRAIN_SPEAKERS is not key=name pairs.
	ErrInvalidSpeakers = errors.New("invalid speakers binding")
)

// Config holds every environment-derived setting, ready to be passed by value.
type Config struct {
	Env string

	HTTP struct {
		Addr string
	}

	Log struct {
		Level  string
		Format string // "text" or "json"
	}

	Telemetry struct {
		Enabled     bool
		Endpoint    string // OTLP gRPC endpoint, e.g. "localhost:4317"
		ServiceName string
	}

	// Upstream is the OpenAI-compatible endpoint backing the model roles.
	Upstream struct {
		BaseURL string // empty means the provider SDK's default
		APIKey  string
	}

	// Models binds brain-declared roles to upstream model names, parsed
	// from BIG_BRAIN_MODELS, e.g. "fast=gpt-4o-mini,smart=gpt-4o".
	Models map[string]string

	// Memory is where the zero-setup fact store lives.
	Memory struct {
		Path string
	}

	// Jobs is where the zero-setup durable job log lives.
	Jobs struct {
		Path string
	}

	// Notify configures the outgoing-webhook channel; empty URL means
	// notifications are logged instead of delivered.
	Notify struct {
		URL string
	}

	// Speakers maps API keys to speaker names, parsed from
	// BIG_BRAIN_SPEAKERS, e.g. "key-dad=dad,key-kid=kid". Empty means all
	// callers are anonymous.
	Speakers map[string]string
}

// Loader loads configuration. Implementations must be safe to call once at
// startup; the returned Config is a value and never mutated afterwards.
type Loader interface {
	Load() (Config, error)
}

// New returns the environment-variable backed Loader.
func New() Loader { return envLoader{} }

type envLoader struct{}

var _ Loader = envLoader{}

func (envLoader) Load() (Config, error) {
	v := viper.New()
	v.SetEnvPrefix("BIG_BRAIN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("env", EnvLocal)
	v.SetDefault("http.addr", ":8080")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("telemetry.enabled", false)
	v.SetDefault("telemetry.endpoint", "localhost:4317")
	v.SetDefault("telemetry.service_name", "big-brain")

	var c Config
	c.Env = v.GetString("env")
	c.HTTP.Addr = v.GetString("http.addr")
	c.Log.Level = v.GetString("log.level")
	c.Log.Format = v.GetString("log.format")
	c.Telemetry.Enabled = v.GetBool("telemetry.enabled")
	c.Telemetry.Endpoint = v.GetString("telemetry.endpoint")
	c.Telemetry.ServiceName = v.GetString("telemetry.service_name")
	c.Upstream.BaseURL = v.GetString("upstream.base_url")
	c.Upstream.APIKey = v.GetString("upstream.api_key")

	models, err := parsePairs(v.GetString("models"), ErrInvalidModels)
	if err != nil {
		return Config{}, fmt.Errorf("%w: %w", ErrLoad, err)
	}
	c.Models = models

	v.SetDefault("memory.path", "memory.jsonl")
	c.Memory.Path = v.GetString("memory.path")
	v.SetDefault("jobs.path", "jobs.jsonl")
	c.Jobs.Path = v.GetString("jobs.path")
	c.Notify.URL = v.GetString("notify.url")

	speakers, err := parsePairs(v.GetString("speakers"), ErrInvalidSpeakers)
	if err != nil {
		return Config{}, fmt.Errorf("%w: %w", ErrLoad, err)
	}
	c.Speakers = speakers

	if c.Env != EnvLocal && c.Env != EnvProduction {
		return Config{}, fmt.Errorf("%w: %w: %q", ErrLoad, ErrInvalidEnv, c.Env)
	}
	return c, nil
}

func parsePairs(s string, invalid error) (map[string]string, error) {
	pairs := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, val, ok := strings.Cut(pair, "=")
		if !ok || k == "" || val == "" {
			return nil, fmt.Errorf("%w: %q", invalid, pair)
		}
		pairs[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	return pairs, nil
}
