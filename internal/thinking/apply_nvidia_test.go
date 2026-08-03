package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"

	// Register the nvidia provider applier exactly like the production
	// executor path does (helps/thinking_providers.go); without this the
	// ApplyThinking end-to-end tests silently passthrough.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/nvidia"
)

// TestApplyThinking_NvidiaEnabledOnly_KeepsFullThinking verifies that a bare
// enable_thinking=true on a registered NVIDIA model is not rewritten into
// medium_effort=true (reduced depth). The NVIDIA-native full-thinking shape
// (enable_thinking=true alone) must survive intact, matching the DeepSeek
// enabled-only passthrough semantics.
func TestApplyThinking_NvidiaEnabledOnly_KeepsFullThinking(t *testing.T) {
	body := []byte(`{"model":"nemotron-3-ultra","enable_thinking":true,"messages":[{"role":"user","content":"hi"}]}`)
	result, err := thinking.ApplyThinking(body, "nemotron-3-ultra", "openai", "nvidia", "nvidia")
	if err != nil {
		t.Fatalf("ApplyThinking failed: %v", err)
	}
	if got := gjson.GetBytes(result, "enable_thinking").Bool(); !got {
		t.Errorf("enable_thinking should remain true, got false in %s", result)
	}
	if me := gjson.GetBytes(result, "medium_effort"); me.Exists() {
		t.Errorf("bare enable_thinking=true must not be downgraded with medium_effort=true, got %s", result)
	}
}

// TestApplyThinking_NvidiaBudgetClampedToLow_ClearsBudget verifies that when a
// reasoning_budget request is clamped to the model's nearest supported level
// (low), the original reasoning_budget field is removed so the upstream never
// receives the mutually exclusive reasoning_budget + medium_effort pair.
func TestApplyThinking_NvidiaBudgetClampedToLow_ClearsBudget(t *testing.T) {
	body := []byte(`{"model":"nemotron-3-ultra","enable_thinking":true,"reasoning_budget":4096,"messages":[{"role":"user","content":"hi"}]}`)
	result, err := thinking.ApplyThinking(body, "nemotron-3-ultra", "openai", "nvidia", "nvidia")
	if err != nil {
		t.Fatalf("ApplyThinking failed: %v", err)
	}
	if rb := gjson.GetBytes(result, "reasoning_budget"); rb.Exists() {
		t.Errorf("clamped level must clear reasoning_budget (mutually exclusive with medium_effort), got %s", result)
	}
	if got := gjson.GetBytes(result, "medium_effort").Bool(); !got {
		t.Errorf("clamped low level should set medium_effort=true, got %s", result)
	}
}
