package mimo

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func newModelInfo() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID: "mimo-v2.5-pro",
		Thinking: &registry.ThinkingSupport{
			Min:         8192,
			Max:         64512,
			ZeroAllowed: true,
		},
	}
}

func TestApply_ModeLevel_SetsThinkingEnabled(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type").String()
	if thinkingType != "enabled" {
		t.Errorf("expected thinking.type=enabled, got %q", thinkingType)
	}
}

func TestApply_ModeNone_SetsThinkingDisabled(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeNone}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type").String()
	if thinkingType != "disabled" {
		t.Errorf("expected thinking.type=disabled, got %q", thinkingType)
	}
}

func TestApply_ModeBudget_NonZero_SetsThinkingEnabled(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type").String()
	if thinkingType != "enabled" {
		t.Errorf("expected thinking.type=enabled, got %q", thinkingType)
	}
}

func TestApply_ModeBudget_Zero_SetsThinkingDisabled(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 0}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type").String()
	if thinkingType != "disabled" {
		t.Errorf("expected thinking.type=disabled, got %q", thinkingType)
	}
}

func TestApply_ModeAuto_LeavesUnchanged(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeAuto}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type")
	if thinkingType.Exists() {
		t.Errorf("expected no thinking.type for auto mode, got %q", thinkingType.String())
	}
}

func TestApply_UserDefinedModel_AppliesConfig(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-custom","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}
	userModel := &registry.ModelInfo{
		ID:          "mimo-custom",
		UserDefined: true,
	}

	result, err := a.Apply(body, config, userModel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type").String()
	if thinkingType != "enabled" {
		t.Errorf("expected thinking.type=enabled for user-defined model, got %q", thinkingType)
	}
}

func TestApply_NilThinking_ReturnsUnchanged(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2-flash","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}
	modelNoThinking := &registry.ModelInfo{
		ID:       "mimo-v2-flash",
		Thinking: nil,
	}

	result, err := a.Apply(body, config, modelNoThinking)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type")
	if thinkingType.Exists() {
		t.Errorf("expected no thinking.type for model without thinking support, got %q", thinkingType.String())
	}
}

func TestApply_EmptyBody_CreatesJSON(t *testing.T) {
	a := NewApplier()
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}

	result, err := a.Apply([]byte(""), config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type").String()
	if thinkingType != "enabled" {
		t.Errorf("expected thinking.type=enabled, got %q", thinkingType)
	}
}

func TestApply_ExistingThinkingField_IsOverwritten(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","thinking":{"type":"disabled"},"messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelMedium}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinkingType := gjson.GetBytes(result, "thinking.type").String()
	if thinkingType != "enabled" {
		t.Errorf("expected thinking.type=enabled, got %q", thinkingType)
	}
}

func TestMimoBoostMaxCompletion_BudgetSetsMaxCompletionTokens(t *testing.T) {
	body := []byte(`{"model":"mimo-v2.5-pro","thinking":{"type":"enabled"},"messages":[]}`)
	out := mimoBoostMaxCompletion(body, 64512)

	got := gjson.GetBytes(out, "max_completion_tokens").Int()
	if got != 64512 {
		t.Errorf("max_completion_tokens = %d, want 64512", got)
	}
}

func TestMimoBoostMaxCompletion_UserHigherPreserved(t *testing.T) {
	body := []byte(`{"model":"mimo-v2.5-pro","thinking":{"type":"enabled"},"max_completion_tokens":128000,"messages":[]}`)
	out := mimoBoostMaxCompletion(body, 64512)

	got := gjson.GetBytes(out, "max_completion_tokens").Int()
	if got != 128000 {
		t.Errorf("max_completion_tokens = %d, want 128000 (user value preserved)", got)
	}
}

func TestMimoBoostMaxCompletion_UserLowerBoosted(t *testing.T) {
	body := []byte(`{"model":"mimo-v2.5-pro","thinking":{"type":"enabled"},"max_completion_tokens":8192,"messages":[]}`)
	out := mimoBoostMaxCompletion(body, 24576)

	got := gjson.GetBytes(out, "max_completion_tokens").Int()
	if got != 24576 {
		t.Errorf("max_completion_tokens = %d, want 24576 (boosted from 8192)", got)
	}
}

func TestMimoBoostMaxCompletion_DisabledThinkingNoBoost(t *testing.T) {
	body := []byte(`{"model":"mimo-v2.5-pro","thinking":{"type":"disabled"},"max_completion_tokens":4096,"messages":[]}`)
	out := mimoBoostMaxCompletion(body, 64512)

	got := gjson.GetBytes(out, "max_completion_tokens").Int()
	if got != 4096 {
		t.Errorf("max_completion_tokens = %d, want 4096 (unchanged, thinking disabled)", got)
	}
}

func TestMimoBoostMaxCompletion_ZeroBudgetNoOp(t *testing.T) {
	body := []byte(`{"model":"mimo-v2.5-pro","thinking":{"type":"enabled"},"messages":[]}`)
	out := mimoBoostMaxCompletion(body, 0)

	if gjson.GetBytes(out, "max_completion_tokens").Exists() {
		t.Errorf("max_completion_tokens should not be set when budget is 0")
	}
}

func TestApply_ModeBudget_NonZero_SetsMaxCompletionTokens(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 64512}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gjson.GetBytes(result, "thinking.type").String() != "enabled" {
		t.Errorf("expected thinking.type=enabled")
	}
	got := gjson.GetBytes(result, "max_completion_tokens").Int()
	if got != 64512 {
		t.Errorf("max_completion_tokens = %d, want 64512", got)
	}
}

func TestApply_ModeBudget_WithExistingMaxCompletionTokens_TakesMax(t *testing.T) {
	a := NewApplier()
	body, _ := sjson.SetBytes([]byte(`{"model":"mimo-v2.5-pro","messages":[]}`), "max_completion_tokens", 128000)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 24576}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := gjson.GetBytes(result, "max_completion_tokens").Int()
	if got != 128000 {
		t.Errorf("max_completion_tokens = %d, want 128000 (user value higher)", got)
	}
}

// --- Tests for reasoning_effort fallback behavior ---
// These test that the applier correctly handles budget values derived
// from reasoning_effort mapping (low→8192, medium→24576, high/max→64512).

func TestApply_ReasoningEffortLow_BudgetSetsEnabled(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	// Simulate reasoning_effort=low → ModeBudget/8192 after extractMIMOConfig
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gjson.GetBytes(result, "thinking.type").String() != "enabled" {
		t.Errorf("expected thinking.type=enabled for low effort")
	}
	if got := gjson.GetBytes(result, "max_completion_tokens").Int(); got != 8192 {
		t.Errorf("max_completion_tokens = %d, want 8192", got)
	}
}

func TestApply_ReasoningEffortMedium_BudgetSetsEnabled(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 24576}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gjson.GetBytes(result, "thinking.type").String() != "enabled" {
		t.Errorf("expected thinking.type=enabled for medium effort")
	}
	if got := gjson.GetBytes(result, "max_completion_tokens").Int(); got != 24576 {
		t.Errorf("max_completion_tokens = %d, want 24576", got)
	}
}

func TestApply_ReasoningEffortMax_BudgetSetsEnabled(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"model":"mimo-v2.5-pro","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 64512}

	result, err := a.Apply(body, config, newModelInfo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gjson.GetBytes(result, "thinking.type").String() != "enabled" {
		t.Errorf("expected thinking.type=enabled for max effort")
	}
	if got := gjson.GetBytes(result, "max_completion_tokens").Int(); got != 64512 {
		t.Errorf("max_completion_tokens = %d, want 64512", got)
	}
}
