package gemini

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func levelModelInfo() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "gemini-test",
		Type: "gemini",
		Thinking: &registry.ThinkingSupport{
			Levels: []string{"low", "medium", "high"},
		},
	}
}

// TestApplyModeNoneFullyDisabledRemovesThinkingConfig covers the new early
// fully-disabled return: ModeNone + Budget 0 + empty Level removes the whole
// thinkingConfig subtree regardless of its value shape, so a default-on model
// cannot keep thinking after a disable.
func TestApplyModeNoneFullyDisabledRemovesThinkingConfig(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "object config", body: `{"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`},
		{name: "array config", body: `{"generationConfig":{"thinkingConfig":[]}}`},
		{name: "scalar config", body: `{"generationConfig":{"thinkingConfig":"x"}}`},
		{name: "absent config", body: `{"generationConfig":{}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewApplier().Apply([]byte(tt.body), thinking.ThinkingConfig{Mode: thinking.ModeNone}, levelModelInfo())
			if err != nil {
				t.Fatalf("Apply failed: %v", err)
			}
			if gjson.GetBytes(got, "generationConfig.thinkingConfig").Exists() {
				t.Fatalf("thinkingConfig must be removed, got %s", got)
			}
		})
	}
}

// TestApplyModeLevelSetsLevelAndNormalizesConflictingFields covers the rewritten
// applyLevelFormat: thinkingLevel is set, conflicting budget/level fields and
// legacy includeThoughts names are removed, and an explicit includeThoughts
// boolean is restored in camelCase.
func TestApplyModeLevelSetsLevelAndNormalizesConflictingFields(t *testing.T) {
	body := []byte(`{"generationConfig":{"thinkingConfig":{"thinkingBudget":8192,"thinking_level":"low","include_thoughts":true}}}`)
	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: "high"}, levelModelInfo())
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	tc := gjson.GetBytes(got, "generationConfig.thinkingConfig")
	if gotLevel := tc.Get("thinkingLevel").String(); gotLevel != "high" {
		t.Fatalf("thinkingLevel = %q, want high", gotLevel)
	}
	if tc.Get("thinkingBudget").Exists() || tc.Get("thinking_level").Exists() {
		t.Fatalf("conflicting fields must be removed, got %s", tc.Raw)
	}
	if !tc.Get("includeThoughts").Bool() {
		t.Fatalf("includeThoughts = false, want true (restored from include_thoughts)")
	}
}

// TestApplyNoVisibleChangeKeepsBodyUntouched covers
// setGeminiThinkingConfigIfChanged: when the mutation leaves the subtree
// byte-identical, the original body is returned untouched instead of being
// rewritten with a re-serialized (potentially relabeled) object.
func TestApplyNoVisibleChangeKeepsBodyUntouched(t *testing.T) {
	body := []byte(`{"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`)
	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}, levelModelInfo())
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("no-op budget must keep the body untouched, got %s", got)
	}
}

// TestApplyArrayThinkingConfigUntouched covers the array guard: an array-valued
// thinkingConfig cannot be written via sjson, so the body must pass through
// unchanged.
func TestApplyArrayThinkingConfigUntouched(t *testing.T) {
	body := []byte(`{"generationConfig":{"thinkingConfig":[1,2]}}`)
	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 8192}, levelModelInfo())
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("array-valued thinkingConfig must be left untouched, got %s", got)
	}
}
