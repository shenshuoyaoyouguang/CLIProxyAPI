// Package kimi thinking applier translation tests.
//
// Kimi uses a native thinking object (thinking.type + thinking.effort) and must
// strip the legacy flat reasoning_effort field from the final payload. These
// tests pin that contract, including the budget->level conversion path and the
// explicit-disable path.
package kimi

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// kimiModel builds a Kimi model definition. Kimi is treated as a level-capable
// model with a discrete Levels set at the thinking layer.
func kimiModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "kimi-k2.5",
		Type: "kimi",
		Thinking: &registry.ThinkingSupport{
			Levels: []string{"low", "medium", "high"},
		},
	}
}

func TestKimiApply_TranslationMatrix(t *testing.T) {
	applier := NewApplier()

	cases := []struct {
		name       string
		body       string
		config     thinking.ThinkingConfig
		model      *registry.ModelInfo
		wantType   string
		wantEffort string // "" means effort must be absent
		wantAbsent []string
	}{
		{
			name:       "level_enabled_with_effort",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
			model:      kimiModel(),
			wantType:   "enabled",
			wantEffort: "high",
		},
		{
			name:       "none_produces_disabled_object",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			model:      kimiModel(),
			wantType:   "disabled",
			wantEffort: "",
		},
		{
			name:       "budget_converts_to_level_effort",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 24576}, // -> "high"
			model:      kimiModel(),
			wantType:   "enabled",
			wantEffort: "high",
		},
		{
			name:       "auto_maps_to_auto_effort",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeAuto, Budget: -1},
			model:      kimiModel(),
			wantType:   "enabled",
			wantEffort: "auto",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := applier.Apply([]byte(tc.body), tc.config, tc.model)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			if got := gjson.GetBytes(out, "thinking.type").String(); got != tc.wantType {
				t.Errorf("thinking.type = %q, want %q\npayload: %s", got, tc.wantType, out)
			}
			effort := gjson.GetBytes(out, "thinking.effort")
			if tc.wantEffort == "" {
				if effort.Exists() {
					t.Errorf("expected thinking.effort absent, got %q\npayload: %s", effort.String(), out)
				}
			} else if effort.String() != tc.wantEffort {
				t.Errorf("thinking.effort = %q, want %q\npayload: %s", effort.String(), tc.wantEffort, out)
			}
			for _, absent := range tc.wantAbsent {
				if gjson.GetBytes(out, absent).Exists() {
					t.Errorf("expected %s to be absent, payload: %s", absent, out)
				}
			}
		})
	}
}

// TestKimiApply_StripsLegacyReasoningEffort verifies the legacy flat
// reasoning_effort field is removed from the final Kimi payload on both the
// enabled and disabled paths.
func TestKimiApply_StripsLegacyReasoningEffort(t *testing.T) {
	applier := NewApplier()

	t.Run("enabled_strips_legacy", func(t *testing.T) {
		out, err := applier.Apply([]byte(`{"reasoning_effort":"medium"}`),
			thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}, kimiModel())
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
		if gjson.GetBytes(out, "reasoning_effort").Exists() {
			t.Errorf("legacy reasoning_effort must be stripped, payload: %s", out)
		}
		if gjson.GetBytes(out, "thinking.effort").String() != "high" {
			t.Errorf("expected thinking.effort=high, payload: %s", out)
		}
	})

	t.Run("disabled_strips_legacy", func(t *testing.T) {
		out, err := applier.Apply([]byte(`{"reasoning_effort":"medium"}`),
			thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0}, kimiModel())
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
		if gjson.GetBytes(out, "reasoning_effort").Exists() {
			t.Errorf("legacy reasoning_effort must be stripped, payload: %s", out)
		}
		if gjson.GetBytes(out, "thinking.type").String() != "disabled" {
			t.Errorf("expected thinking.type=disabled, payload: %s", out)
		}
	})
}

// TestKimiApply_NilThinkingPassthrough verifies a model without thinking support
// passes the body through unchanged.
func TestKimiApply_NilThinkingPassthrough(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"messages":[]}`)
	out, err := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
		&registry.ModelInfo{ID: "kimi-no-think", Type: "kimi"})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if string(out) != string(body) {
		t.Errorf("expected passthrough, got %s", out)
	}
}
