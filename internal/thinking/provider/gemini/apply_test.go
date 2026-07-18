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

// TestGeminiApply_ModeNoneHidesThoughts verifies that a ModeNone request always emits
// includeThoughts=false, even when the budget is clamped to a positive value (a model
// that cannot be disabled). Non-disabled modes must emit includeThoughts=true by default.
func TestGeminiApply_ModeNoneHidesThoughts(t *testing.T) {
	a := NewApplier()

	// Budget-only model (no Levels): ModeNone keeps thinkingConfig with
	// thinkingBudget and includeThoughts=false regardless of whether Budget=0 or >0.
	budgetOnlyCases := []struct {
		name   string
		config thinking.ThinkingConfig
	}{
		{name: "none_budget_zero", config: thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0}},
		// A clamped positive budget (model cannot disable) must still hide thoughts.
		{name: "none_budget_clamped", config: thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 4096}},
	}
	for _, tc := range budgetOnlyCases {
		t.Run("budget_only/"+tc.name, func(t *testing.T) {
			out, err := a.Apply([]byte(`{}`), tc.config, gemini25Model())
			if err != nil {
				t.Fatalf("Apply error: %v", err)
			}
			if gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingBudget").Exists() {
				if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.includeThoughts").Bool(); got {
					t.Errorf("ModeNone must set includeThoughts=false, got true (%s)", out)
				}
			}
		})
	}

	// Level model: ModeNone + Budget=0 removes thinkingConfig entirely (no includeThoughts).
	t.Run("level/none_disables_removes_config", func(t *testing.T) {
		out, err := a.Apply([]byte(`{}`), thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0}, gemini3Model())
		if err != nil {
			t.Fatalf("Apply error: %v", err)
		}
		if gjson.GetBytes(out, "generationConfig.thinkingConfig").Exists() {
			t.Errorf("ModeNone level model must remove thinkingConfig, got: %s", out)
		}
	})

	// Non-disabled modes emit includeThoughts=true by default.
	enabledCases := []struct {
		name   string
		config thinking.ThinkingConfig
	}{
		{name: "budget_positive", config: thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}},
		{name: "auto", config: thinking.ThinkingConfig{Mode: thinking.ModeAuto, Budget: -1}},
		{name: "level", config: thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}},
	}
	for _, tc := range enabledCases {
		t.Run("enabled/"+tc.name, func(t *testing.T) {
			model := gemini25Model()
			if tc.config.Mode == thinking.ModeLevel {
				model = gemini3Model()
			}
			out, err := a.Apply([]byte(`{}`), tc.config, model)
			if err != nil {
				t.Fatalf("Apply error: %v", err)
			}
			if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.includeThoughts").Bool(); !got {
				t.Errorf("%s must emit includeThoughts=true, got false (%s)", tc.name, out)
			}
		})
	}
}
