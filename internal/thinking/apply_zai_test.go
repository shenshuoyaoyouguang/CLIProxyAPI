package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"

	// Register the zai provider applier exactly like the production executor
	// path does (helps/thinking_providers.go).
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/zai"
)

// TestApplyThinking_ZaiSuffixApplies verifies that a Z.ai GLM model routed
// through the zai provider applier honors the thinking suffix. The zai applier
// is OpenAI-compatible, so glm-5.2(high) must produce reasoning_effort=high on
// the wire.
func TestApplyThinking_ZaiSuffixApplies(t *testing.T) {
	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`)
	result, err := thinking.ApplyThinking(body, "glm-5.2(high)", "openai", "zai", "zai")
	if err != nil {
		t.Fatalf("ApplyThinking failed: %v", err)
	}
	if got := gjson.GetBytes(result, "reasoning_effort").String(); got != "high" {
		t.Errorf("glm-5.2(high) should set reasoning_effort=high, got %q in %s", got, result)
	}
}
