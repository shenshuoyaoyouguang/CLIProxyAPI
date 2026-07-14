package thinking

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestThinkingValidationFamily_XAIIsNotOpenAICompatible(t *testing.T) {
	openAICompatiblePairs := [][2]string{
		{"openai", "openai-response"},
		{"openai", "codex"},
		{"openai", "deepseek"},
		{"openai", "mimo"},
	}
	for _, pair := range openAICompatiblePairs {
		if !isSameThinkingValidationFamily(pair[0], pair[1]) {
			t.Fatalf("isSameThinkingValidationFamily(%q, %q) = false, want true", pair[0], pair[1])
		}
	}

	xaiPairs := [][2]string{
		{"openai", "xai"},
		{"openai-response", "xai"},
		{"codex", "xai"},
	}
	for _, pair := range xaiPairs {
		if isSameThinkingValidationFamily(pair[0], pair[1]) {
			t.Fatalf("isSameThinkingValidationFamily(%q, %q) = true, want false", pair[0], pair[1])
		}
	}
}

func TestValidateConfig_OpenAIToXAIClampsUnsupportedLevels(t *testing.T) {
	modelInfo := &registry.ModelInfo{
		ID:   "grok-test",
		Type: "xai",
		Thinking: &registry.ThinkingSupport{
			Levels:         []string{"none", "low", "medium", "high"},
			ZeroAllowed:    true,
			DynamicAllowed: false,
		},
	}

	cases := []struct {
		name      string
		from      string
		level     ThinkingLevel
		wantLevel ThinkingLevel
	}{
		{name: "chat xhigh clamps to high", from: "openai", level: LevelXHigh, wantLevel: LevelHigh},
		{name: "responses max clamps to high", from: "openai-response", level: LevelMax, wantLevel: LevelHigh},
		{name: "responses minimal clamps to low", from: "openai-response", level: LevelMinimal, wantLevel: LevelLow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateConfig(
				ThinkingConfig{Mode: ModeLevel, Level: tc.level},
				modelInfo,
				tc.from,
				"xai",
				false,
			)
			if err != nil {
				t.Fatalf("ValidateConfig returned error: %v", err)
			}
			if got == nil {
				t.Fatal("ValidateConfig returned nil config")
			}
			if got.Mode != ModeLevel || got.Level != tc.wantLevel {
				t.Fatalf("ValidateConfig level = (%v, %q), want (%v, %q)", got.Mode, got.Level, ModeLevel, tc.wantLevel)
			}
		})
	}
}

func TestValidateConfig_XAIToXAIKeepsUnsupportedLevelsStrict(t *testing.T) {
	modelInfo := &registry.ModelInfo{
		ID:   "grok-test",
		Type: "xai",
		Thinking: &registry.ThinkingSupport{
			Levels:         []string{"none", "low", "medium", "high"},
			ZeroAllowed:    true,
			DynamicAllowed: false,
		},
	}

	_, err := ValidateConfig(
		ThinkingConfig{Mode: ModeLevel, Level: LevelXHigh},
		modelInfo,
		"xai",
		"xai",
		false,
	)
	if err == nil {
		t.Fatal("ValidateConfig returned nil error, want unsupported level error")
	}
}
