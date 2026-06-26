package kimi

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func TestApply_ModeNone_UsesDisabledThinking(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2",
		Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
	}
	body := []byte(`{"model":"kimi-k2","reasoning_effort":"high","thinking":{"type":"enabled"}}`)

	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeNone}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want %q, body=%s", got, "disabled", string(out))
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort should be removed in ModeNone, body=%s", string(out))
	}
}

func TestApply_ModeLevel_UsesReasoningEffort(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2",
		Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
	}
	body := []byte(`{"model":"kimi-k2","thinking":{"type":"disabled"}}`)

	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want %q, body=%s", got, "high", string(out))
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Fatalf("thinking should be removed when reasoning_effort is used, body=%s", string(out))
	}
}

func TestApply_UserDefinedModeNone_UsesDisabledThinking(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:          "custom-kimi-model",
		UserDefined: true,
	}
	body := []byte(`{"model":"custom-kimi-model","reasoning_effort":"high"}`)

	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeNone}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want %q, body=%s", got, "disabled", string(out))
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort should be removed in ModeNone, body=%s", string(out))
	}
}

func TestApply_ModeBudget_ConvertsToLevel(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2",
		Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
	}

	tests := []struct {
		name       string
		budget     int
		wantEffort string
	}{
		{name: "budget_1024_maps_to_minimal", budget: 1024, wantEffort: "minimal"},
		{name: "budget_1500_maps_to_low", budget: 1500, wantEffort: "low"},
		{name: "budget_8192_maps_to_medium", budget: 8192, wantEffort: "medium"},
		{name: "budget_20000_maps_to_high", budget: 20000, wantEffort: "high"},
		{name: "budget_0_maps_to_none", budget: 0, wantEffort: "none"},
		{name: "budget_minus1_maps_to_auto", budget: -1, wantEffort: "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"model":"kimi-k2","thinking":{"type":"enabled"}}`)
			out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: tt.budget}, modelInfo)
			if errApply != nil {
				t.Fatalf("Apply() error = %v", errApply)
			}
			if got := gjson.GetBytes(out, "reasoning_effort").String(); got != tt.wantEffort {
				t.Fatalf("reasoning_effort = %q, want %q, body=%s", got, tt.wantEffort, string(out))
			}
		})
	}
}

func TestApply_ModeBudget_InvalidReturnsBody(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2",
		Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
	}
	body := []byte(`{"model":"kimi-k2","thinking":{"type":"enabled"}}`)

	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: -5}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort should be absent for invalid budget, body=%s", string(out))
	}
}

func TestApply_ModeAuto_SetsAuto(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2",
		Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
	}
	body := []byte(`{"model":"kimi-k2","thinking":{"type":"enabled"}}`)

	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeAuto}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "auto" {
		t.Fatalf("reasoning_effort = %q, want %q, body=%s", got, "auto", string(out))
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Fatalf("thinking should be removed in ModeAuto, body=%s", string(out))
	}
}

func TestApply_UserDefinedModeBudget_ConvertsToLevel(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:          "custom-kimi-model",
		UserDefined: true,
	}
	body := []byte(`{"model":"custom-kimi-model","thinking":{"type":"enabled"}}`)

	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "medium" {
		t.Fatalf("reasoning_effort = %q, want %q, body=%s", got, "medium", string(out))
	}
}

func TestApply_UserDefinedModeAuto_SetsAuto(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:          "custom-kimi-model",
		UserDefined: true,
	}
	body := []byte(`{"model":"custom-kimi-model","reasoning_effort":"high"}`)

	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeAuto}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "auto" {
		t.Fatalf("reasoning_effort = %q, want %q, body=%s", got, "auto", string(out))
	}
}

// TestApply_ModeBudget_LargeBudget_CurrentBehavior documents current behavior:
// budget > ThresholdHigh (32768) maps to "xhigh" which is passed through unchanged.
// If Kimi later supports "max" level, this test should be updated to use MapXHighToMax.
func TestApply_ModeBudget_LargeBudget_CurrentBehavior(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2",
		Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
	}
	body := []byte(`{"model":"kimi-k2"}`)

	// Budget 50000 > ThresholdHigh (32768), ConvertBudgetToLevel returns "xhigh"
	// Currently Kimi passes "xhigh" through unchanged
	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 50000}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	// Current behavior: xhigh is passed through
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "xhigh" {
		t.Fatalf("reasoning_effort = %q, want %q (current behavior: xhigh passthrough), body=%s", got, "xhigh", string(out))
	}
}

// TestApply_ModeLevel_XHigh_CurrentBehavior documents current behavior:
// Level "xhigh" is passed through unchanged.
// If Kimi later supports "max" level, apply MapXHighToMax to convert xhigh → max.
func TestApply_ModeLevel_XHigh_CurrentBehavior(t *testing.T) {
	applier := NewApplier()
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2",
		Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high", "xhigh"}},
	}
	body := []byte(`{"model":"kimi-k2"}`)

	out, errApply := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelXHigh}, modelInfo)
	if errApply != nil {
		t.Fatalf("Apply() error = %v", errApply)
	}
	// Current behavior: xhigh is passed through
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "xhigh" {
		t.Fatalf("reasoning_effort = %q, want %q (current behavior: xhigh passthrough), body=%s", got, "xhigh", string(out))
	}
}
