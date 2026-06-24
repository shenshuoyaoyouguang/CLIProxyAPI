package mimo

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func newModelInfo() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID: "mimo-v2.5-pro",
		Thinking: &registry.ThinkingSupport{
			Min:         1024,
			Max:         16384,
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
