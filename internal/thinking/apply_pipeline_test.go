// Package thinking end-to-end pipeline tests.
//
// These tests exercise the full ApplyThinking() entry point (suffix parsing →
// model lookup → config extraction → validation → provider application) rather
// than a single provider applier in isolation. They lock in the cross-cutting
// contracts that unit tests on individual appliers cannot cover:
//   - suffix override priority over body config
//   - cross-provider-family conversion (clamp vs error)
//   - passthrough for unknown providers / non-thinking models
//   - defensive body return on validation failure
package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	// Register native provider appliers via package init side effects.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/codex"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/deepseek"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/gemini"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/openai"
	"github.com/tidwall/gjson"
)

// registerModels registers a client with the given models against the global
// registry and returns a cleanup func. Using the real registry (rather than a
// hand-built ModelInfo) ensures the pipeline exercises the same LookupModelInfo
// path as production.
func registerModels(t *testing.T, clientID, provider string, models []*registry.ModelInfo) {
	t.Helper()
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(clientID, provider, models)
	t.Cleanup(func() { reg.UnregisterClient(clientID) })
}

// TestApplyThinking_SuffixOverridesBody verifies that a model-name suffix takes
// priority over thinking config already present in the request body.
func TestApplyThinking_SuffixOverridesBody(t *testing.T) {
	registerModels(t, "test-pipe-suffix", "gemini", registry.GetGeminiModels())

	// Body asks for a small budget; suffix asks for a larger one. Suffix wins.
	body := []byte(`{"generationConfig":{"thinkingConfig":{"thinkingBudget":1024}}}`)
	out, err := thinking.ApplyThinking(body, "gemini-2.5-pro(4096)", "gemini", "gemini", "gemini")
	if err != nil {
		t.Fatalf("ApplyThinking error: %v", err)
	}
	got := gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingBudget").Int()
	if got != 4096 {
		t.Fatalf("suffix should override body budget: got %d, want 4096 (%s)", got, out)
	}
}

// TestApplyThinking_UnknownProviderPassthrough verifies that an unregistered
// provider returns the body unchanged with no error.
func TestApplyThinking_UnknownProviderPassthrough(t *testing.T) {
	body := []byte(`{"reasoning_effort":"high"}`)
	out, err := thinking.ApplyThinking(body, "some-model", "openai", "definitely-not-a-provider", "definitely-not-a-provider")
	if err != nil {
		t.Fatalf("unknown provider must passthrough without error, got: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("unknown provider must return body unchanged: got %s", out)
	}
}

// TestApplyThinking_InvalidSuffixTreatedAsNoConfig verifies that a malformed or
// unknown suffix does not corrupt the request: it is treated as no config and,
// with an otherwise-empty body, passes through unchanged.
func TestApplyThinking_InvalidSuffixTreatedAsNoConfig(t *testing.T) {
	registerModels(t, "test-pipe-badsuffix", "openai", registry.GetClaudeModels())

	body := []byte(`{"messages":[]}`)
	// "ultra" is not a valid level, numeric, or special value.
	out, err := thinking.ApplyThinking(body, "gpt-5.2(ultra)", "openai", "openai", "openai")
	if err != nil {
		t.Fatalf("invalid suffix should not error, got: %v", err)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("invalid suffix must not inject reasoning config: %s", out)
	}
}

// TestApplyThinking_MalformedBodyReturnedUnchanged verifies defensive handling
// of non-JSON bodies: extraction yields no config and the body is returned as-is.
func TestApplyThinking_MalformedBodyReturnedUnchanged(t *testing.T) {
	registerModels(t, "test-pipe-badbody", "openai", registry.GetClaudeModels())

	body := []byte(`this is not json`)
	out, err := thinking.ApplyThinking(body, "gpt-5.2", "openai", "openai", "openai")
	if err != nil {
		t.Fatalf("malformed body should not error, got: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("malformed body must be returned unchanged: got %s", out)
	}
}

func TestApplyThinking_DeepSeekNativeThinkingObject(t *testing.T) {
	registerModels(t, "test-pipe-deepseek-native", "deepseek", []*registry.ModelInfo{
		{
			ID:   "deepseek-r1",
			Type: "deepseek",
			Thinking: &registry.ThinkingSupport{
				Levels:         []string{"low", "medium", "high", "max"},
				DynamicAllowed: true,
				ZeroAllowed:    true,
			},
		},
	})

	body := []byte(`{"thinking":{"type":"enabled","effort":"max"},"reasoning_effort":"low"}`)
	out, err := thinking.ApplyThinking(body, "deepseek-r1", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking error: %v", err)
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Fatalf("enabled DeepSeek output must remove native thinking object: %s", out)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "max" {
		t.Fatalf("reasoning_effort = %q, want max. output: %s", got, out)
	}
}

func TestApplyThinking_DeepSeekDisabledStripsEffort(t *testing.T) {
	registerModels(t, "test-pipe-deepseek-disabled", "deepseek", []*registry.ModelInfo{
		{
			ID:   "deepseek-r1",
			Type: "deepseek",
			Thinking: &registry.ThinkingSupport{
				Levels:      []string{"low", "medium", "high", "max"},
				ZeroAllowed: true,
			},
		},
	})

	body := []byte(`{"thinking":{"type":"disabled"},"reasoning_effort":"high"}`)
	out, err := thinking.ApplyThinking(body, "deepseek-r1", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking error: %v", err)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("disabled DeepSeek output must remove reasoning_effort: %s", out)
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled. output: %s", got, out)
	}
}

func TestApplyThinking_DeepSeekUnsupportedModelStripsThinkingConfig(t *testing.T) {
	registerModels(t, "test-pipe-deepseek-unsupported", "deepseek", []*registry.ModelInfo{
		{ID: "deepseek-no-think", Type: "deepseek"},
	})

	body := []byte(`{"thinking":{"type":"enabled","effort":"high"},"reasoning_effort":"low","messages":[]}`)
	out, err := thinking.ApplyThinking(body, "deepseek-no-think", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking error: %v", err)
	}
	if gjson.GetBytes(out, "thinking").Exists() || gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("unsupported DeepSeek model must strip thinking config: %s", out)
	}
	if !gjson.GetBytes(out, "messages").Exists() {
		t.Fatalf("non-thinking fields must be preserved: %s", out)
	}
}

// TestApplyThinking_ExtractReasoningEffort verifies the usage-logging helper
// derives a canonical reasoning_effort label consistently with ApplyThinking's
// suffix priority.
func TestApplyThinking_ExtractReasoningEffort(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		provider string
		model    string
		want     string
	}{
		{"suffix_level", `{}`, "openai", "gpt-5.2(high)", "high"},
		{"suffix_overrides_body", `{"reasoning_effort":"low"}`, "openai", "gpt-5.2(high)", "high"},
		{"body_level", `{"reasoning_effort":"medium"}`, "openai", "gpt-5.2", "medium"},
		{"none", `{"reasoning_effort":"none"}`, "openai", "gpt-5.2", "none"},
		{"no_config", `{}`, "openai", "gpt-5.2", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := thinking.ExtractReasoningEffort([]byte(tc.body), tc.provider, tc.model)
			if got != tc.want {
				t.Errorf("ExtractReasoningEffort = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestApplyThinking_DeepSeekEnabledWithoutEffortDefaultsToAuto verifies that
// thinking:{type:enabled} without explicit effort defaults to auto mode.
// Previously the native thinking object was leaked to DeepSeek, which silently
// dropped thinking.
func TestApplyThinking_DeepSeekEnabledWithoutEffortDefaultsToAuto(t *testing.T) {
	registerModels(t, "test-pipe-deepseek-auto", "deepseek", []*registry.ModelInfo{
		{
			ID:   "deepseek-r1",
			Type: "deepseek",
			Thinking: &registry.ThinkingSupport{
				Levels:         []string{"low", "medium", "high", "max"},
				DynamicAllowed: true,
				ZeroAllowed:    true,
			},
		},
	})

	body := []byte(`{"thinking":{"type":"enabled"}}`)
	out, err := thinking.ApplyThinking(body, "deepseek-r1", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking error: %v", err)
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Fatalf("enabled DeepSeek output must remove native thinking object: %s", out)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "auto" {
		t.Fatalf("reasoning_effort = %q, want auto. output: %s", got, out)
	}
}

// TestApplyThinking_DeepSeekReasoningEffortOnly verifies that a body with only
// reasoning_effort (no native thinking object) is handled correctly on the
// enabled path. The applier must strip the thinking key safely and set the
// reasoning_effort field.
func TestApplyThinking_DeepSeekReasoningEffortOnly(t *testing.T) {
	registerModels(t, "test-pipe-deepseek-effortonly", "deepseek", []*registry.ModelInfo{
		{
			ID:   "deepseek-r1",
			Type: "deepseek",
			Thinking: &registry.ThinkingSupport{
				Levels:         []string{"low", "medium", "high", "max"},
				DynamicAllowed: true,
			},
		},
	})

	body := []byte(`{"reasoning_effort":"high","messages":[]}`)
	out, err := thinking.ApplyThinking(body, "deepseek-r1", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking error: %v", err)
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Fatalf("output must not contain thinking object: %s", out)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want high. output: %s", got, out)
	}
	if !gjson.GetBytes(out, "messages").Exists() {
		t.Fatalf("non-thinking fields must be preserved: %s", out)
	}
}
