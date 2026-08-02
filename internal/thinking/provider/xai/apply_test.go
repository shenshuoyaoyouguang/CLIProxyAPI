// Package xai thinking applier translation tests.
//
// xAI reuses the Codex applier (embedded codex.Applier) but registers under the
// "xai" provider name and emits the OpenAI Responses API "reasoning.effort" field.
// These tests pin that contract so a future refactor of the embedding cannot
// silently change the emitted payload.
package xai

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// xaiModel builds a level-only xAI model definition.
func xaiModel() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "grok-4",
		Type: "xai",
		Thinking: &registry.ThinkingSupport{
			Levels: []string{"low", "high"},
		},
	}
}

func TestXAIApply_TranslationMatrix(t *testing.T) {
	applier := NewApplier()

	cases := []struct {
		name       string
		body       string
		config     thinking.ThinkingConfig
		model      *registry.ModelInfo
		wantPath   string
		wantValue  string
		wantAbsent []string
	}{
		{
			name:      "level_sets_reasoning_effort",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
			model:     xaiModel(),
			wantPath:  "reasoning.effort",
			wantValue: "high",
		},
		{
			name:      "none_falls_back_to_lowest_level_when_zero_not_allowed",
			body:      `{}`,
			config:    thinking.ThinkingConfig{Mode: thinking.ModeNone, Budget: 0},
			model:     xaiModel(),
			wantPath:  "reasoning.effort",
			wantValue: "low", // lowest supported level, since ZeroAllowed=false and no "none" level
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := applier.Apply([]byte(tc.body), tc.config, tc.model)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			if got := gjson.GetBytes(out, tc.wantPath).String(); got != tc.wantValue {
				t.Errorf("%s = %q, want %q\npayload: %s", tc.wantPath, got, tc.wantValue, out)
			}
			for _, absent := range tc.wantAbsent {
				if gjson.GetBytes(out, absent).Exists() {
					t.Errorf("expected %s to be absent, payload: %s", absent, out)
				}
			}
		})
	}
}

// TestXAIApply_UsesReasoningEffortNestedField guards the field-name contract:
// xAI must emit the nested "reasoning.effort" (Responses API), never the flat
// "reasoning_effort" (Chat Completions API).
func TestXAIApply_UsesReasoningEffortNestedField(t *testing.T) {
	applier := NewApplier()
	out, err := applier.Apply([]byte(`{}`),
		thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
		xaiModel())
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Errorf("xai must not emit flat reasoning_effort, payload: %s", out)
	}
	if !gjson.GetBytes(out, "reasoning.effort").Exists() {
		t.Errorf("xai must emit nested reasoning.effort, payload: %s", out)
	}
}

// TestXAIApply_NilThinkingPassthrough verifies a model without thinking support
// passes the body through unchanged.
func TestXAIApply_NilThinkingPassthrough(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"messages":[]}`)
	out, err := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelHigh},
		&registry.ModelInfo{ID: "grok-no-think", Type: "xai"})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if string(out) != string(body) {
		t.Errorf("expected passthrough, got %s", out)
	}
}
