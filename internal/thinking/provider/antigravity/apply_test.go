package antigravity

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// budgetModel returns a Gemini-style Antigravity model that uses numeric budgets.
func budgetModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:                  "gemini-2.5-pro",
		Type:                "gemini",
		MaxCompletionTokens: 65536,
		Thinking: &registry.ThinkingSupport{
			Min:            128,
			Max:            32768,
			ZeroAllowed:    true,
			DynamicAllowed: true,
		},
	}
}

// levelModel returns a Gemini 3 style Antigravity model that uses discrete levels.
func levelModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "gemini-3-pro",
		Type: "gemini",
		Thinking: &registry.ThinkingSupport{
			Levels:         []string{"low", "medium", "high"},
			DynamicAllowed: true,
		},
	}
}

// claudeAntigravityModel returns a Claude-family model served over Antigravity.
func claudeAntigravityModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:                  "claude-sonnet-4-5",
		Type:                "claude",
		MaxCompletionTokens: 8000,
		Thinking: &registry.ThinkingSupport{
			Min: 1024,
			Max: 32768,
		},
	}
}

// TestAntigravityApply_TranslationMatrix pins the canonical ThinkingConfig -> Antigravity
// request.generationConfig.thinkingConfig.* translation for each Mode.
func TestAntigravityApply_TranslationMatrix(t *testing.T) {
	a := NewApplier()

	cases := []struct {
		name       string
		body       string
		config     thinking.ThinkingConfig
		modelInfo  *registry.ModelInfo
		wantPath   string
		wantValue  interface{}
		wantAbsent []string
	}{
		{
			name:      "budget_sets_thinkingBudget",
			body:      `{"request":{"generationConfig":{}}}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192},
			modelInfo: budgetModel(),
			wantPath:  "request.generationConfig.thinkingConfig.thinkingBudget",
			wantValue: int64(8192),
		},
		{
			name:      "budget_includeThoughts_true",
			body:      `{"request":{"generationConfig":{}}}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192},
			modelInfo: budgetModel(),
			wantPath:  "request.generationConfig.thinkingConfig.includeThoughts",
			wantValue: true,
		},
		{
			name:      "auto_sets_negative_one",
			body:      `{"request":{"generationConfig":{}}}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeAuto, Budget: -1},
			modelInfo: budgetModel(),
			wantPath:  "request.generationConfig.thinkingConfig.thinkingBudget",
			wantValue: int64(-1),
		},
		{
			// Budget-only model: ModeNone writes thinkingBudget:0 with includeThoughts:false.
			// (thinkingConfig removal only happens on the level-format path.)
			name:      "none_zeroes_budget",
			body:      `{"request":{"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			modelInfo: budgetModel(),
			wantPath:  "request.generationConfig.thinkingConfig.thinkingBudget",
			wantValue: int64(0),
		},
		{
			name:      "level_sets_thinkingLevel",
			body:      `{"request":{"generationConfig":{}}}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: "high"},
			modelInfo: levelModel(),
			wantPath:  "request.generationConfig.thinkingConfig.thinkingLevel",
			wantValue: "high",
		},
		{
			// Claude on Antigravity: budget >= max_tokens must clamp to max_tokens-1.
			name:      "claude_budget_clamped_below_max",
			body:      `{"request":{"generationConfig":{"maxOutputTokens":8000}}}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 16384},
			modelInfo: claudeAntigravityModel(),
			wantPath:  "request.generationConfig.thinkingConfig.thinkingBudget",
			wantValue: int64(7999),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := a.Apply([]byte(tc.body), tc.config, tc.modelInfo)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			if tc.wantValue != nil {
				assertJSONValue(t, out, tc.wantPath, tc.wantValue)
			}
			for _, absent := range tc.wantAbsent {
				if gjson.GetBytes(out, absent).Exists() {
					t.Errorf("path %q should be absent, got: %s", absent, out)
				}
			}
		})
	}
}

// TestAntigravityApply_ClaudeBudgetBelowMinRemovesConfig verifies that when the Claude
// max_tokens constraint would push the budget below the model minimum, the whole
// thinkingConfig is removed (budget=-2 sentinel path).
func TestAntigravityApply_ClaudeBudgetBelowMinRemovesConfig(t *testing.T) {
	a := NewApplier()
	// max_tokens=1000 -> budget capped at 999, which is below Min=1024 -> config removed.
	body := `{"request":{"generationConfig":{"maxOutputTokens":1000,"thinkingConfig":{"thinkingBudget":16384}}}}`
	out, err := a.Apply([]byte(body), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 16384}, claudeAntigravityModel())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if gjson.GetBytes(out, "request.generationConfig.thinkingConfig").Exists() {
		t.Errorf("thinkingConfig should be removed when budget < min, got: %s", out)
	}
}

// TestAntigravityApply_NilThinkingPassthrough verifies models without thinking support
// pass through unchanged.
func TestAntigravityApply_NilThinkingPassthrough(t *testing.T) {
	a := NewApplier()
	body := `{"request":{"generationConfig":{}}}`
	modelInfo := &registry.ModelInfo{ID: "no-think", Type: "gemini"}
	out, err := a.Apply([]byte(body), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}, modelInfo)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if string(out) != body {
		t.Errorf("expected passthrough, got: %s", out)
	}
}

func assertJSONValue(t *testing.T, data []byte, path string, want interface{}) {
	t.Helper()
	got := gjson.GetBytes(data, path)
	if !got.Exists() {
		t.Fatalf("path %q missing in output: %s", path, data)
	}
	switch expected := want.(type) {
	case int64:
		if got.Int() != expected {
			t.Errorf("path %q = %d, want %d\noutput: %s", path, got.Int(), expected, data)
		}
	case string:
		if got.String() != expected {
			t.Errorf("path %q = %q, want %q\noutput: %s", path, got.String(), expected, data)
		}
	case bool:
		if got.Bool() != expected {
			t.Errorf("path %q = %v, want %v\noutput: %s", path, got.Bool(), expected, data)
		}
	default:
		t.Fatalf("unsupported want type %T", want)
	}
}
