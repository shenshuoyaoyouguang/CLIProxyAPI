package executor

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/tidwall/gjson"
)

func TestApplyOpenAICompatProviderNormalizeRules_RequestByModelKind(t *testing.T) {
	body := []byte(`{
		"thinking":{"type":"enabled"},
		"temperature":0.2,
		"top_p":0.3,
		"messages":[
			{"role":"assistant","content":"plan","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}
		]
	}`)

	out := applyOpenAICompatProviderNormalizeRules(body, openAICompatProviderNormalizeRequest{
		Model: "mimo-v2.5-pro",
	})

	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "plan" {
		t.Fatalf("reasoning_content = %q, want plan; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != mimoThinkingTemperature {
		t.Fatalf("temperature = %v, want %v; body=%s", got, mimoThinkingTemperature, string(out))
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != mimoThinkingTopP {
		t.Fatalf("top_p = %v, want %v; body=%s", got, mimoThinkingTopP, string(out))
	}
}

func TestApplyOpenAICompatProviderNormalizeRules_RequestByCapabilities(t *testing.T) {
	body := []byte(`{
		"model":"strict-upstream",
		"temperature":0.2,
		"top_p":0.3,
		"seed":123,
		"metadata":{"trace":"keep"}
	}`)

	out := applyOpenAICompatProviderNormalizeRules(body, openAICompatProviderNormalizeRequest{
		Model: "strict-upstream",
		CompatModel: &config.OpenAICompatibilityModel{
			UnsupportedParameters: []string{"seed"},
			LockedParameters: map[string]any{
				"temperature": 1.0,
				"top_p":       0.95,
			},
		},
	})

	if gjson.GetBytes(out, "seed").Exists() {
		t.Fatalf("seed should be stripped; body=%s", string(out))
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 1.0 {
		t.Fatalf("temperature = %v, want 1.0; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.95 {
		t.Fatalf("top_p = %v, want 0.95; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "metadata.trace").String(); got != "keep" {
		t.Fatalf("metadata.trace = %q, want keep; body=%s", got, string(out))
	}
}

func TestApplyOpenAICompatProviderNormalizeRules_ResponseUsage(t *testing.T) {
	body := []byte(`{
		"usage":{
			"prompt_tokens":64,
			"completion_tokens":8,
			"total_tokens":72,
			"prompt_cache_hit_tokens":32,
			"prompt_cache_miss_tokens":32
		}
	}`)

	out := applyOpenAICompatProviderNormalizeResponseRules(body, openAICompatProviderNormalizeRequest{
		Model: "deepseek-chat",
	})

	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cached_tokens").Int(); got != 32 {
		t.Fatalf("cached_tokens = %d, want 32; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "usage.input_tokens_details.cached_tokens").Int(); got != 32 {
		t.Fatalf("input cached_tokens = %d, want 32; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "usage.prompt_cache_hit_tokens").Int(); got != 32 {
		t.Fatalf("prompt_cache_hit_tokens = %d, want preserved 32; body=%s", got, string(out))
	}
}
