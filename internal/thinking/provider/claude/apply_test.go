// Package claude thinking applier tests.
//
// These are pure, offline unit tests for the Claude ProviderApplier. They do NOT
// hit the network or consume upstream tokens. Each case constructs a ModelInfo
// directly and asserts the exact JSON translation produced by Apply, guarding the
// "canonical ThinkingConfig -> Claude native protocol" contract against regressions.
package claude

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// budgetModel returns a Claude budget-only model (manual thinking via budget_tokens).
func budgetModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:                  "claude-sonnet-4-5",
		Type:                "claude",
		MaxCompletionTokens: 64000,
		Thinking: &registry.ThinkingSupport{
			Min:         1024,
			Max:         32000,
			ZeroAllowed: true,
		},
	}
}

// adaptiveModel returns a Claude 4.6-style model that advertises discrete effort levels.
func adaptiveModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:                  "claude-opus-4-6",
		Type:                "claude",
		MaxCompletionTokens: 64000,
		Thinking: &registry.ThinkingSupport{
			Levels: []string{"low", "medium", "high", "max"},
		},
	}
}

// TestClaudeApply_TranslationMatrix asserts the exact Claude wire format produced
// for each canonical Mode, including the Anthropic max_tokens > budget_tokens
// constraint and field-clearing behavior.
func TestClaudeApply_TranslationMatrix(t *testing.T) {
	applier := NewApplier()

	type assertion struct {
		path string
		want interface{}
	}
	cases := []struct {
		name    string
		body    string
		config  thinking.ThinkingConfig
		model   *registry.ModelInfo
		asserts []assertion
		absent  []string // JSON paths that must NOT exist after Apply
	}{
		{
			name:   "budget_enabled",
			body:   `{"max_tokens":64000}`,
			config: thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 16384},
			model:  budgetModel(),
			asserts: []assertion{
				{"thinking.type", "enabled"},
				{"thinking.budget_tokens", int64(16384)},
			},
		},
		{
			name:   "budget_clamped_below_max_tokens",
			body:   `{"max_tokens":8000}`,
			config: thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 16384},
			model:  budgetModel(),
			asserts: []assertion{
				{"thinking.type", "enabled"},
				// budget >= max_tokens must be reduced to max_tokens-1.
				{"thinking.budget_tokens", int64(7999)},
			},
		},
		{
			name:   "budget_zero_disables",
			body:   `{}`,
			config: thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 0},
			model:  budgetModel(),
			asserts: []assertion{
				{"thinking.type", "disabled"},
			},
			absent: []string{"thinking.budget_tokens"},
		},
		{
			name:   "none_disables",
			body:   `{"thinking":{"type":"enabled","budget_tokens":9999}}`,
			config: thinking.ThinkingConfig{Mode: thinking.ModeNone},
			model:  budgetModel(),
			asserts: []assertion{
				{"thinking.type", "disabled"},
			},
			absent: []string{"thinking.budget_tokens"},
		},
		{
			name:   "level_adaptive_effort",
			body:   `{}`,
			config: thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
			model:  adaptiveModel(),
			asserts: []assertion{
				{"thinking.type", "adaptive"},
				{"output_config.effort", "high"},
			},
			absent: []string{"thinking.budget_tokens"},
		},
		{
			name:   "auto_adaptive_defaults",
			body:   `{}`,
			config: thinking.ThinkingConfig{Mode: thinking.ModeAuto, Budget: -1},
			model:  adaptiveModel(),
			asserts: []assertion{
				{"thinking.type", "adaptive"},
			},
			// Adaptive auto omits explicit effort so upstream default applies.
			absent: []string{"output_config.effort", "thinking.budget_tokens"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := applier.Apply([]byte(tc.body), tc.config, tc.model)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			if !gjson.ValidBytes(out) {
				t.Fatalf("Apply produced invalid JSON: %s", out)
			}
			for _, a := range tc.asserts {
				got := gjson.GetBytes(out, a.path)
				if !got.Exists() {
					t.Fatalf("path %q missing; body=%s", a.path, out)
				}
				switch want := a.want.(type) {
				case string:
					if got.String() != want {
						t.Errorf("path %q = %q, want %q", a.path, got.String(), want)
					}
				case int64:
					if got.Int() != want {
						t.Errorf("path %q = %d, want %d", a.path, got.Int(), want)
					}
				}
			}
			for _, p := range tc.absent {
				if gjson.GetBytes(out, p).Exists() {
					t.Errorf("path %q should be absent; body=%s", p, out)
				}
			}
		})
	}
}

// TestClaudeApply_NilThinkingPassthrough verifies that a model without thinking
// support is left untouched.
func TestClaudeApply_NilThinkingPassthrough(t *testing.T) {
	applier := NewApplier()
	model := &registry.ModelInfo{ID: "claude-haiku-4-5", Type: "claude"} // Thinking == nil
	body := []byte(`{"messages":[]}`)
	out, err := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}, model)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated for nil-thinking model: got %s, want %s", out, body)
	}
}
