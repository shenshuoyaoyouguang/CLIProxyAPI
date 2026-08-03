package kimi

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func modelInfo() *registry.ModelInfo {
	return &registry.ModelInfo{
		ID:   "kimi-test",
		Type: "kimi",
		Thinking: &registry.ThinkingSupport{
			Levels: []string{"low", "medium", "high"},
		},
	}
}

// TestApplyEnabledThinkingSetsTypeAndEffort covers applyEnabledThinking: the
// legacy top-level reasoning_effort is removed, thinking.type is set to enabled,
// and the effort lands in thinking.effort via a single re-insert.
func TestApplyEnabledThinkingSetsTypeAndEffort(t *testing.T) {
	body := []byte(`{"thinking":{"keep":true},"reasoning_effort":"high"}`)
	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: "high"}, modelInfo())
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if gjson.GetBytes(got, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort must be removed, got %s", got)
	}
	if tt := gjson.GetBytes(got, "thinking.type").String(); tt != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled", tt)
	}
	if effort := gjson.GetBytes(got, "thinking.effort").String(); effort != "high" {
		t.Fatalf("thinking.effort = %q, want high", effort)
	}
	if !gjson.GetBytes(got, "thinking.keep").Bool() {
		t.Fatalf("existing thinking.keep must be preserved, got %s", got)
	}
}

// TestApplyArrayThinkingReturnsErrorAndKeepsEffortDeletion covers the array
// guard in applyEnabledThinking: array-valued thinking cannot be written, so
// Apply returns an error, and the reasoning_effort deletion made before the
// guard is preserved in the returned body (the caller can still observe the
// mutated state instead of an untouched original).
func TestApplyArrayThinkingReturnsErrorAndKeepsEffortDeletion(t *testing.T) {
	body := []byte(`{"thinking":["x"],"reasoning_effort":"high"}`)
	got, err := NewApplier().Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: "high"}, modelInfo())
	if err == nil {
		t.Fatal("Apply error = nil, want array-thinking error")
	}
	if gjson.GetBytes(got, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort must remain deleted on the error path, got %s", got)
	}
	if tt := gjson.GetBytes(got, "thinking.type").String(); tt != "" {
		t.Fatalf("thinking.type = %q, want unchanged (no write into array)", tt)
	}
}
