package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
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

// --- Regression tests for the Thinking == nil passthrough fix ---
// When the registry has no thinking block for a known model but the request
// carries explicit thinking intent, ApplyThinking must NOT strip the config.
// It should pass through to the upstream provider for裁决 (mirrors the
// user-defined model behavior where the upstream is authoritative).

func TestApplyThinking_ThinkingNil_NvidiaEnableThinking_Passthrough(t *testing.T) {
	body := []byte(`{"model":"fake-nvidia","enable_thinking":true,"reasoning_budget":4096,"messages":[{"role":"user","content":"hi"}]}`)
	modelInfo := &registry.ModelInfo{ID: "fake-nvidia", Type: "nvidia"}
	result, err := thinking.ApplyThinkingWithModelInfoAndSummary(
		body, body, "fake-nvidia", "openai", "nvidia", "nvidia", modelInfo, thinking.SummaryConfig{},
	)
	if err != nil {
		t.Fatalf("ApplyThinking failed: %v", err)
	}
	// Must NOT be stripped: enable_thinking and reasoning_budget should survive.
	if !gjson.GetBytes(result, "enable_thinking").Bool() {
		t.Errorf("enable_thinking should survive passthrough, got stripped: %s", result)
	}
	if rb := gjson.GetBytes(result, "reasoning_budget"); !rb.Exists() || rb.Int() != 4096 {
		t.Errorf("reasoning_budget should survive passthrough, got: %s", result)
	}
}

func TestApplyThinking_ThinkingNil_OpenAIReasoningEffort_Passthrough(t *testing.T) {
	body := []byte(`{"model":"fake-nvidia","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`)
	modelInfo := &registry.ModelInfo{ID: "fake-nvidia", Type: "nvidia"}
	result, err := thinking.ApplyThinkingWithModelInfoAndSummary(
		body, body, "fake-nvidia", "openai", "nvidia", "nvidia", modelInfo, thinking.SummaryConfig{},
	)
	if err != nil {
		t.Fatalf("ApplyThinking failed: %v", err)
	}
	if got := gjson.GetBytes(result, "reasoning_effort").String(); got != "high" {
		t.Errorf("reasoning_effort=high should survive passthrough, got: %s", result)
	}
}

func TestApplyThinking_ThinkingNil_NoThinkingIntent_Passthrough(t *testing.T) {
	body := []byte(`{"model":"fake-nvidia","messages":[{"role":"user","content":"hi"}]}`)
	modelInfo := &registry.ModelInfo{ID: "fake-nvidia", Type: "nvidia"}
	result, err := thinking.ApplyThinkingWithModelInfoAndSummary(
		body, body, "fake-nvidia", "openai", "nvidia", "nvidia", modelInfo, thinking.SummaryConfig{},
	)
	if err != nil {
		t.Fatalf("ApplyThinking failed: %v", err)
	}
	// No thinking intent → body unchanged.
	if string(result) != string(body) {
		t.Errorf("body should be unchanged when no thinking intent, got: %s", result)
	}
}
