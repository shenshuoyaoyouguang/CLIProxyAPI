// Package deepseek thinking applier translation tests.
//
// These tests pin DeepSeek's ThinkingConfig -> reasoning_effort/thinking.type
// translation, including budget->level conversion, xhigh->max mapping, and the
// explicit-disable path.
package deepseek

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// deepseekModel builds a DeepSeek model definition with discrete levels.
func deepseekModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "deepseek-r1",
		Type: "deepseek",
		Thinking: &registry.ThinkingSupport{
			Levels: []string{"low", "medium", "high", "max"},
		},
	}
}

func TestDeepSeekApply_TranslationMatrix(t *testing.T) {
	applier := NewApplier()

	cases := []struct {
		name       string
		body       string
		config     thinking.ThinkingConfig
		model      *registry.ModelInfo
		wantEffort string // "" means reasoning_effort must be absent
		wantType   string // "" means thinking.type must be absent
		wantAbsent []string
	}{
		{
			name:       "level_high",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
			model:      deepseekModel(),
			wantEffort: "high",
		},
		{
			name:       "level_xhigh_maps_to_max",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelXHigh},
			model:      deepseekModel(),
			wantEffort: "max",
		},
		{
			name:       "auto_maps_to_auto_effort",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeAuto, Budget: -1},
			model:      deepseekModel(),
			wantEffort: "auto",
		},
		{
			name:       "budget_converts_to_level_effort",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 24576}, // -> "high"
			model:      deepseekModel(),
			wantEffort: "high",
		},
		{
			name:       "none_produces_disabled_object",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			model:      deepseekModel(),
			wantType:   "disabled",
		},
		{
			name:       "none_with_clamped_level_respects_fallback",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeNone, Level: thinking.LevelLow},
			model:      deepseekModel(),
			wantEffort: "low",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := applier.Apply([]byte(tc.body), tc.config, tc.model)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			effort := gjson.GetBytes(out, "reasoning_effort")
			if tc.wantEffort == "" {
				if effort.Exists() {
					t.Errorf("expected reasoning_effort absent, got %q\npayload: %s", effort.String(), out)
				}
			} else if effort.String() != tc.wantEffort {
				t.Errorf("reasoning_effort = %q, want %q\npayload: %s", effort.String(), tc.wantEffort, out)
			}
			thinkingType := gjson.GetBytes(out, "thinking.type")
			if tc.wantType == "" {
				if thinkingType.Exists() {
					t.Errorf("expected thinking.type absent, got %q\npayload: %s", thinkingType.String(), out)
				}
			} else if thinkingType.String() != tc.wantType {
				t.Errorf("thinking.type = %q, want %q\npayload: %s", thinkingType.String(), tc.wantType, out)
			}
			for _, absent := range tc.wantAbsent {
				if gjson.GetBytes(out, absent).Exists() {
					t.Errorf("expected %s absent, payload: %s", absent, out)
				}
			}
		})
	}
}

// TestDeepSeekApply_StripsLegacyThinkingObject verifies the legacy flat/native
// thinking fields are removed from the final DeepSeek payload on the enabled path.
func TestDeepSeekApply_StripsLegacyThinkingObject(t *testing.T) {
	applier := NewApplier()

	out, err := applier.Apply([]byte(`{"reasoning_effort":"medium","thinking":{"type":"enabled","effort":"low"}}`),
		thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}, deepseekModel())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Errorf("legacy thinking object must be stripped, payload: %s", out)
	}
	if gjson.GetBytes(out, "reasoning_effort").String() != "high" {
		t.Errorf("expected reasoning_effort=high, payload: %s", out)
	}
}

// TestDeepSeekApply_DisabledStripsLegacyReasoningEffort verifies the legacy
// reasoning_effort field is removed when thinking is explicitly disabled.
func TestDeepSeekApply_DisabledStripsLegacyReasoningEffort(t *testing.T) {
	applier := NewApplier()

	out, err := applier.Apply([]byte(`{"reasoning_effort":"high"}`),
		thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0}, deepseekModel())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Errorf("legacy reasoning_effort must be stripped, payload: %s", out)
	}
	if gjson.GetBytes(out, "thinking.type").String() != "disabled" {
		t.Errorf("expected thinking.type=disabled, payload: %s", out)
	}
}

// TestDeepSeekApply_NilThinkingPassthrough verifies a model without thinking
// support passes the body through unchanged.
func TestDeepSeekApply_NilThinkingPassthrough(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"messages":[]}`)
	out, err := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
		&registry.ModelInfo{ID: "deepseek-no-think", Type: "deepseek"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("expected passthrough, got %s", out)
	}
}

// TestDeepSeekApply_UserDefinedBudgetConversion verifies user-defined DeepSeek
// models convert a numeric budget to the nearest reasoning_effort level.
func TestDeepSeekApply_UserDefinedBudgetConversion(t *testing.T) {
	applier := NewApplier()
	model := &registry.ModelInfo{ID: "custom-deepseek", Type: "deepseek", UserDefined: true}
	out, err := applier.Apply([]byte(`{}`), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 32768}, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 32768 maps to "xhigh" via ConvertBudgetToLevel, then normalized to "max" for DeepSeek.
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "max" {
		t.Fatalf("reasoning_effort = %q, want max", got)
	}
}

// TestDeepSeekApply_UserDefinedDisabledStripsReasoningEffort verifies
// user-defined models still emit the disabled object when thinking is off.
func TestDeepSeekApply_UserDefinedDisabledStripsReasoningEffort(t *testing.T) {
	applier := NewApplier()
	model := &registry.ModelInfo{ID: "custom-deepseek", Type: "deepseek", UserDefined: true}
	out, err := applier.Apply([]byte(`{"reasoning_effort":"high"}`), thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0}, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Errorf("reasoning_effort must be stripped, payload: %s", out)
	}
	if gjson.GetBytes(out, "thinking.type").String() != "disabled" {
		t.Errorf("expected thinking.type=disabled, payload: %s", out)
	}
}
