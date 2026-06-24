package thinking

import "testing"

// TestExtractOpenAIConfig_AutoReasoningEffort verifies the newly-added "auto"
// case in extractOpenAIConfig: reasoning_effort="auto" must produce ModeAuto
// with Budget=-1.
func TestExtractOpenAIConfig_AutoReasoningEffort(t *testing.T) {
	body := []byte(`{"reasoning_effort":"auto"}`)
	got := extractOpenAIConfig(body)
	if got.Mode != ModeAuto {
		t.Errorf("Mode = %v, want ModeAuto", got.Mode)
	}
	if got.Budget != -1 {
		t.Errorf("Budget = %d, want -1", got.Budget)
	}
}

// TestExtractOpenAIConfig_NoneReasoningEffort verifies the "none" case.
func TestExtractOpenAIConfig_NoneReasoningEffort(t *testing.T) {
	body := []byte(`{"reasoning_effort":"none"}`)
	got := extractOpenAIConfig(body)
	if got.Mode != ModeNone {
		t.Errorf("Mode = %v, want ModeNone", got.Mode)
	}
	if got.Budget != 0 {
		t.Errorf("Budget = %d, want 0", got.Budget)
	}
}

// TestExtractOpenAIConfig_LevelValues verifies that arbitrary level strings are
// returned as ModeLevel.
func TestExtractOpenAIConfig_LevelValues(t *testing.T) {
	tests := []struct {
		name      string
		effort    string
		wantLevel ThinkingLevel
	}{
		{name: "low", effort: "low", wantLevel: "low"},
		{name: "medium", effort: "medium", wantLevel: "medium"},
		{name: "high", effort: "high", wantLevel: "high"},
		{name: "unknown level is still ModeLevel", effort: "custom", wantLevel: "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"reasoning_effort":"` + tt.effort + `"}`)
			got := extractOpenAIConfig(body)
			if got.Mode != ModeLevel {
				t.Errorf("Mode = %v, want ModeLevel", got.Mode)
			}
			if got.Level != tt.wantLevel {
				t.Errorf("Level = %q, want %q", got.Level, tt.wantLevel)
			}
		})
	}
}

// TestExtractOpenAIConfig_MissingField verifies an empty ThinkingConfig is
// returned when the body has no reasoning_effort field.
func TestExtractOpenAIConfig_MissingField(t *testing.T) {
	body := []byte(`{"model":"gpt-4","messages":[]}`)
	got := extractOpenAIConfig(body)
	if hasThinkingConfig(got) {
		t.Errorf("extractOpenAIConfig() returned non-empty config for body without reasoning_effort: %+v", got)
	}
}

// TestExtractThinkingConfig_DeepseekUsesOpenAIFormat verifies that the "deepseek"
// provider is handled identically to "kimi" — both use the OpenAI-compatible
// reasoning_effort field.
func TestExtractThinkingConfig_DeepseekUsesOpenAIFormat(t *testing.T) {
	tests := []struct {
		name       string
		body       []byte
		wantMode   ThinkingMode
		wantBudget int
		wantLevel  ThinkingLevel
	}{
		{
			name:       "deepseek auto",
			body:       []byte(`{"reasoning_effort":"auto"}`),
			wantMode:   ModeAuto,
			wantBudget: -1,
			wantLevel:  "",
		},
		{
			name:       "deepseek none",
			body:       []byte(`{"reasoning_effort":"none"}`),
			wantMode:   ModeNone,
			wantBudget: 0,
			wantLevel:  "",
		},
		{
			name:       "deepseek high",
			body:       []byte(`{"reasoning_effort":"high"}`),
			wantMode:   ModeLevel,
			wantBudget: 0,
			wantLevel:  "high",
		},
		{
			name: "deepseek missing reasoning_effort returns empty",
			body: []byte(`{"model":"deepseek-v3","messages":[]}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractThinkingConfig(tt.body, "deepseek")
			if got.Mode != tt.wantMode {
				t.Errorf("Mode = %v, want %v", got.Mode, tt.wantMode)
			}
			if got.Budget != tt.wantBudget {
				t.Errorf("Budget = %d, want %d", got.Budget, tt.wantBudget)
			}
			if got.Level != tt.wantLevel {
				t.Errorf("Level = %q, want %q", got.Level, tt.wantLevel)
			}
		})
	}
}

// TestExtractThinkingConfig_DeepseekAndKimiAreEquivalent verifies that "deepseek"
// and "kimi" produce the same result for identical bodies, since both delegate to
// extractOpenAIConfig.
func TestExtractThinkingConfig_DeepseekAndKimiAreEquivalent(t *testing.T) {
	bodies := [][]byte{
		[]byte(`{"reasoning_effort":"auto"}`),
		[]byte(`{"reasoning_effort":"none"}`),
		[]byte(`{"reasoning_effort":"high"}`),
		[]byte(`{"model":"test"}`),
	}
	for _, body := range bodies {
		deepseekCfg := extractThinkingConfig(body, "deepseek")
		kimiCfg := extractThinkingConfig(body, "kimi")
		if deepseekCfg != kimiCfg {
			t.Errorf("deepseek and kimi configs differ for body %s: deepseek=%+v kimi=%+v",
				string(body), deepseekCfg, kimiCfg)
		}
	}
}

// TestExtractCodexConfig_Auto verifies that extractCodexConfig correctly parses
// reasoning.effort="auto" as ModeAuto with Budget=-1, matching extractOpenAIConfig.
func TestExtractCodexConfig_Auto(t *testing.T) {
	body := []byte(`{"reasoning":{"effort":"auto"}}`)
	got := extractCodexConfig(body)
	if got.Mode != ModeAuto {
		t.Errorf("Mode = %v, want ModeAuto", got.Mode)
	}
	if got.Budget != -1 {
		t.Errorf("Budget = %d, want -1", got.Budget)
	}
}

// TestExtractCodexConfig_None verifies the "none" case.
func TestExtractCodexConfig_None(t *testing.T) {
	body := []byte(`{"reasoning":{"effort":"none"}}`)
	got := extractCodexConfig(body)
	if got.Mode != ModeNone {
		t.Errorf("Mode = %v, want ModeNone", got.Mode)
	}
	if got.Budget != 0 {
		t.Errorf("Budget = %d, want 0", got.Budget)
	}
}

// TestExtractCodexConfig_Level verifies that known level strings are ModeLevel.
func TestExtractCodexConfig_Level(t *testing.T) {
	for _, level := range []string{"low", "medium", "high"} {
		t.Run(level, func(t *testing.T) {
			body := []byte(`{"reasoning":{"effort":"` + level + `"}}`)
			got := extractCodexConfig(body)
			if got.Mode != ModeLevel {
				t.Errorf("Mode = %v, want ModeLevel", got.Mode)
			}
			if got.Level != ThinkingLevel(level) {
				t.Errorf("Level = %q, want %q", got.Level, level)
			}
		})
	}
}

// TestExtractCodexConfig_Missing verifies an empty ThinkingConfig is returned
// when the body has no reasoning.effort field.
func TestExtractCodexConfig_Missing(t *testing.T) {
	body := []byte(`{"model":"codex-mini","messages":[]}`)
	got := extractCodexConfig(body)
	if hasThinkingConfig(got) {
		t.Errorf("extractCodexConfig() returned non-empty config for body without reasoning.effort: %+v", got)
	}
}

// TestExtractThinkingConfig_CodexAndXAIAreEquivalent verifies that "codex" and
// "xai" produce the same result for identical bodies, since both delegate to
// extractCodexConfig.
func TestExtractThinkingConfig_CodexAndXAIAreEquivalent(t *testing.T) {
	bodies := [][]byte{
		[]byte(`{"reasoning":{"effort":"auto"}}`),
		[]byte(`{"reasoning":{"effort":"none"}}`),
		[]byte(`{"reasoning":{"effort":"high"}}`),
		[]byte(`{"model":"test"}`),
	}
	for _, body := range bodies {
		codexCfg := extractThinkingConfig(body, "codex")
		xaiCfg := extractThinkingConfig(body, "xai")
		if codexCfg != xaiCfg {
			t.Errorf("codex and xai configs differ for body %s: codex=%+v xai=%+v",
				string(body), codexCfg, xaiCfg)
		}
	}
}

// TestExtractThinkingConfig_UnknownProviderReturnsEmpty verifies that an unknown
// provider produces an empty config.
func TestExtractThinkingConfig_UnknownProviderReturnsEmpty(t *testing.T) {
	body := []byte(`{"reasoning_effort":"high"}`)
	got := extractThinkingConfig(body, "unknown-provider")
	if hasThinkingConfig(got) {
		t.Errorf("unknown provider should return empty config, got %+v", got)
	}
}
