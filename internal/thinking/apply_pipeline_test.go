// Package thinking pipeline tests.
//
// These tests pin end-to-end behavior of the unified thinking pipeline for
// user-defined (unknown) models, where validation is skipped and the upstream
// service is responsible for validating the configuration.
package thinking

import (
	"testing"
)

// TestApplyThinking_UserDefinedSuffixYieldsBudget verifies that a user-defined
// model with a numeric thinking suffix (e.g. "model(8192)") produces a ModeBudget
// config regardless of the fromFormat/toFormat values. The suffix parse direction
// (fromFormat) must not affect the parsing result because parseSuffixToConfig is
// provider-agnostic.
func TestApplyThinking_UserDefinedSuffixYieldsBudget(t *testing.T) {
	cases := []struct {
		name       string
		fromFormat string
		toFormat   string
	}{
		{name: "same_format", fromFormat: "gemini", toFormat: "gemini"},
		{name: "cross_family", fromFormat: "openai", toFormat: "gemini"},
		{name: "claude_to_gemini", fromFormat: "claude", toFormat: "gemini"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Unknown model => treated as user-defined, validation skipped.
			model := "custom-model(8192)"
			_, err := ApplyThinking([]byte(`{}`), model, tc.fromFormat, tc.toFormat, tc.toFormat)
			// We only care that no error occurs and the suffix was parsed as a budget.
			// Apply returns the original body on validation passthrough, so we cannot
			// assert on the payload directly; instead assert the parse helper directly.
			if err != nil {
				t.Fatalf("ApplyThinking returned error: %v", err)
			}
			cfg := parseSuffixToConfig("8192", tc.fromFormat, model)
			if cfg.Mode != ModeBudget {
				t.Fatalf("suffix parse mode = %v, want ModeBudget", cfg.Mode)
			}
			if cfg.Budget != 8192 {
				t.Fatalf("suffix parse budget = %d, want 8192", cfg.Budget)
			}
		})
	}
}

// TestApplyThinking_UserDefinedSuffixLevelMode verifies that a level suffix on a
// user-defined model parses to ModeLevel independent of fromFormat/toFormat.
func TestApplyThinking_UserDefinedSuffixLevelMode(t *testing.T) {
	cfg := parseSuffixToConfig("high", "openai", "custom-model(high)")
	if cfg.Mode != ModeLevel || cfg.Level != LevelHigh {
		t.Fatalf("suffix parse = %+v, want ModeLevel/LevelHigh", cfg)
	}
}

// TestApplyThinking_UnknownProviderPassthrough verifies that an unknown
// provider returns the original body unchanged with no error. This pins the
// route-check stage of the pipeline (FR25 step 1) and does not require any
// provider applier to be registered, so it can live in the internal test file.
func TestApplyThinking_UnknownProviderPassthrough(t *testing.T) {
	body := []byte(`{"reasoning_effort":"low","messages":[]}`)
	out, err := ApplyThinking(body, "custom-model", "openai", "totally-unknown-provider", "totally-unknown-provider")
	if err != nil {
		t.Fatalf("ApplyThinking returned error for unknown provider: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("unknown provider must passthrough unchanged; got %s, want %s", out, body)
	}
}

// TestApplyThinking_MalformedBodyReturnedUnchanged verifies that a malformed
// JSON body without a thinking suffix is returned unchanged. extractThinkingConfig
// guards on gjson.ValidBytes, so config extraction yields no config and the
// pipeline falls through to passthrough. This pins the defensive contract that
// ApplyThinking never panics on bad input and returns the body verbatim.
func TestApplyThinking_MalformedBodyReturnedUnchanged(t *testing.T) {
	body := []byte(`{"invalid json`)
	out, err := ApplyThinking(body, "custom-model", "openai", "deepseek", "deepseek")
	if err != nil {
		t.Fatalf("ApplyThinking returned error on malformed body: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("malformed body must be returned unchanged; got %s, want %s", out, body)
	}
}

// TestApplyThinking_InvalidSuffixTreatedAsNoConfig verifies that a suffix
// whose content is neither a special value, a known level, nor a numeric
// budget is treated as no config. The test uses an unknown provider so no
// applier is required and the body is returned unchanged at the route check.
func TestApplyThinking_InvalidSuffixTreatedAsNoConfig(t *testing.T) {
	body := []byte(`{"reasoning_effort":"low"}`)
	// "ultra" is not a known level, not numeric, and not a special value.
	out, err := ApplyThinking(body, "custom-model(ultra)", "openai", "totally-unknown-provider", "totally-unknown-provider")
	if err != nil {
		t.Fatalf("ApplyThinking returned error on invalid suffix: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("invalid suffix must yield passthrough; got %s, want %s", out, body)
	}
}
