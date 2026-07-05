package mimo

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// newMimoModelInfo builds a non-user-defined ModelInfo with thinking support
// enabled, suitable for exercising the main Apply path.
func newMimoModelInfo() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:          "mimo-v2",
		Type:        "mimo",
		UserDefined: false,
		Thinking: &registry.ThinkingSupport{
			Min:    0,
			Max:    64512,
			Levels: []string{"low", "medium", "high"},
		},
	}
}

// newMimoUserDefinedModelInfo builds a user-defined ModelInfo to exercise the
// applyCompatibleMimo code path.
func newMimoUserDefinedModelInfo() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:          "mimo-user",
		Type:        "mimo",
		UserDefined: true,
		Thinking: &registry.ThinkingSupport{
			Min:    0,
			Max:    64512,
			Levels: []string{"low", "medium", "high"},
		},
	}
}

func TestMimoApply_ModeLevel_High(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}

	result, err := applier.Apply(body, config, newMimoModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if got := gjson.GetBytes(result, "thinking.type").String(); got != "enabled" {
		t.Fatalf("thinking.type = %q, want %q", got, "enabled")
	}
}

func TestMimoApply_ModeLevel_None(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelNone}

	result, err := applier.Apply(body, config, newMimoModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if got := gjson.GetBytes(result, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want %q", got, "disabled")
	}
}

func TestMimoApply_ModeNone_Disabled(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeNone}

	result, err := applier.Apply(body, config, newMimoModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if got := gjson.GetBytes(result, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want %q", got, "disabled")
	}
}

func TestMimoApply_ModeBudget(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 24576}

	result, err := applier.Apply(body, config, newMimoModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if got := gjson.GetBytes(result, "thinking.type").String(); got != "enabled" {
		t.Fatalf("thinking.type = %q, want %q", got, "enabled")
	}
	// max_completion_tokens must be boosted to at least the budget value.
	if got := gjson.GetBytes(result, "max_completion_tokens").Int(); got < 24576 {
		t.Fatalf("max_completion_tokens = %d, want >= %d", got, 24576)
	}
}

func TestMimoApply_ModeBudget_Zero(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 0}

	result, err := applier.Apply(body, config, newMimoModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if got := gjson.GetBytes(result, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want %q (Budget=0 → disabled)", got, "disabled")
	}
}

func TestMimoApply_ModeAuto(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeAuto}

	result, err := applier.Apply(body, config, newMimoModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// ModeAuto must return the original body unchanged — no thinking.type field added.
	if v := gjson.GetBytes(result, "thinking.type"); v.Exists() {
		t.Fatalf("expected no thinking.type for ModeAuto, got %s", v.Raw)
	}
	if string(result) != `{}` {
		t.Fatalf("ModeAuto should return original body unchanged\n got: %s\nwant: {}", string(result))
	}
}

func TestMimoApply_ModeBudget_BoostMaxCompletion(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"thinking":{"type":"enabled"},"max_completion_tokens":1000}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}

	result, err := applier.Apply(body, config, newMimoModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// mimoBoostMaxCompletion must raise max_completion_tokens to the budget.
	if got := gjson.GetBytes(result, "max_completion_tokens").Int(); got != 8192 {
		t.Fatalf("max_completion_tokens = %d, want %d (boost to budget)", got, 8192)
	}
	if got := gjson.GetBytes(result, "thinking.type").String(); got != "enabled" {
		t.Fatalf("thinking.type = %q, want %q", got, "enabled")
	}
}

func TestMimoApply_ModeBudget_BoostNoDowngrade(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"thinking":{"type":"enabled"},"max_completion_tokens":10000}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}

	result, err := applier.Apply(body, config, newMimoModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// mimoBoostMaxCompletion must NOT downgrade an existing larger max_completion_tokens.
	if got := gjson.GetBytes(result, "max_completion_tokens").Int(); got != 10000 {
		t.Fatalf("max_completion_tokens = %d, want %d (no downgrade)", got, 10000)
	}
}

func TestMimoApply_UserDefined(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}

	result, err := applier.Apply(body, config, newMimoUserDefinedModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// applyCompatibleMimo should set thinking.type=enabled for LevelHigh.
	if got := gjson.GetBytes(result, "thinking.type").String(); got != "enabled" {
		t.Fatalf("user-defined: thinking.type = %q, want %q", got, "enabled")
	}
}

func TestMimoApply_UserDefined_ModeNone(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeNone}

	result, err := applier.Apply(body, config, newMimoUserDefinedModelInfo())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// applyCompatibleMimo should set thinking.type=disabled for ModeNone.
	if got := gjson.GetBytes(result, "thinking.type").String(); got != "disabled" {
		t.Fatalf("user-defined ModeNone: thinking.type = %q, want %q", got, "disabled")
	}
}

func TestMimoApply_ThinkingNil_Passthrough(t *testing.T) {
	applier := NewApplier()
	original := []byte(`{"model":"mimo-v2","messages":[]}`)
	config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}

	modelInfo := &registry.ModelInfo{
		ID:          "mimo-v2",
		Type:        "mimo",
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
