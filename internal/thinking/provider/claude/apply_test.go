package claude

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// TestApplyModeNoneDropsDisplay covers the display-keep/drop distinction: the
// ModeNone path removes thinking.display and any budget_tokens, and drops
// output_config entirely when it would end up empty.
func TestApplyModeNoneDropsDisplay(t *testing.T) {
	modelInfo := &registry.ModelInfo{
		ID:   "claude-test",
		Type: "claude",
		Thinking: &registry.ThinkingSupport{
			Min: 1024,
			Max: 32000,
		},
	}
	body := []byte(`{"model":"claude-test","thinking":{"type":"enabled","budget_tokens":4096,"display":{"summary":"x"}},"output_config":{"effort":"high"}}`)
	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeNone}, modelInfo)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if tt := gjson.GetBytes(got, "thinking.type").String(); tt != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled", tt)
	}
	if gjson.GetBytes(got, "thinking.budget_tokens").Exists() {
		t.Fatalf("budget_tokens must be removed, got %s", got)
	}
	if gjson.GetBytes(got, "thinking.display").Exists() {
		t.Fatalf("thinking.display must be dropped on ModeNone, got %s", got)
	}
	if gjson.GetBytes(got, "output_config").Exists() {
		t.Fatalf("empty output_config must be dropped, got %s", got)
	}
}

// TestApplyModeBudgetConflictKeepsDisplay covers the budget-conflict disable
// path: when budget constraints are unsatisfiable, thinking is disabled but
// display is kept so a summary remains visible (the non-ModeNone path).
func TestApplyModeBudgetConflictKeepsDisplay(t *testing.T) {
	modelInfo := &registry.ModelInfo{
		ID:   "claude-test",
		Type: "claude",
		Thinking: &registry.ThinkingSupport{
			Min: 1024,
			Max: 32000,
		},
	}
	body := []byte(`{"model":"claude-test","max_tokens":1000,"thinking":{"type":"enabled","budget_tokens":2000,"display":{"summary":"kept"}}}`)
	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 2000}, modelInfo)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if tt := gjson.GetBytes(got, "thinking.type").String(); tt != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled (unsatisfiable constraints)", tt)
	}
	if gotDisplay := gjson.GetBytes(got, "thinking.display.summary").String(); gotDisplay != "kept" {
		t.Fatalf("thinking.display must be kept on the conflict path, got %s", got)
	}
}

// TestApplyModeBudgetSetsEnabledAndClamps covers the normal budget path:
// thinking is enabled and budget_tokens is clamped below max_tokens.
func TestApplyModeBudgetSetsEnabledAndClamps(t *testing.T) {
	modelInfo := &registry.ModelInfo{
		ID:   "claude-test",
		Type: "claude",
		Thinking: &registry.ThinkingSupport{
			Min: 1024,
			Max: 32000,
		},
	}
	body := []byte(`{"model":"claude-test","max_tokens":4000,"messages":[{"role":"user","content":"hi"}]}`)
	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8000}, modelInfo)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if tt := gjson.GetBytes(got, "thinking.type").String(); tt != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled", tt)
	}
	if bt := gjson.GetBytes(got, "thinking.budget_tokens").Int(); bt != 3999 {
		t.Fatalf("budget_tokens = %d, want 3999 (max_tokens-1)", bt)
	}
}
