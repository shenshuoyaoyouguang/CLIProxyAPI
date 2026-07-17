package gemini

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// gemini25Model returns a budget-only Gemini 2.5 style model (Min/Max, no Levels).
func gemini25Model() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "gemini-2.5-pro",
		Type: "gemini",
		Thinking: &registry.ThinkingSupport{
			Min:            128,
			Max:            32768,
			ZeroAllowed:    true,
			DynamicAllowed: true,
		},
	}
}

// gemini3Model returns a level-based Gemini 3.x style model (has Levels).
func gemini3Model() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "gemini-3-pro",
		Type: "gemini",
		Thinking: &registry.ThinkingSupport{
			Levels:         []string{"low", "medium", "high"},
			DynamicAllowed: true,
		},
	}
}

// TestGeminiApply_BudgetFormat verifies Gemini 2.5 numeric thinkingBudget output.
func TestGeminiApply_BudgetFormat(t *testing.T) {
	a := NewApplier()
	cases := []struct {
		name              string
		body              string
		config            thinking.ThinkingConfig
		wantBudget        int64
		wantIncludeThinks bool
		wantConfigAbsent  bool
	}{
		{
			name:              "budget_positive",
			body:              `{}`,
			config:            thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192},
			wantBudget:        8192,
			wantIncludeThinks: true,
		},
		{
			name:              "auto_dynamic",
			body:              `{}`,
			config:            thinking.ThinkingConfig{Mode: thinking.ModeAuto, Budget: -1},
			wantBudget:        -1,
			wantIncludeThinks: true,
		},
		{
			// Budget-only model (no Levels): ModeNone keeps thinkingConfig but sets
			// thinkingBudget=0 and includeThoughts=false (config removal only happens
			// in the level-format path). This pins that provider-specific behavior.
			name:              "none_sets_zero_budget",
			body:              `{}`,
			config:            thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			wantBudget:        0,
			wantIncludeThinks: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := a.Apply([]byte(tc.body), tc.config, gemini25Model())
			if err != nil {
				t.Fatalf("Apply error: %v", err)
			}
			cfg := gjson.GetBytes(out, "generationConfig.thinkingConfig")
			if tc.wantConfigAbsent {
				if cfg.Exists() {
					t.Fatalf("expected thinkingConfig removed, got: %s", out)
				}
				return
			}
			if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingBudget").Int(); got != tc.wantBudget {
				t.Errorf("thinkingBudget = %d, want %d (%s)", got, tc.wantBudget, out)
			}
			if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.includeThoughts").Bool(); got != tc.wantIncludeThinks {
				t.Errorf("includeThoughts = %v, want %v", got, tc.wantIncludeThinks)
			}
			// Budget format must never emit a thinkingLevel field.
			if gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingLevel").Exists() {
				t.Errorf("budget format leaked thinkingLevel: %s", out)
			}
		})
	}
}

// TestGeminiApply_LevelFormat verifies Gemini 3.x discrete thinkingLevel output.
func TestGeminiApply_LevelFormat(t *testing.T) {
	a := NewApplier()
	cases := []struct {
		name       string
		body       string
		config     thinking.ThinkingConfig
		wantLevel  string
		wantAbsent bool // whole thinkingConfig absent
	}{
		{
			name:      "level_high",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
			wantLevel: "high",
		},
		{
			name:       "none_disables_removes_config",
			body:       `{}`,
			config:     thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			wantAbsent: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := a.Apply([]byte(tc.body), tc.config, gemini3Model())
			if err != nil {
				t.Fatalf("Apply error: %v", err)
			}
			if tc.wantAbsent {
				if gjson.GetBytes(out, "generationConfig.thinkingConfig").Exists() {
					t.Fatalf("expected thinkingConfig removed, got: %s", out)
				}
				return
			}
			if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingLevel").String(); got != tc.wantLevel {
				t.Errorf("thinkingLevel = %q, want %q (%s)", got, tc.wantLevel, out)
			}
			// Level format must never emit a numeric thinkingBudget field.
			if gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingBudget").Exists() {
				t.Errorf("level format leaked thinkingBudget: %s", out)
			}
		})
	}
}

// TestGeminiApply_RespectsIncludeThoughts verifies user's explicit includeThoughts is preserved.
func TestGeminiApply_RespectsIncludeThoughts(t *testing.T) {
	a := NewApplier()
	body := `{"generationConfig":{"thinkingConfig":{"includeThoughts":false}}}`
	out, err := a.Apply([]byte(body), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}, gemini25Model())
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.includeThoughts").Bool(); got != false {
		t.Errorf("includeThoughts = %v, want false (user override must win)", got)
	}
}

// TestGeminiApply_NilThinkingPassthrough verifies models without thinking support pass through.
func TestGeminiApply_NilThinkingPassthrough(t *testing.T) {
	a := NewApplier()
	body := `{"foo":"bar"}`
	out, err := a.Apply([]byte(body), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192},
		&registry.ModelInfo{ID: "no-think", Type: "gemini"})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if string(out) != body {
		t.Errorf("expected passthrough, got: %s", out)
	}
}
