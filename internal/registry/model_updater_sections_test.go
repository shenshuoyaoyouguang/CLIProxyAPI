package registry

import "testing"

// TestDetectChangedProviders_IncludesZAIAndNvidia verifies that remote catalog
// refreshes detect changes in the zai and nvidia sections. Without these the
// providers are silently dropped from refresh notifications while deepseek was
// already covered (asymmetric updater support).
func TestDetectChangedProviders_IncludesZAIAndNvidia(t *testing.T) {
	oldData := &staticModelsJSON{
		ZAI:    []*ModelInfo{{ID: "glm-5"}},
		Nvidia: []*ModelInfo{{ID: "nemotron-3-ultra"}},
	}
	newData := &staticModelsJSON{
		ZAI:    []*ModelInfo{{ID: "glm-5"}, {ID: "glm-5.2"}},
		Nvidia: []*ModelInfo{{ID: "nemotron-3-ultra"}},
	}
	changed := detectChangedProviders(oldData, newData)

	foundZAI := false
	foundNvidia := false
	for _, p := range changed {
		if p == "zai" {
			foundZAI = true
		}
		if p == "nvidia" {
			foundNvidia = true
		}
	}
	if !foundZAI {
		t.Fatalf("zai section change not reported, got %v", changed)
	}
	// nvidia did not change: must not be reported spuriously.
	if foundNvidia {
		t.Fatalf("unchanged nvidia section reported as changed: %v", changed)
	}
}

// TestDetectChangedProviders_NvidiaChangeReported covers the nvidia side of the
// same asymmetric-updater gap.
func TestDetectChangedProviders_NvidiaChangeReported(t *testing.T) {
	oldData := &staticModelsJSON{Nvidia: []*ModelInfo{{ID: "nemotron-3"}}}
	newData := &staticModelsJSON{Nvidia: []*ModelInfo{{ID: "nemotron-3-ultra"}}}
	changed := detectChangedProviders(oldData, newData)
	for _, p := range changed {
		if p == "nvidia" {
			return
		}
	}
	t.Fatalf("nvidia section change not reported, got %v", changed)
}

// TestValidateModelsCatalog_AcceptsZAIAndNvidia ensures a catalog carrying the
// zai/nvidia sections passes validation once they become required.
func TestValidateModelsCatalog_AcceptsZAIAndNvidia(t *testing.T) {
	data := &staticModelsJSON{
		Claude: []*ModelInfo{{ID: "claude-x"}},
		Gemini: []*ModelInfo{{ID: "gemini-x"}},
		ZAI:    []*ModelInfo{{ID: "glm-5.2"}},
		Nvidia: []*ModelInfo{{ID: "nemotron-3-ultra"}},
	}
	if err := validateModelsCatalog(data); err != nil {
		t.Fatalf("catalog with zai/nvidia must validate, got: %v", err)
	}
}
