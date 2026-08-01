package registry

import "testing"

func TestModelOverrideHeadersFromEmbeddedModels(t *testing.T) {
	const wantUA = "codex-tui/0.144.0 (Mac OS 26.5.1; arm64) iTerm.app/3.6.11 (codex-tui; 0.144.0)"
	got := ModelOverrideHeaders("gpt-5.6-luna")
	if got == nil {
		t.Fatal("ModelOverrideHeaders(gpt-5.6-luna) = nil, want headers")
	}
	if got["user-agent"] != wantUA {
		t.Fatalf("user-agent = %q, want %q", got["user-agent"], wantUA)
	}
	if got := ModelOverrideHeaders("gpt-5.4"); got != nil {
		t.Fatalf("ModelOverrideHeaders(gpt-5.4) = %#v, want nil", got)
	}
}

func TestGeminiVertexModelsUseFlashLiteReleaseID(t *testing.T) {
	const releaseID = "gemini-3.1-flash-lite"
	const previewID = releaseID + "-preview"

	for _, model := range GetGeminiVertexModels() {
		if model == nil {
			continue
		}
		if model.ID == previewID {
			t.Fatalf("Vertex model ID = %q, want release ID %q", model.ID, releaseID)
		}
		if model.ID == releaseID {
			return
		}
	}

	t.Fatalf("Vertex models do not contain %q", releaseID)
}

func TestWithXAIBuiltinsIncludesVideoPreviewModel(t *testing.T) {
	models := WithXAIBuiltins(nil)

	for _, model := range models {
		if model == nil {
			continue
		}
		if model.ID == xaiBuiltinVideo15PreviewModelID {
			return
		}
	}

	t.Fatalf("expected xAI builtin model %s", xaiBuiltinVideo15PreviewModelID)
}

func TestGetZAIModelsIncludesGLM52(t *testing.T) {
	models := GetZAIModels()

	for _, model := range models {
		if model == nil {
			continue
		}
		if model.ID == "glm-5.2" {
			return
		}
	}

	t.Fatalf("expected Z.ai model glm-5.2 in GetZAIModels")
}

func TestAntigravityWebSearchModelForRequiresRequestedModelCapability(t *testing.T) {
	registryRef := GetGlobalRegistry()
	registryRef.RegisterClient("test-antigravity-websearch-route", "antigravity", []*ModelInfo{
		{ID: "gemini-route-test"},
		{ID: "gemini-web-search-test", SupportsWebSearch: true},
	})
	registryRef.RegisterClient("test-gemini-websearch-route", "gemini", []*ModelInfo{
		{ID: "gemini-cross-provider-route"},
		{ID: "gemini-cross-provider-search", SupportsWebSearch: true},
	})
	t.Cleanup(func() {
		registryRef.UnregisterClient("test-antigravity-websearch-route")
		registryRef.UnregisterClient("test-gemini-websearch-route")
	})

	if got := AntigravityWebSearchModelFor("gemini-route-test"); got != "" {
		t.Fatalf("route model without web search support should not get fallback model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-route-test(high)"); got != "" {
		t.Fatalf("suffix route model without web search support should not get fallback model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-web-search-test"); got != "gemini-web-search-test" {
		t.Fatalf("AntigravityWebSearchModelFor capable model = %q, want itself", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-cross-provider-route"); got != "" {
		t.Fatalf("cross-provider model should not get Antigravity web search model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("unknown-model"); got != "" {
		t.Fatalf("unknown model should not get Antigravity web search model, got %q", got)
	}
}

// TestDeepSeekModelsDeserializedFromCatalog verifies that the "deepseek"
// section of models.json is deserialized and retrievable (issue #1).
func TestDeepSeekModelsDeserializedFromCatalog(t *testing.T) {
	models := GetDeepSeekModels()
	if len(models) == 0 {
		t.Fatal("GetDeepSeekModels returned empty slice; deepseek section not deserialized")
	}

	seen := make(map[string]bool, len(models))
	for _, m := range models {
		if m == nil {
			continue
		}
		seen[m.ID] = true
	}
	for _, want := range []string{"deepseek-v4-pro", "deepseek-v4-flash"} {
		if !seen[want] {
			t.Errorf("deepseek models missing %q", want)
		}
	}
}

// TestDeepSeekModelsAvailableViaChannel verifies GetStaticModelDefinitionsByChannel
// returns DeepSeek models for the "deepseek" channel (issue #1).
func TestDeepSeekModelsAvailableViaChannel(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("deepseek")
	if len(models) == 0 {
		t.Fatal(`GetStaticModelDefinitionsByChannel("deepseek") returned empty slice`)
	}
}

// TestLookupStaticModelInfoFindsDeepSeek verifies LookupStaticModelInfo
// searches the DeepSeek section (issue #1).
func TestLookupStaticModelInfoFindsDeepSeek(t *testing.T) {
	info := LookupStaticModelInfo("deepseek-v4-pro")
	if info == nil {
		t.Fatal("LookupStaticModelInfo(deepseek-v4-pro) = nil; deepseek section not searched")
	}
	if info.ID != "deepseek-v4-pro" {
		t.Errorf("LookupStaticModelInfo returned ID %q, want deepseek-v4-pro", info.ID)
	}
}

// TestDeepSeekV4ProRetainsThinkingBlock verifies that thinking-capable
// DeepSeek models still carry thinking metadata (regression guard for #14).
func TestDeepSeekV4ProRetainsThinkingBlock(t *testing.T) {
	info := LookupStaticModelInfo("deepseek-v4-pro")
	if info == nil {
		t.Fatal("LookupStaticModelInfo(deepseek-v4-pro) = nil")
	}
	if info.Thinking == nil {
		t.Error("deepseek-v4-pro Thinking = nil, want non-nil (thinking-capable model)")
	}
}
