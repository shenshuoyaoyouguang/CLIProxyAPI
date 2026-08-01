package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// TestStripThinkingConfig_ZaiStripsReasoningEffort verifies that the zai
// provider is handled by StripThinkingConfig. The zai applier is
// OpenAI-compatible (provider/zai.Applier embeds openai.Applier), so a model
// that does not support thinking must have reasoning_effort removed instead of
// forwarding it verbatim to the upstream.
func TestStripThinkingConfig_ZaiStripsReasoningEffort(t *testing.T) {
	body := []byte(`{"model":"glm-5.2","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`)
	got := thinking.StripThinkingConfig(body, "zai")
	if gjson.GetBytes(got, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort must be stripped for zai, got %s", got)
	}
	if gjson.GetBytes(got, "model").String() != "glm-5.2" {
		t.Fatalf("unrelated fields must be preserved, got %s", got)
	}
}

// TestStripThinkingConfig_OpenAIStillStripped guards the shared switch case so
// adding zai does not regress the original openai behaviour.
func TestStripThinkingConfig_OpenAIStillStripped(t *testing.T) {
	body := []byte(`{"model":"gpt-5","reasoning_effort":"low"}`)
	got := thinking.StripThinkingConfig(body, "openai")
	if gjson.GetBytes(got, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort must be stripped for openai, got %s", got)
	}
}
