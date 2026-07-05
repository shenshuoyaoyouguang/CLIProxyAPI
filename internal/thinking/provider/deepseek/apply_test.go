package deepseek

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// newDeepSeekModelInfo builds a non-user-defined ModelInfo with thinking support
// enabled, suitable for exercising the main Apply path.
func newDeepSeekModelInfo() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:         "deepseek-chat",
		Type:       "deepseek",
		UserDefined: false,
		Thinking: &registry.ThinkingSupport{
			Min:  0,
			Max:  64512,
			Levels: []string{"low", "medium", "high", "max"},
		},
	}
}

// newUserDefinedModelInfo builds a user-defined ModelInfo to exercise the
// applyCompatibleDeepSeek code path.
func newUserDefinedModelInfo() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:          "deepseek-user",
		Type:        "deepseek",
		UserDefined: true,
		Thinking: &registry.ThinkingSupport{
			Min:  0,
			Max:  64512,
			Levels: []string{"low", "medium", "high", "max"},
		},
	}
}

func TestDeepSeekApply_ModeLevel_High(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}

	result, err := applier.Apply(body, config, newDeepSeekModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// thinking object must be removed.
	if v := gjson.GetBytes(result, "thinking"); v.Exists() {
		t.Fatalf("expected thinking object to be removed, got %s", v.Raw)
	}
	// reasoning_effort must be set to "high".
	if got := gjson.GetBytes(result, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want %q", got, "high")
	}
}

func TestDeepSeekApply_ModeLevel_XHigh(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelXHigh}

	result, err := applier.Apply(body, config, newDeepSeekModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// xhigh must be normalized to "max" because DeepSeek does not accept xhigh.
	if got := gjson.GetBytes(result, "reasoning_effort").String(); got != "max" {
		t.Fatalf("reasoning_effort = %q, want %q (xhigh→max normalization)", got, "max")
	}
}

func TestDeepSeekApply_ModeNone_Disabled(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"thinking":{"type":"enabled"},"reasoning_effort":"high"}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeNone}

	result, err := applier.Apply(body, config, newDeepSeekModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// thinking.type must be set to "disabled".
	if got := gjson.GetBytes(result, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want %q", got, "disabled")
	}
	// reasoning_effort must be removed.
	if v := gjson.GetBytes(result, "reasoning_effort"); v.Exists() {
		t.Fatalf("expected reasoning_effort to be removed, got %s", v.Raw)
	}
}

func TestDeepSeekApply_ModeBudget(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	// Budget=24576 maps to LevelHigh via ConvertBudgetToLevel.
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 24576}

	result, err := applier.Apply(body, config, newDeepSeekModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// ConvertBudgetToLevel(24576) → "high", so reasoning_effort must be "high".
	if got := gjson.GetBytes(result, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want %q (budget 24576 → high)", got, "high")
	}
	if v := gjson.GetBytes(result, "thinking"); v.Exists() {
		t.Fatalf("expected thinking object to be removed, got %s", v.Raw)
	}
}

func TestDeepSeekApply_ModeAuto(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeAuto}

	result, err := applier.Apply(body, config, newDeepSeekModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if got := gjson.GetBytes(result, "reasoning_effort").String(); got != "auto" {
		t.Fatalf("reasoning_effort = %q, want %q", got, "auto")
	}
}

func TestDeepSeekApply_UserDefined(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}

	result, err := applier.Apply(body, config, newUserDefinedModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// applyCompatibleDeepSeek should still set reasoning_effort for LevelHigh.
	if got := gjson.GetBytes(result, "reasoning_effort").String(); got != "high" {
		t.Fatalf("user-defined: reasoning_effort = %q, want %q", got, "high")
	}
}

func TestDeepSeekApply_UserDefined_XHigh(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelXHigh}

	result, err := applier.Apply(body, config, newUserDefinedModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// applyCompatibleDeepSeek should normalize xhigh → max.
	if got := gjson.GetBytes(result, "reasoning_effort").String(); got != "max" {
		t.Fatalf("user-defined: reasoning_effort = %q, want %q (xhigh→max)", got, "max")
	}
}

func TestDeepSeekApply_ThinkingNil_Passthrough(t *testing.T) {
	applier := NewApplier()
	original := []byte(`{"model":"deepseek-chat","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}

	modelInfo := &registry.ModelInfo{
		ID:          "deepseek-chat",
		Type:        "deepseek",
		UserDefined: false,
		Thinking:    nil,
	}

	result, err := applier.Apply(original, config, modelInfo)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// Body must be returned unchanged when Thinking is nil.
	if string(result) != string(original) {
		t.Fatalf("expected passthrough when Thinking == nil\n got: %s\nwant: %s", string(result), string(original))
	}
}
