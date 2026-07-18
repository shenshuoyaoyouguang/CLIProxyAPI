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
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/deepseek"
	"github.com/tidwall/gjson"
)

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
