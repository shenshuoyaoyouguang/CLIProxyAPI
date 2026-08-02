package thinking

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestProviderFamilyClassification(t *testing.T) {
	// A model type is authoritative over the wire format.
	if got := providerFamily(&registry.ModelInfo{Type: "kimi"}, "claude"); got != "kimi" {
		t.Fatalf("providerFamily(kimi-type, claude-wire) = %q, want kimi", got)
	}
	if got := providerFamily(&registry.ModelInfo{Type: "claude"}, "claude"); got != "claude" {
		t.Fatalf("providerFamily(claude-type, claude-wire) = %q, want claude", got)
	}
	// Gemini/antigravity and the OpenAI-compat providers each group into one family.
	if got := providerFamily(&registry.ModelInfo{Type: "gemini"}, "antigravity"); got != "gemini" {
		t.Fatalf("providerFamily(gemini-type, antigravity-wire) = %q, want gemini", got)
	}
	if got := providerFamily(&registry.ModelInfo{Type: "openai"}, "codex"); got != "openai" {
		t.Fatalf("providerFamily(openai-type, codex-wire) = %q, want openai", got)
	}
	// The wire format is the fallback when the model type is unknown.
	if got := providerFamily(nil, "claude"); got != "claude" {
		t.Fatalf("providerFamily(nil, claude-wire) = %q, want claude", got)
	}
}

// TestClaudeWireNonClaudeTypeClampsConsistently asserts that ValidateConfig and
// shouldMapConfiguredHighIntent agree for a non-Claude model served over the
// Claude wire: both clamp/map rather than reject.
func TestClaudeWireNonClaudeTypeClampsConsistently(t *testing.T) {
	levelModel := func(typ string) *registry.ModelInfo {
		return &registry.ModelInfo{
			ID:   "m",
			Type: typ,
			Thinking: &registry.ThinkingSupport{
				Levels: []string{"low", "high"},
			},
		}
	}

	// Non-Claude type on the Claude wire: unsupported "max" clamps to "high"
	// instead of failing, and high intent is mapped.
	kimiModel := levelModel("kimi")
	clamped, err := ValidateConfig(ThinkingConfig{Mode: ModeLevel, Level: "max"}, kimiModel, "claude", "claude", false)
	if err != nil {
		t.Fatalf("ValidateConfig(claude-wire + kimi-type) returned error: %v", err)
	}
	if clamped.Level != "high" {
		t.Fatalf("ValidateConfig clamped level = %q, want high", clamped.Level)
	}
	if !shouldMapConfiguredHighIntent("claude", "claude", kimiModel) {
		t.Fatal("shouldMapConfiguredHighIntent(claude-wire + kimi-type) = false, want true")
	}

	// Claude type on the Claude wire: the same input fails validation (no clamp)
	// and high intent is not mapped.
	claudeModel := levelModel("claude")
	if _, err := ValidateConfig(ThinkingConfig{Mode: ModeLevel, Level: "max"}, claudeModel, "claude", "claude", false); err == nil {
		t.Fatal("ValidateConfig(claude-wire + claude-type) = nil error, want ErrLevelNotSupported")
	}
	if shouldMapConfiguredHighIntent("claude", "claude", claudeModel) {
		t.Fatal("shouldMapConfiguredHighIntent(claude-wire + claude-type) = true, want false")
	}
}
