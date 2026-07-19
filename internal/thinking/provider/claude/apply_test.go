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

// TestClaudeApply_BudgetCappedAtMaxTokensMinusGap verifies that when the requested
// budget is at or above max_tokens, the applied thinking.budget_tokens is reduced to
// max_tokens - claudeBudgetMaxGap (Anthropic requires budget_tokens < max_tokens).
func TestClaudeApply_BudgetCappedAtMaxTokensMinusGap(t *testing.T) {
	applier := NewApplier()
	model := budgetModel() // MaxCompletionTokens=64000, Min/Max=1024/32000

	cases := []struct {
		name       string
		body       string
		budget     int
		wantBudget int64
	}{
		{
			name:       "budget_equals_max_tokens",
			body:       `{"max_tokens":8000}`,
			budget:     8000,
			wantBudget: 8000 - claudeBudgetMaxGap,
		},
		{
			name:       "budget_exceeds_max_tokens",
			body:       `{"max_tokens":8000}`,
			budget:     32000,
			wantBudget: 8000 - claudeBudgetMaxGap,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := applier.Apply([]byte(tc.body), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: tc.budget}, model)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			if got := gjson.GetBytes(out, "thinking.budget_tokens").Int(); got != tc.wantBudget {
				t.Errorf("thinking.budget_tokens = %d, want %d (max_tokens-claudeBudgetMaxGap)", got, tc.wantBudget)
			}
		})
	}
}

func TestClaudeApply_BudgetBelowMinAfterMaxCapRemovesThinking(t *testing.T) {
	applier := NewApplier()
	model := budgetModel()

	out, err := applier.Apply([]byte(`{"max_tokens":1000}`), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 16384}, model)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if gjson.GetBytes(out, "thinking").Exists() {
		t.Fatalf("thinking config should be removed when max_tokens cap pushes budget below min; payload: %s", out)
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

// TestNormalizeClaudeBudget_WritesAdjustedBudgetBack pins the contract that
// normalizeClaudeBudget itself writes the adjusted budget back into
// thinking.budget_tokens on the returned body. The Apply path relies on this
// (it no longer re-writes the field at the call site), so a future refactor
// that drops the in-function write would silently break the clamp. Run this
// alongside the higher-level Apply test to keep both layers honest.
func TestNormalizeClaudeBudget_WritesAdjustedBudgetBack(t *testing.T) {
	applier := NewApplier()
	model := budgetModel() // MaxCompletionTokens=64000

	cases := []struct {
		name        string
		body        string
		budget      int
		wantBudget  int64
		wantWritten bool
	}{
		{
			name:        "budget below max_tokens: no write needed",
			body:        `{"max_tokens":64000,"thinking":{"type":"enabled","budget_tokens":16384}}`,
			budget:      16384,
			wantBudget:  16384,
			wantWritten: false, // adjustedBudget == budgetTokens; in-function write is skipped
		},
		{
			name:        "budget equals max_tokens: clamped and written",
			body:        `{"max_tokens":8000,"thinking":{"type":"enabled","budget_tokens":8000}}`,
			budget:      8000,
			wantBudget:  8000 - claudeBudgetMaxGap,
			wantWritten: true,
		},
		{
			name:        "budget exceeds max_tokens: clamped and written",
			body:        `{"max_tokens":8000,"thinking":{"type":"enabled","budget_tokens":32000}}`,
			budget:      32000,
			wantBudget:  8000 - claudeBudgetMaxGap,
			wantWritten: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, adjusted := applier.normalizeClaudeBudget([]byte(tc.body), tc.budget, model)
			if int64(adjusted) != tc.wantBudget {
				t.Fatalf("adjusted budget = %d, want %d", adjusted, tc.wantBudget)
			}
			got := gjson.GetBytes(out, "thinking.budget_tokens").Int()
			if got != tc.wantBudget {
				t.Fatalf("thinking.budget_tokens in returned body = %d, want %d (body=%s)", got, tc.wantBudget, out)
			}
		})
	}
}

// TestNormalizeClaudeBudget_ApplierPathReliesOnInFunctionWrite is a behavioural
// mirror of the Apply call site: Apply no longer re-writes thinking.budget_tokens
// after normalizeClaudeBudget returns. If a future change removes the
// in-function write, this test will fail because budget_tokens will retain the
// pre-clamp value.
func TestNormalizeClaudeBudget_ApplierPathReliesOnInFunctionWrite(t *testing.T) {
	applier := NewApplier()
	model := budgetModel()
	body := []byte(`{"max_tokens":8000}`)
	out, err := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 32000}, model)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got := gjson.GetBytes(out, "thinking.budget_tokens").Int()
	if got != 8000-claudeBudgetMaxGap {
		t.Fatalf("Apply did not propagate normalizeClaudeBudget's clamped value; got %d, want %d (body=%s)",
			got, 8000-claudeBudgetMaxGap, out)
	}
}
