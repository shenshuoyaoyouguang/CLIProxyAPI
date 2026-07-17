// Package codex thinking applier translation tests.
//
// Codex mirrors OpenAI but writes the nested reasoning.effort field. These
// tests lock that translation offline via direct Apply calls.
package codex

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func levelOnlyModel(levels []string, zeroAllowed bool) *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "gpt-5.2-codex",
		Type: "codex",
		Thinking: &registry.ThinkingSupport{
			Levels:      levels,
			ZeroAllowed: zeroAllowed,
		},
	}
}

func TestCodexApply_TranslationMatrix(t *testing.T) {
	a := NewApplier()

	cases := []struct {
		name          string
		body          string
		config        thinking.ThinkingConfig
		model         *registry.ModelInfo
		wantPath      string
		wantValue     string
		wantUnchanged bool
	}{
		{
			name:      "level_high_nested_path",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
			model:     levelOnlyModel([]string{"low", "medium", "high"}, false),
			wantPath:  "reasoning.effort",
			wantValue: "high",
		},
		{
			name:      "none_zero_allowed_emits_none",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			model:     levelOnlyModel([]string{"none", "low", "high"}, true),
			wantPath:  "reasoning.effort",
			wantValue: "none",
		},
		{
			name:      "none_zero_not_allowed_falls_to_lowest_level",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			model:     levelOnlyModel([]string{"low", "medium", "high"}, false),
			wantPath:  "reasoning.effort",
			wantValue: "low",
		},
		{
			name:          "budget_mode_passthrough",
			body:          `{"reasoning":{"effort":"medium"}}`,
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

func TestCodexApply_NilThinkingPassthrough(t *testing.T) {
	a := NewApplier()
	body := []byte(`{"input":[]}`)
	model := &registry.ModelInfo{ID: "gpt-4o", Type: "codex"}
	out, err := a.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("expected passthrough, got %s", out)
	}
}
