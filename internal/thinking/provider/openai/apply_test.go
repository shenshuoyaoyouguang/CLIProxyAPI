// Package openai thinking applier translation tests.
//
// These tests lock the canonical ThinkingConfig -> OpenAI reasoning_effort
// translation. They construct ModelInfo directly and call Apply so boundary
// behaviour can be asserted without network access or token spend.
package openai

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// levelOnlyModel builds an OpenAI level-only model with the given supported levels.
func levelOnlyModel(levels []string, zeroAllowed bool) *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "gpt-5.2",
		Type: "openai",
		Thinking: &registry.ThinkingSupport{
			Levels:      levels,
			ZeroAllowed: zeroAllowed,
		},
	}
}

func TestOpenAIApply_TranslationMatrix(t *testing.T) {
	a := NewApplier()

	cases := []struct {
		name      string
		body      string
		config    thinking.ThinkingConfig
		model     *registry.ModelInfo
		wantPath  string
		wantValue string
		// wantUnchanged asserts the body is returned untouched (passthrough).
		wantUnchanged bool
	}{
		{
			name:      "level_high",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
			model:     levelOnlyModel([]string{"low", "medium", "high"}, false),
			wantPath:  "reasoning_effort",
			wantValue: "high",
		},
		{
			name:      "none_zero_allowed_emits_none",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			model:     levelOnlyModel([]string{"none", "low", "high"}, true),
			wantPath:  "reasoning_effort",
			wantValue: "none",
		},
		{
			name:      "none_zero_not_allowed_falls_to_lowest_level",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			model:     levelOnlyModel([]string{"low", "medium", "high"}, false),
			wantPath:  "reasoning_effort",
			wantValue: "low",
		},
		{
			name:          "budget_mode_passthrough",
			body:          `{"reasoning_effort":"medium"}`,
			config:        thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192},
			model:         levelOnlyModel([]string{"low", "medium", "high"}, false),
			wantUnchanged: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := a.Apply([]byte(tc.body), tc.config, tc.model)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			if tc.wantUnchanged {
				if string(out) != tc.body {
					t.Fatalf("expected passthrough, body changed to %s", out)
				}
				return
			}
			if got := gjson.GetBytes(out, tc.wantPath).String(); got != tc.wantValue {
				t.Fatalf("%s = %q, want %q\nbody: %s", tc.wantPath, got, tc.wantValue, out)
			}
		})
	}
}

// TestOpenAIApply_NilThinkingPassthrough verifies models without thinking
// support are left untouched.
func TestOpenAIApply_NilThinkingPassthrough(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"messages":[]}`)
	model := &registry.ModelInfo{ID: "gpt-4o", Type: "openai"}
	out, err := a.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("expected passthrough, got %s", out)
	}
}

// TestOpenAIApply_UserDefinedBudgetConversion verifies user-defined models
// convert a numeric budget to the nearest reasoning_effort level.
func TestOpenAIApply_UserDefinedBudgetConversion(t *testing.T) {
	a := NewApplier()
	model := &registry.ModelInfo{ID: "custom-model", Type: "openai", UserDefined: true}
	out, err := a.Apply([]byte(`{}`), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 24576}, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 24576 maps to "high" via ConvertBudgetToLevel thresholds.
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want high", got)
	}
}
