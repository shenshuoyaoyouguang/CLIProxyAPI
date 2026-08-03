package claude

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// TestApply_BudgetConflictDisablesThinking covers the case where Anthropic's
// budget_tokens < max_tokens constraint and the model's minimum budget cannot
// both be satisfied. Previously the request was returned untouched, which left
// budget_tokens >= max_tokens on the wire and produced a guaranteed upstream
// 400. Thinking must be disabled instead so the request stays valid.
func TestApply_BudgetConflictDisablesThinking(t *testing.T) {
	modelInfo := &registry.ModelInfo{
		ID:   "claude-test",
		Type: "claude",
		Thinking: &registry.ThinkingSupport{
			Min: 1024,
			Max: 32000,
		},
	}
	body := []byte(`{"model":"claude-test","max_tokens":1000,"messages":[{"role":"user","content":"hi"}]}`)

	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 2000}, modelInfo)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if tt := gjson.GetBytes(got, "thinking.type").String(); tt != "disabled" {
		t.Fatalf("thinking.type = %q, want \"disabled\" (unsatisfiable constraints), body=%s", tt, got)
	}
	if gjson.GetBytes(got, "thinking.budget_tokens").Exists() {
		t.Fatalf("illegal budget_tokens must not survive, got %s", got)
	}
	if mt := gjson.GetBytes(got, "max_tokens").Int(); mt != 1000 {
		t.Fatalf("caller-provided max_tokens must be preserved, got %d in %s", mt, got)
	}
}

// TestApply_BudgetClampedStaysEnabled guards the normal clamp path: when the
// adjusted budget still satisfies the model minimum, thinking stays enabled and
// budget_tokens is clamped below max_tokens.
func TestApply_BudgetClampedStaysEnabled(t *testing.T) {
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
		t.Fatalf("thinking.type = %q, want \"enabled\", body=%s", tt, got)
	}
	if bt := gjson.GetBytes(got, "thinking.budget_tokens").Int(); bt != 3999 {
		t.Fatalf("budget_tokens = %d, want 3999 (max_tokens-1), body=%s", bt, got)
	}
}
