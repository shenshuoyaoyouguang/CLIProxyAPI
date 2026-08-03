package executor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

// captureLogs redirects log output to a buffer during fn execution and returns captured text.
func captureLogs(fn func()) string {
	var buf bytes.Buffer
	orig := log.StandardLogger().Out
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	origLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(origLevel)
	fn()
	return buf.String()
}

// buildNestedMap creates a chain of nested maps from keys descending to leaf.
// Example: buildNestedMap([]string{"a","b","c"}, "leaf") → {"a": {"b": {"c": "leaf"}}}
func buildNestedMap(keys []string, leaf any) map[string]any {
	if len(keys) == 0 {
		return nil
	}
	if len(keys) == 1 {
		return map[string]any{keys[0]: leaf}
	}
	return map[string]any{keys[0]: buildNestedMap(keys[1:], leaf)}
}

// modelID and provider match; otherwise returns nil (unknown model).
func makeModelLookup(modelID, provider string, info *registry.ModelInfo) func(string, ...string) *registry.ModelInfo {
	return func(mID string, prov ...string) *registry.ModelInfo {
		if len(prov) > 0 && mID == modelID && prov[0] == provider {
			return info
		}
		return nil
	}
}

func TestEnhanceModelParams(t *testing.T) {
	modelID := "test-model-enhance"
	provider := "openai-compatibility"

	testModel := &registry.ModelInfo{
		ID:                  modelID,
		Object:              "model",
		Created:             1234567890,
		OwnedBy:             provider,
		Type:                "chat",
		DisplayName:         "Test Model",
		UserDefined:         false,
		Thinking:            &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
		ContextLength:       4096,
		MaxCompletionTokens: 2048,
		ExtraBody: map[string]any{
			"chat_template_kwargs": map[string]any{
				"enable_thinking":   true,
				"thinking_template": "{{thinking}}",
			},
			"flat_key": "flat_value",
		},
	}

	executor := &OpenAICompatExecutor{
		provider:    provider,
		cfg:         &config.Config{},
		modelLookup: makeModelLookup(modelID, provider, testModel),
	}

	t.Run("empty body returns empty", func(t *testing.T) {
		body := []byte("")
		result := executor.enhanceModelParams(body, modelID)
		assert.Empty(t, result)
	})

	t.Run("unknown model returns body unchanged", func(t *testing.T) {
		body := []byte(`{"model": "unknown"}`)
		result := executor.enhanceModelParams(body, "unknown-model")
		assert.JSONEq(t, string(body), string(result))
	})

	t.Run("max_tokens injected when both absent", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance", "messages": [{"role": "user", "content": "hi"}]}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.True(t, gjson.GetBytes(result, "max_tokens").Exists())
		assert.Equal(t, float64(2048), gjson.GetBytes(result, "max_tokens").Float())
	})

	t.Run("max_tokens not injected when max_completion_tokens present", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance", "max_completion_tokens": 100}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.False(t, gjson.GetBytes(result, "max_tokens").Exists())
		assert.Equal(t, float64(100), gjson.GetBytes(result, "max_completion_tokens").Float())
	})

	t.Run("max_tokens not injected when already present", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance", "max_tokens": 500}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.Equal(t, float64(500), gjson.GetBytes(result, "max_tokens").Float())
	})

	t.Run("reasoning_effort NOT injected when thinking capability exists but user did not enable", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance", "messages": [{"role": "user", "content": "hi"}]}`)
		result := executor.enhanceModelParams(body, modelID)
		// enhanceModelParams must not inject reasoning_effort based on model
		// capability alone; ApplyRequestThinking is responsible for setting it
		// when the user explicitly enables thinking.
		assert.False(t, gjson.GetBytes(result, "reasoning_effort").Exists(),
			"reasoning_effort must not be injected when the user has not enabled thinking")
	})

	t.Run("reasoning_effort not injected when already present", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance", "reasoning_effort": "high"}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.Equal(t, "high", gjson.GetBytes(result, "reasoning_effort").String())
	})

	t.Run("extra_body flat key merged when absent", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance"}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.True(t, gjson.GetBytes(result, "extra_body.flat_key").Exists())
		assert.Equal(t, "flat_value", gjson.GetBytes(result, "extra_body.flat_key").String())
	})

	t.Run("extra_body flat key NOT merged when already present", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance", "extra_body": {"flat_key": "user_value"}}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.Equal(t, "user_value", gjson.GetBytes(result, "extra_body.flat_key").String())
	})

	t.Run("extra_body nested object deep merged - leaf level merge", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance", "extra_body": {"chat_template_kwargs": {"thinking_template": "user_template"}}}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.Equal(t, "user_template", gjson.GetBytes(result, "extra_body.chat_template_kwargs.thinking_template").String())
		assert.True(t, gjson.GetBytes(result, "extra_body.chat_template_kwargs.enable_thinking").Exists())
		assert.True(t, gjson.GetBytes(result, "extra_body.chat_template_kwargs.enable_thinking").Bool())
	})

	t.Run("debug log emitted on max_tokens injection", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance", "messages": [{"role": "user", "content": "hi"}]}`)
		logs := captureLogs(func() {
			_ = executor.enhanceModelParams(body, modelID)
		})
		assert.True(t, strings.Contains(logs, "injected default"), "expected debug log about injection, got: %s", logs)
		assert.True(t, strings.Contains(logs, "max_tokens"), "expected max_tokens in log, got: %s", logs)
	})

	t.Run("debug log emitted on extra_body merge", func(t *testing.T) {
		body := []byte(`{"model": "test-model-enhance"}`)
		logs := captureLogs(func() {
			_ = executor.enhanceModelParams(body, modelID)
		})
		assert.True(t, strings.Contains(logs, "merged extra_body"), "expected extra_body merge log, got: %s", logs)
	})
}

func TestEnhanceModelParamsExtraBodyDeepMerge(t *testing.T) {
	modelID := "test-deep-merge"
	provider := "openai-compatibility"

	testModel := &registry.ModelInfo{
		ID:                  modelID,
		Object:              "model",
		Created:             1234567890,
		OwnedBy:             provider,
		Type:                "chat",
		DisplayName:         "Test Deep Merge",
		UserDefined:         false,
		Thinking:            &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
		ContextLength:       4096,
		MaxCompletionTokens: 2048,
		ExtraBody: map[string]any{
			"nested": map[string]any{
				"level1": map[string]any{
					"config_a": "from_config",
					"config_b": "also_from_config",
				},
				"level1_direct": "direct_value",
			},
		},
	}

	executor := &OpenAICompatExecutor{
		provider:    provider,
		cfg:         &config.Config{},
		modelLookup: makeModelLookup(modelID, provider, testModel),
	}

	t.Run("deep merge at leaf level", func(t *testing.T) {
		body := []byte(`{"model": "test-deep-merge", "extra_body": {"nested": {"level1": {"config_a": "user_value"}}}}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.Equal(t, "user_value", gjson.GetBytes(result, "extra_body.nested.level1.config_a").String())
		assert.Equal(t, "also_from_config", gjson.GetBytes(result, "extra_body.nested.level1.config_b").String())
		assert.Equal(t, "direct_value", gjson.GetBytes(result, "extra_body.nested.level1_direct").String())
	})

	t.Run("user object completely overrides when all leaves present", func(t *testing.T) {
		body := []byte(`{"model": "test-deep-merge", "extra_body": {"nested": {"level1": {"config_a": "user_a", "config_b": "user_b"}}}}`)
		result := executor.enhanceModelParams(body, modelID)
		assert.Equal(t, "user_a", gjson.GetBytes(result, "extra_body.nested.level1.config_a").String())
		assert.Equal(t, "user_b", gjson.GetBytes(result, "extra_body.nested.level1.config_b").String())
		assert.Equal(t, "direct_value", gjson.GetBytes(result, "extra_body.nested.level1_direct").String())
	})

	t.Run("no debug log when no extra_body merge", func(t *testing.T) {
		noExtraModelID := "test-no-extra"
		noExtraModel := &registry.ModelInfo{
			ID:       noExtraModelID,
			Object:   "model",
			Created:  1234567890,
			OwnedBy:  provider,
			Type:     "chat",
			Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
		}
		noExtraExec := &OpenAICompatExecutor{
			provider:    provider,
			cfg:         &config.Config{},
			modelLookup: makeModelLookup(noExtraModelID, provider, noExtraModel),
		}
		body := []byte(`{"model": "test-no-extra"}`)
		logs := captureLogs(func() {
			_ = noExtraExec.enhanceModelParams(body, noExtraModelID)
		})
		assert.False(t, strings.Contains(logs, "merged extra_body"), "expected no extra_body log, got: %s", logs)
	})

	t.Run("depth guard stops deep recursion via pre-existing body path", func(t *testing.T) {
		// Config has a 12-level nested path: extra_body.deep.a.b.c...k = "leaf"
		// User body pre-occupies levels a through i at extra_body.deep:
		//   extra_body.deep.a.b.c.d.e.f.g.h.i = {"different": "val"}
		// deepMergeExtraBody must recurse level-by-level because each key
		// already exists as an object. At depth 10 (key "i") it hits the guard
		// and stops, never merging "j" and "k".
		keys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}
		depthModelID := "test-depth-guard"
		depthModel := &registry.ModelInfo{
			ID:       depthModelID,
			Object:   "model",
			Created:  1234567890,
			OwnedBy:  provider,
			Type:     "chat",
			Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
			ExtraBody: map[string]any{
				"deep": buildNestedMap(keys, "leaf"),
			},
		}
		depthExec := &OpenAICompatExecutor{
			provider:    provider,
			cfg:         &config.Config{},
			modelLookup: makeModelLookup(depthModelID, provider, depthModel),
		}
		// User body pre-occupies levels deep.a through deep.i (9 levels)
		bodyPreKeys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
		bodyNested := buildNestedMap(bodyPreKeys, map[string]any{"different": "val"})
		bodyRaw, _ := json.Marshal(map[string]any{
			"model": depthModelID,
			"extra_body": map[string]any{
				"deep": bodyNested,
			},
		})

		logs := captureLogs(func() {
			result := depthExec.enhanceModelParams(bodyRaw, depthModelID)
			// User's leaf at i is preserved
			assert.Equal(t, "val", gjson.GetBytes(result, "extra_body.deep.a.b.c.d.e.f.g.h.i.different").String(),
				"user value at depth 9 must be preserved")
			// Config's j.k was stopped by depth guard — NOT injected
			assert.False(t, gjson.GetBytes(result, "extra_body.deep.a.b.c.d.e.f.g.h.i.j").Exists(),
				"j must NOT be merged (depth guard stopped at i)")
			assert.False(t, gjson.GetBytes(result, "extra_body.deep.a.b.c.d.e.f.g.h.i.j.k").Exists(),
				"k must NOT be merged (depth guard stopped at i)")
		})
		// Warn log should mention max depth reached
		assert.True(t, strings.Contains(logs, "max depth reached"),
			"expected depth guard warn, got: %s", logs)
	})
}
