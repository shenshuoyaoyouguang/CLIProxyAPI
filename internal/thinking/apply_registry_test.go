package thinking

import (
	"testing"
)

func TestRegisterPluginProviderExtractorDispatch(t *testing.T) {
	RegisterPluginProviderExtractor("test-owner", "plugin-x", 1, func(body []byte) ThinkingConfig {
		return ThinkingConfig{Mode: ModeLevel, Level: "high"}
	})
	defer UnregisterPluginProviders("test-owner")

	config := extractThinkingConfig([]byte(`{"reasoning_effort":"medium"}`), "plugin-x")
	if config.Mode != ModeLevel || config.Level != "high" {
		t.Fatalf("extracted config = %+v, want ModeLevel/high from plugin extractor", config)
	}
}

func TestRegisterPluginProviderExtractorConflictSemantics(t *testing.T) {
	// Native providers win over plugin registrations.
	if ok := RegisterPluginProviderExtractor("test-owner", "openai", 1, func(body []byte) ThinkingConfig {
		return ThinkingConfig{Mode: ModeNone, Budget: 0}
	}); ok {
		t.Fatal("RegisterPluginProviderExtractor() over native provider = true, want false")
	}
	if !RegisterPluginProviderExtractor("owner-a", "plugin-y", 2, extractClaudeConfig) {
		t.Fatal("first registration = false, want true")
	}
	if RegisterPluginProviderExtractor("owner-b", "plugin-y", 1, extractClaudeConfig) {
		t.Fatal("lower priority registration = true, want false")
	}
	if RegisterPluginProviderExtractor("owner-a", "plugin-y", 1, extractClaudeConfig) {
		t.Fatal("same priority later owner registration = true, want false")
	}
	defer UnregisterPluginProviders("owner-a")
	defer UnregisterPluginProviders("owner-b")

	if extractor := GetProviderExtractor("plugin-y"); extractor == nil {
		t.Fatal("GetProviderExtractor() = nil, want registered extractor")
	}
}

func TestUnregisterPluginProvidersClearsExtractors(t *testing.T) {
	RegisterPluginProviderExtractor("test-owner", "plugin-z", 1, extractOpenAIConfig)
	if extractor := GetProviderExtractor("plugin-z"); extractor == nil {
		t.Fatal("GetProviderExtractor() = nil, want registered extractor")
	}
	UnregisterPluginProviders("test-owner")
	if extractor := GetProviderExtractor("plugin-z"); extractor != nil {
		t.Fatal("GetProviderExtractor() = non-nil after unregister, want nil")
	}
}
