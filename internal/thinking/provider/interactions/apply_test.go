// Package interactions thinking applier translation tests.
//
// Interactions writes native generation_config.thinking_level and
// generation_config.thinking_summaries fields. These tests pin the level path,
// the budget->level conversion, the auto path, and the none path, and confirm
// that stale thinking fields from the incoming body are stripped.
package interactions

import (
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func interactionsModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "gemini-3-pro",
		Type: "gemini",
		Thinking: &registry.ThinkingSupport{
			Levels: []string{"low", "medium", "high"},
		},
	}
}

func TestInteractionsApply_TranslationMatrix(t *testing.T) {
	applier := NewApplier()

	cases := []struct {
		name          string
		body          string
		config        thinking.ThinkingConfig
		model         *registry.ModelInfo
		wantLevel     string // value at generation_config.thinking_level ("" means must be absent)
		wantSummaries string // value at generation_config.thinking_summaries ("" means unchecked)
	}{
		{
			name:          "level_high",
			body:          `{}`,
			config:        thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
			model:         interactionsModel(),
			wantLevel:     "high",
			wantSummaries: "auto",
		},
		{
			name:          "budget_converts_to_level",
			body:          `{}`,
			config:        thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: 24576}, // -> "high"
			model:         interactionsModel(),
			wantLevel:     "high",
			wantSummaries: "auto",
		},
		{
			name:          "auto_sets_summaries_only",
			body:          `{}`,
			config:        thinking.ThinkingConfig{Mode: thinking.ModeAuto, Budget: -1},
			model:         interactionsModel(),
			wantLevel:     "",
			wantSummaries: "auto",
		},
		{
			name:          "none_sets_summaries_none",
			body:          `{}`,
			config:        thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			model:         interactionsModel(),
			wantLevel:     "",
			wantSummaries: "none",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := applier.Apply([]byte(tc.body), tc.config, tc.model)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			level := gjson.GetBytes(out, "generation_config.thinking_level")
			if tc.wantLevel == "" {
				if level.Exists() {
					t.Errorf("expected thinking_level absent, got %q\npayload: %s", level.String(), out)
				}
			} else if level.String() != tc.wantLevel {
				t.Errorf("thinking_level = %q, want %q\npayload: %s", level.String(), tc.wantLevel, out)
			}
			if tc.wantSummaries != "" {
				if got := gjson.GetBytes(out, "generation_config.thinking_summaries").String(); got != tc.wantSummaries {
					t.Errorf("thinking_summaries = %q, want %q\npayload: %s", got, tc.wantSummaries, out)
				}
			}
		})
	}
}

// TestInteractionsApply_InvalidBudgetReturnsError verifies an explicit invalid
// budget (below -1) is rejected with a ThinkingError rather than silently passed through.
func TestInteractionsApply_InvalidBudgetReturnsError(t *testing.T) {
	applier := NewApplier()
	out, err := applier.Apply([]byte(`{}`), thinking.ThinkingConfig{Mode: thinking.ModeBudget, Budget: -5}, interactionsModel())
	if err == nil {
		t.Fatalf("expected ThinkingError for invalid budget, got nil (out=%s)", out)
	}
	var te *thinking.ThinkingError
	if !errors.As(err, &te) {
		t.Fatalf("expected *thinking.ThinkingError, got %T: %v", err, err)
	}
	if te.Code != thinking.ErrBudgetOutOfRange {
		t.Fatalf("expected ErrBudgetOutOfRange, got %q", te.Code)
	}
}

// TestInteractionsApply_StripsStaleThinkingFields verifies stale thinking fields
// present in the incoming body are removed and rewritten canonically, preventing
// conflicting duplicates (e.g. both thinkingBudget and thinking_level) reaching
// upstream.
func TestInteractionsApply_StripsStaleThinkingFields(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"generation_config":{"thinking_budget":9999,"thinkingLevel":"low"}}`)
	out, err := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}, interactionsModel())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if gjson.GetBytes(out, "generation_config.thinking_budget").Exists() {
		t.Errorf("stale thinking_budget must be stripped, payload: %s", out)
	}
	if gjson.GetBytes(out, "generation_config.thinkingLevel").Exists() {
		t.Errorf("stale thinkingLevel must be stripped, payload: %s", out)
	}
	if got := gjson.GetBytes(out, "generation_config.thinking_level").String(); got != "high" {
		t.Errorf("thinking_level = %q, want high\npayload: %s", got, out)
	}
}

// TestInteractionsApply_ClampsUnsupportedLevel verifies a level outside the
// model's supported set is normalized to the highest supported level rather than
// passed through verbatim.
func TestInteractionsApply_ClampsUnsupportedLevel(t *testing.T) {
	applier := NewApplier()
	// Model supports only low/medium; request "high" should clamp to "medium".
	model := &registry.ModelInfo{
		ID:       "gemini-3-lite",
		Type:     "gemini",
		Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium"}},
	}
	out, err := applier.Apply([]byte(`{}`), thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh}, model)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if got := gjson.GetBytes(out, "generation_config.thinking_level").String(); got != "medium" {
		t.Errorf("thinking_level = %q, want medium (clamped)\npayload: %s", got, out)
	}
}
