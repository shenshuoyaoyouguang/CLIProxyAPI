package nvidia

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// TestApplyEnabledLevel_NonLowClearsReasoningBudget verifies that the default
// branch clears reasoning_budget just like the low branch does.
// reasoning_budget and medium_effort are mutually exclusive upstream, and a
// level request must never leave a stale budget behind.
func TestApplyEnabledLevel_NonLowClearsReasoningBudget(t *testing.T) {
	body := []byte(`{"model":"nemotron-3-ultra","reasoning_budget":4096,"medium_effort":true}`)

	got, err := applyEnabledLevel(body, string(thinking.LevelHigh))
	if err != nil {
		t.Fatalf("applyEnabledLevel failed: %v", err)
	}
	if gjson.GetBytes(got, "reasoning_budget").Exists() {
		t.Fatalf("reasoning_budget must be cleared for non-low levels, got %s", got)
	}
	if gjson.GetBytes(got, "medium_effort").Exists() {
		t.Fatalf("medium_effort must be cleared for non-low levels, got %s", got)
	}
	if !gjson.GetBytes(got, "enable_thinking").Bool() {
		t.Fatalf("enable_thinking must be set, got %s", got)
	}
}

// TestApplyEnabledLevel_LowStillClearsBudget guards the pre-existing low-branch
// behaviour against regressions from the default-branch change.
func TestApplyEnabledLevel_LowStillClearsBudget(t *testing.T) {
	body := []byte(`{"model":"nemotron-3-ultra","reasoning_budget":4096}`)

	got, err := applyEnabledLevel(body, string(thinking.LevelLow))
	if err != nil {
		t.Fatalf("applyEnabledLevel failed: %v", err)
	}
	if gjson.GetBytes(got, "reasoning_budget").Exists() {
		t.Fatalf("reasoning_budget must be cleared for low, got %s", got)
	}
	if !gjson.GetBytes(got, "medium_effort").Bool() {
		t.Fatalf("medium_effort must be set for low, got %s", got)
	}
}
