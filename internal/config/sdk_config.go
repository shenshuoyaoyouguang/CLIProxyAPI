// Package config provides configuration management for the CLI Proxy API server.
// It handles loading and parsing YAML configuration files, and provides structured
// access to application settings including server port, authentication directory,
// debug settings, proxy configuration, and API keys.
package config

// SDKConfig represents the application's configuration, loaded from a YAML file.
type SDKConfig struct {
	// ProxyURL is the URL of an optional proxy server to use for outbound requests.
	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`

	// DisableImageGeneration controls whether the built-in image_generation tool is injected/allowed.
	//
	// Supported values:
	//   - false (default): image_generation is enabled everywhere (normal behavior).
	//   - true: image_generation is disabled everywhere. The server stops injecting it, removes it from request payloads,
	//     and returns 404 for /v1/images/generations and /v1/images/edits.
	//   - "chat": disable image_generation injection for all non-images endpoints (e.g. /v1/responses, /v1/chat/completions),
	//     while keeping /v1/images/generations and /v1/images/edits enabled and preserving image_generation there.
	//   - "passthrough": do not modify the tool list on non-images endpoints — keep image_generation if the client
	//     sent it and do not inject it otherwise; on /v1/images/generations and /v1/images/edits behave like "chat".
	DisableImageGeneration DisableImageGenerationMode `yaml:"disable-image-generation" json:"disable-image-generation"`

	// GPTImage2BaseModel sets the base (mainline) model used by the legacy hosted
	// image_generation tool path when a Codex image request is not proxied directly
	// through the Image API.
	//
	// The value must start with "gpt-" (case-insensitive). If empty or invalid, the
	// default base model ("gpt-5.4-mini") is used.
	GPTImage2BaseModel string `yaml:"gpt-image-2-base-model,omitempty" json:"gpt-image-2-base-model,omitempty"`

	// VideoResultAuthCacheTTL controls how long video IDs stay pinned to the credential
	// that created them. Accepts duration strings like "30m" or "3h".
	// Empty or invalid values use the default 3h.
	VideoResultAuthCacheTTL string `yaml:"video-result-auth-cache-ttl,omitempty" json:"video-result-auth-cache-ttl,omitempty"`

	// ForceModelPrefix requires explicit model prefixes (e.g., "teamA/gemini-3-pro-preview")
	// to target prefixed credentials. When false, unprefixed model requests may use prefixed
	// credentials as well.
	ForceModelPrefix bool `yaml:"force-model-prefix" json:"force-model-prefix"`

	// RequestLog enables or disables detailed request logging functionality.
	RequestLog bool `yaml:"request-log" json:"request-log"`

	// APIKeys is a list of keys for authenticating clients to this proxy server.
	APIKeys []string `yaml:"api-keys" json:"api-keys"`

	// PassthroughHeaders controls whether upstream response headers are forwarded to downstream clients.
	// Default is false (disabled).
	PassthroughHeaders bool `yaml:"passthrough-headers" json:"passthrough-headers"`

	// Streaming configures server-side streaming behavior (keep-alives and safe bootstrap retries).
	Streaming StreamingConfig `yaml:"streaming" json:"streaming"`

	// NonStreamKeepAliveInterval controls how often blank lines are emitted for non-streaming responses.
	// <= 0 disables keep-alives. Value is in seconds.
	NonStreamKeepAliveInterval int `yaml:"nonstream-keepalive-interval,omitempty" json:"nonstream-keepalive-interval,omitempty"`
}

// StreamingConfig holds server streaming behavior configuration.
type StreamingConfig struct {
	// KeepAliveSeconds controls how often the server emits SSE heartbeats (": keep-alive\n\n").
	// <= 0 disables keep-alives. Default is 0.
	KeepAliveSeconds int `yaml:"keepalive-seconds,omitempty" json:"keepalive-seconds,omitempty"`

	// BootstrapRetries controls how many times the server may retry a streaming request before any bytes are sent,
	// to allow auth rotation / transient recovery.
	// <= 0 disables bootstrap retries. Default is 0.
	BootstrapRetries int `yaml:"bootstrap-retries,omitempty" json:"bootstrap-retries,omitempty"`

	// StreamRetryEnabled enables OpenAI-compatible executor retries for streams
	// that fail before any SSE data is received. Default is false because
	// compatible upstreams may not honor idempotency keys.
	StreamRetryEnabled bool `yaml:"stream-retry-enabled,omitempty" json:"stream-retry-enabled,omitempty"`

	// StreamRetryCount controls the maximum number of retry attempts for a stream that
	// disconnects before any SSE data is received. Each retry applies exponential backoff
	// with jitter and degrades reasoning_effort if applicable.
	// Only applies when StreamRetryEnabled is true. <= 0 uses the default of 2.
	// Set to 1 to disable retries (only the initial attempt).
	StreamRetryCount int `yaml:"stream-retry-count,omitempty" json:"stream-retry-count,omitempty"`

	// StreamRetryDegradeAfter is the number of retry attempts to keep the
	// original request body (backoff only, no reasoning_effort change) before
	// starting to degrade. 0 = degrade on the first retry (legacy). 1 = first
	// retry uses original body, later retries degrade. Negative values are
	// treated as 0. Unset (nil) means the executor uses the default from
	// helps.DefaultStreamRetryConfig (currently 1).
	StreamRetryDegradeAfter *int `yaml:"stream-retry-degrade-after,omitempty" json:"stream-retry-degrade-after,omitempty"`

	// CompletionsEmptyChunkPolicy controls how /v1/completions streaming handles
	// upstream chat-completions chunks whose delta has no text content and no
	// usage field. Filtering such chunks changes the original timeline; some
	// clients rely on the empty chunks to reconstruct timing/keep-alive signals.
	// Allowed values:
	//   - "filter"  (default): drop empty chunks (legacy behavior)
	//   - "preserve": keep empty chunks in original order, text field is ""
	//   - "mark"    : keep empty chunks and add an "empty": true marker field
	// Any other/empty value falls back to "filter".
	CompletionsEmptyChunkPolicy string `yaml:"completions-empty-chunk-policy,omitempty" json:"completions-empty-chunk-policy,omitempty"`
}

// Completions empty chunk policy string constants.
const (
	CompletionsEmptyChunkPolicyFilter   = "filter"
	CompletionsEmptyChunkPolicyPreserve = "preserve"
	CompletionsEmptyChunkPolicyMark     = "mark"
)
