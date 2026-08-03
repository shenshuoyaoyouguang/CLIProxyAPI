package thinking

import (
	"testing"

	"github.com/tidwall/gjson"
)

// TestFieldNonNil verifies the unified JSON null guard: a present non-null
// value is true, while JSON null and a missing field are both false.
func TestFieldNonNil(t *testing.T) {
	present := gjson.GetBytes([]byte(`{"a":"x"}`), "a")
	if !fieldNonNil(present) {
		t.Error("present field: expected fieldNonNil=true")
	}
	nullF := gjson.GetBytes([]byte(`{"a":null}`), "a")
	if fieldNonNil(nullF) {
		t.Error("JSON null field: expected fieldNonNil=false (gjson.Exists reports true for null)")
	}
	missing := gjson.GetBytes([]byte(`{}`), "a")
	if fieldNonNil(missing) {
		t.Error("missing field: expected fieldNonNil=false")
	}
}

// TestExtractOpenAIConfig_NullReasoningEffort verifies that a JSON null
// reasoning_effort is treated like a missing field (passthrough), not as an
// empty LevelMode. Before the fix, gjson.Exists() returned true for null and
// the empty string fell through to ModeLevel with an empty level.
func TestExtractOpenAIConfig_NullReasoningEffort(t *testing.T) {
	body := []byte(`{"reasoning_effort":null}`)
	cfg := extractOpenAIConfig(body)
	if hasThinkingConfig(cfg) {
		t.Errorf("reasoning_effort=null: expected passthrough (empty config), got Mode=%v Level=%q Budget=%d", cfg.Mode, cfg.Level, cfg.Budget)
	}
}

// TestExtractCodexConfig_NullReasoningEffort mirrors the OpenAI case for the
// nested Codex/Responses-API field reasoning.effort.
func TestExtractCodexConfig_NullReasoningEffort(t *testing.T) {
	body := []byte(`{"reasoning":{"effort":null}}`)
	cfg := extractCodexConfig(body)
	if hasThinkingConfig(cfg) {
		t.Errorf("reasoning.effort=null: expected passthrough (empty config), got Mode=%v Level=%q Budget=%d", cfg.Mode, cfg.Level, cfg.Budget)
	}
}

// TestExtractClaudeConfig_NullBudgetTokensWithEnabled verifies that a JSON
// null budget_tokens under thinking.type=enabled is treated as "no budget
// specified" (ModeAuto), not as disabled (ModeNone). Before the fix the null
// was read as 0 and silently disabled thinking.
func TestExtractClaudeConfig_NullBudgetTokensWithEnabled(t *testing.T) {
	body := []byte(`{"thinking":{"type":"enabled","budget_tokens":null}}`)
	cfg := extractClaudeConfig(body)
	if cfg.Mode != ModeAuto {
		t.Errorf("thinking.type=enabled + budget_tokens=null: expected ModeAuto, got Mode=%v", cfg.Mode)
	}
}

// TestExtractClaudeConfig_NullBudgetTokensNoType verifies that a JSON null
// budget_tokens with no thinking.type is treated like a missing field
// (passthrough), not as disabled.
func TestExtractClaudeConfig_NullBudgetTokensNoType(t *testing.T) {
	body := []byte(`{"thinking":{"budget_tokens":null}}`)
	cfg := extractClaudeConfig(body)
	if hasThinkingConfig(cfg) {
		t.Errorf("budget_tokens=null without type: expected passthrough (empty config), got Mode=%v Level=%q", cfg.Mode, cfg.Level)
	}
}

// TestExtractGeminiConfig_NullThinkingLevel verifies that a JSON null
// thinkingLevel is treated like a missing field (passthrough), not as an
// empty ModeLevel.
func TestExtractGeminiConfig_NullThinkingLevel(t *testing.T) {
	body := []byte(`{"generationConfig":{"thinkingConfig":{"thinkingLevel":null}}}`)
	cfg := extractGeminiConfig(body, "gemini")
	if hasThinkingConfig(cfg) {
		t.Errorf("thinkingLevel=null: expected passthrough (empty config), got Mode=%v Level=%q", cfg.Mode, cfg.Level)
	}
}

// TestExtractGeminiConfig_NullThinkingBudget verifies that a JSON null
// thinkingBudget is treated like a missing field (passthrough), not as
// disabled (ModeNone via Int()==0).
func TestExtractGeminiConfig_NullThinkingBudget(t *testing.T) {
	body := []byte(`{"generationConfig":{"thinkingConfig":{"thinkingBudget":null}}}`)
	cfg := extractGeminiConfig(body, "gemini")
	if hasThinkingConfig(cfg) {
		t.Errorf("thinkingBudget=null: expected passthrough (empty config), got Mode=%v Level=%q Budget=%d", cfg.Mode, cfg.Level, cfg.Budget)
	}
}
