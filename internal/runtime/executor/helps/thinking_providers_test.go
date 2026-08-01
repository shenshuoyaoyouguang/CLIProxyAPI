package helps

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
)

// TestThinkingProvidersRegistered guards the blank-import list in
// thinking_providers.go: every first-class provider must have its applier
// registered in the production executor environment, otherwise ApplyThinking
// silently passthroughs (e.g. zai was missing and its applier was dead code).
func TestThinkingProvidersRegistered(t *testing.T) {
	for _, p := range []string{"antigravity", "claude", "codex", "deepseek", "gemini", "interactions", "kimi", "nvidia", "openai", "xai", "zai"} {
		if thinking.GetProviderApplier(p) == nil {
			t.Errorf("provider %q applier not registered; is it missing from thinking_providers.go?", p)
		}
	}
}
