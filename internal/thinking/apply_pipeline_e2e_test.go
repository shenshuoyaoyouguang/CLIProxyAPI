// Package thinking_test contains end-to-end pipeline tests for ApplyThinking
// that exercise registered provider appliers. These live in an external test
// package so they can blank-import provider packages, whose init() calls
// RegisterProvider, without creating an import cycle.
package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	// Blank-import provider packages so their init() registers appliers with
	// the thinking registry. Without these, GetProviderApplier returns nil and
	// ApplyThinking would passthrough unchanged.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/codex"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/deepseek"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/gemini"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/openai"
	"github.com/tidwall/gjson"
)

// TestApplyThinkingE2E_NormalizesMixedCaseLevelEffort verifies that mixed-case
// and whitespace-padded effort/level strings are normalized to lowercase trimmed
// wire values for user-defined models. Providers that require canonical enums
// (OpenAI reasoning_effort, Gemini thinkingLevel) must not receive "HIGH"/" high ".
func TestApplyThinkingE2E_NormalizesMixedCaseLevelEffort(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		from     string
		to       string
		wantPath string
		want     string
	}{
		{
			name:     "openai_reasoning_effort_upper",
			body:     `{"reasoning_effort":"HIGH"}`,
			from:     "openai",
			to:       "openai",
			wantPath: "reasoning_effort",
			want:     "high",
		},
		{
			name:     "openai_reasoning_effort_padded",
			body:     `{"reasoning_effort":" High "}`,
			from:     "openai",
			to:       "openai",
			wantPath: "reasoning_effort",
			want:     "high",
		},
		{
			// User-defined Gemini is budget-capable, so ModeLevel is converted to
			// ModeBudget via ConvertLevelToBudget before apply. HIGH → 24576.
			name:     "gemini_thinking_level_upper_to_budget",
			body:     `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"HIGH"}}}`,
			from:     "gemini",
			to:       "gemini",
			wantPath: "generationConfig.thinkingConfig.thinkingBudget",
			want:     "24576",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := thinking.ApplyThinking([]byte(tc.body), "custom-model", tc.from, tc.to, tc.to)
			if err != nil {
				t.Fatalf("ApplyThinking returned error: %v", err)
			}
			if got := gjson.GetBytes(out, tc.wantPath).String(); got != tc.want {
				t.Fatalf("%s = %q, want %q; payload: %s", tc.wantPath, got, tc.want, out)
			}
		})
	}
}

// TestApplyThinkingE2E_OpenAIFamilyNoneCaseInsensitive verifies that mixed-case
// "NONE" on reasoning_effort / reasoning.effort is treated as ModeNone for
// user-defined OpenAI and Codex models (emits effort "none").
func TestApplyThinkingE2E_OpenAIFamilyNoneCaseInsensitive(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		from     string
		to       string
		wantPath string
	}{
		{
			name:     "openai_reasoning_effort_NONE",
			body:     `{"reasoning_effort":"NONE"}`,
			from:     "openai",
			to:       "openai",
			wantPath: "reasoning_effort",
		},
		{
			name:     "codex_reasoning_effort_None",
			body:     `{"reasoning":{"effort":"None"}}`,
			from:     "codex",
			to:       "codex",
			wantPath: "reasoning.effort",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := thinking.ApplyThinking([]byte(tc.body), "custom-model", tc.from, tc.to, tc.to)
			if err != nil {
				t.Fatalf("ApplyThinking returned error: %v", err)
			}
			if got := gjson.GetBytes(out, tc.wantPath).String(); got != "none" {
				t.Fatalf("%s = %q, want %q; payload: %s", tc.wantPath, got, "none", out)
			}
		})
	}
}

// TestApplyThinkingE2E_ClaudeDisabledTypeCaseInsensitive verifies that
// thinking.type="Disabled" is accepted as ModeNone and written as lowercase
// "disabled" for user-defined Claude models.
func TestApplyThinkingE2E_ClaudeDisabledTypeCaseInsensitive(t *testing.T) {
	body := []byte(`{"thinking":{"type":"Disabled","budget_tokens":8192},"max_tokens":1024}`)
	out, err := thinking.ApplyThinking(body, "custom-claude", "claude", "claude", "claude")
	if err != nil {
		t.Fatalf("ApplyThinking returned error: %v", err)
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want %q; payload: %s", got, "disabled", out)
	}
	if gjson.GetBytes(out, "thinking.budget_tokens").Exists() {
		t.Fatalf("budget_tokens must be cleared on disable; payload: %s", out)
	}
}

// TestApplyThinkingE2E_DeepSeekLegacyAutoClearsFields verifies that OpenAI-
// compatible reasoning_effort="auto" is treated as ModeAuto for DeepSeek and
// therefore clears both the thinking object and reasoning_effort (provider
// default), matching thinking.type=enabled + thinking.effort=auto.
func TestApplyThinkingE2E_DeepSeekLegacyAutoClearsFields(t *testing.T) {
	// Legacy-only body: no native thinking object, so extractDeepSeekConfig must
	// map reasoning_effort=auto to ModeAuto (not ModeLevel "auto").
	body := []byte(`{"reasoning_effort":"auto","messages":[]}`)
	out, err := thinking.ApplyThinking(body, "custom-dsk-model", "deepseek", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking returned error: %v", err)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort must be absent for auto/default; payload: %s", out)
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Fatalf("thinking object must be stripped for auto/default; payload: %s", out)
	}
}

// TestApplyThinkingE2E_SuffixOverridesBody verifies that a thinking suffix in
// the model name takes priority over thinking fields in the request body. This
// is a cross-cutting contract of ApplyThinking: suffix → model lookup → extract
// → validate → apply. For a user-defined DeepSeek model, the suffix "high" must
// override the body's reasoning_effort=low.
func TestApplyThinkingE2E_SuffixOverridesBody(t *testing.T) {
	body := []byte(`{"reasoning_effort":"low"}`)
	out, err := thinking.ApplyThinking(body, "custom-dsk-model(high)", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking returned error: %v", err)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want %q (suffix must override body); payload: %s", got, "high", out)
	}
}

// TestApplyThinkingE2E_UserDefinedNumericSuffixWritesWireBudget verifies that a
// numeric suffix on a user-defined model is applied to the target provider body
// (not merely parsed). Gemini is budget-capable so (8192) must land as
// thinkingBudget=8192.
func TestApplyThinkingE2E_UserDefinedNumericSuffixWritesWireBudget(t *testing.T) {
	out, err := thinking.ApplyThinking([]byte(`{}`), "custom-model(8192)", "openai", "gemini", "gemini")
	if err != nil {
		t.Fatalf("ApplyThinking returned error: %v", err)
	}
	if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingBudget").Int(); got != 8192 {
		t.Fatalf("thinkingBudget = %d, want 8192; payload: %s", got, out)
	}
}

// TestApplyThinkingE2E_DeepSeekNativeThinkingObject verifies that DeepSeek's
// native thinking object (thinking.type=enabled with thinking.effort) is
// extracted and translated to reasoning_effort. This exercises the full
// pipeline: extract → apply, confirming the native shape is recognized for
// user-defined models.
func TestApplyThinkingE2E_DeepSeekNativeThinkingObject(t *testing.T) {
	body := []byte(`{"thinking":{"type":"enabled","effort":"high"},"messages":[]}`)
	out, err := thinking.ApplyThinking(body, "custom-dsk-model", "deepseek", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking returned error: %v", err)
	}
	// Native thinking object must be collapsed to reasoning_effort=high and
	// the legacy thinking object stripped by the DeepSeek applier.
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want %q; payload: %s", got, "high", out)
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Fatalf("native thinking object must be stripped; payload: %s", out)
	}
}

// TestApplyThinkingE2E_UnknownProviderPassthrough is the E2E counterpart of the
// internal TestApplyThinking_UnknownProviderPassthrough. It verifies that an
// unknown provider returns the body unchanged even when provider appliers are
// registered for other providers.
func TestApplyThinkingE2E_UnknownProviderPassthrough(t *testing.T) {
	body := []byte(`{"reasoning_effort":"low","messages":[]}`)
	out, err := thinking.ApplyThinking(body, "custom-model", "openai", "totally-unknown-provider", "totally-unknown-provider")
	if err != nil {
		t.Fatalf("ApplyThinking returned error for unknown provider: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("unknown provider must passthrough unchanged; got %s, want %s", out, body)
	}
}

// TestApplyThinkingE2E_MalformedBodyReturnedUnchanged is the E2E counterpart of
// the internal TestApplyThinking_MalformedBodyReturnedUnchanged. It verifies
// that a malformed JSON body without a suffix is returned unchanged even when
// provider appliers are registered.
func TestApplyThinkingE2E_MalformedBodyReturnedUnchanged(t *testing.T) {
	body := []byte(`{"invalid json`)
	out, err := thinking.ApplyThinking(body, "custom-model", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking returned error on malformed body: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("malformed body must be returned unchanged; got %s, want %s", out, body)
	}
}

// TestApplyThinkingE2E_InvalidSuffixTreatedAsNoConfig is the E2E counterpart of
// the internal TestApplyThinking_InvalidSuffixTreatedAsNoConfig. It verifies
// that an invalid suffix content yields no config so the body is returned
// unchanged, even when a DeepSeek applier is registered.
func TestApplyThinkingE2E_InvalidSuffixTreatedAsNoConfig(t *testing.T) {
	body := []byte(`{"reasoning_effort":"low"}`)
	// "ultra" is not a known level, not numeric, and not a special value.
	out, err := thinking.ApplyThinking(body, "custom-dsk-model(ultra)", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking returned error on invalid suffix: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("invalid suffix must yield passthrough; got %s, want %s", out, body)
	}
}
