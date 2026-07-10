package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestNormalizeDeepSeekToolMessageReasoning_NoToolCallsUnchanged(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"hi there"}
		]
	}`)

	out := normalizeDeepSeekToolMessageReasoning(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body should be unchanged when no tool_calls present, got %s", string(out))
	}
}

func TestNormalizeDeepSeekToolMessageReasoning_UsesReasoningField(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","reasoning":"my reasoning trace","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeDeepSeekToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "my reasoning trace" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "my reasoning trace")
	}
}

func TestNormalizeDeepSeekToolMessageReasoning_ExistingReasoningUnchanged(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":"keep me"}
		]
	}`)

	out := normalizeDeepSeekToolMessageReasoning(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body should be unchanged when reasoning_content already present, got %s", string(out))
	}
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "keep me" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "keep me")
	}
}

func TestNormalizeDeepSeekToolMessageReasoning_UsesContentStringFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"assistant summary","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeDeepSeekToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "assistant summary" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "assistant summary")
	}
}

func TestNormalizeDeepSeekToolMessageReasoning_UsesPreviousReasoningFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"plan","reasoning_content":"previous reasoning"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeDeepSeekToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.1.reasoning_content").String(); got != "previous reasoning" {
		t.Fatalf("messages.1.reasoning_content = %q, want %q", got, "previous reasoning")
	}
}

func TestNormalizeDeepSeekToolMessageReasoning_InterveningAssistantClearsPreviousReasoningFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"plan","reasoning_content":"previous reasoning"},
			{"role":"assistant","content":"plain follow-up"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeDeepSeekToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.2.reasoning_content").String(); got != "[reasoning unavailable]" {
		t.Fatalf("messages.2.reasoning_content = %q, want %q", got, "[reasoning unavailable]")
	}
}

func TestNormalizeDeepSeekToolMessageReasoning_DoesNotTouchToolMessages(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","reasoning":"planning","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"[]"}
		]
	}`)

	out := normalizeDeepSeekToolMessageReasoning(body)
	if gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("tool message must not receive reasoning_content")
	}
}

func TestNormalizeDeepSeekToolMessageReasoning_WhitespaceReasoningContentReplaced(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"backfill","tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}],"reasoning_content":"   \t  "}
		]
	}`)

	out := normalizeDeepSeekToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "backfill" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "backfill")
	}
}

func TestOpenAICompatExecutor_DeepSeekReasoningContentPassback(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}

	payload := []byte(`{
		"model":"deepseek-reasoner",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"plan","reasoning_content":"previous reasoning"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-reasoner",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if got := gjson.GetBytes(gotBody, "messages.2.reasoning_content").String(); got != "previous reasoning" {
		t.Fatalf("messages.2.reasoning_content = %q, want %q; body=%s", got, "previous reasoning", string(gotBody))
	}
}

func TestOpenAICompatExecutor_DeepSeekCacheUsageNormalizedForOpenAIResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":64,"completion_tokens":8,"total_tokens":72,"prompt_cache_hit_tokens":32,"prompt_cache_miss_tokens":32}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}

	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-chat",
		Payload: []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat:   sdktranslator.FromString("openai"),
		ResponseFormat: sdktranslator.FromString("openai-response"),
		Stream:         false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if got := gjson.GetBytes(resp.Payload, "usage.input_tokens_details.cached_tokens").Int(); got != 32 {
		t.Fatalf("usage.input_tokens_details.cached_tokens = %d, want 32; payload=%s", got, string(resp.Payload))
	}
}
