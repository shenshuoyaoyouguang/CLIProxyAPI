package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// TestCountTokensMirrorsExecuteDeepSeekReasoningPassback covers issue #6:
// CountTokens must share the same thinking route + ensureDeepSeekReasoningContent
// pipeline as Execute / ExecuteStream so token counts match the upstream body.
//
// Strategy:
//  1. Multi-turn deepseek-reasoner request that triggers passback; capture the
//     body Execute sends upstream.
//  2. Assert assistant reasoning_content was back-filled (empty string).
//  3. CountTokens must succeed with prompt_tokens > 0; deepseek-chat (no
//     passback) still counts successfully as a baseline.
func TestCountTokensMirrorsExecuteDeepSeekReasoningPassback(t *testing.T) {
	var gotExecuteBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 0, r.ContentLength)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			if n > 0 {
				body = append(body, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		gotExecuteBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}

	// Multi-turn: assistant has tool_calls but no reasoning_content → Phase 2 fill.
	// reasoning_effort=low opens the DeepSeek thinking-active gate.
	payload := []byte(`{
			"model": "deepseek-reasoner",
			"reasoning_effort": "low",
			"messages": [
				{"role": "user", "content": "lookup weather"},
				{"role": "assistant", "content": "", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"sf\"}"}}]},
				{"role": "tool", "tool_call_id": "call_1", "content": "sunny"}
			]
		}`)

	// 1. Execute path: upstream must receive back-filled reasoning_content.
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
	executeAssistantRC := gjson.GetBytes(gotExecuteBody, "messages.1.reasoning_content")
	if !executeAssistantRC.Exists() {
		t.Fatalf("Execute body missing reasoning_content on assistant message; body=%s", string(gotExecuteBody))
	}
	if executeAssistantRC.Type == gjson.Null {
		t.Fatalf("Execute body has null reasoning_content; want empty string; body=%s", string(gotExecuteBody))
	}

	// 2. CountTokens path: no error and non-zero count.
	countResp, err := executor.CountTokens(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-reasoner",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("CountTokens error (should mirror Execute pipeline): %v", err)
	}
	promptTokens := gjson.GetBytes(countResp.Payload, "usage.prompt_tokens").Int()
	if promptTokens <= 0 {
		t.Fatalf("CountTokens prompt_tokens = %d, want > 0; payload=%s", promptTokens, string(countResp.Payload))
	}

	// 3. Baseline: deepseek-chat does not trigger passback; CountTokens still works.
	noPassbackPayload := []byte(`{
			"model": "deepseek-chat",
			"messages": [{"role": "user", "content": "hi"}]
		}`)
	_, err = executor.CountTokens(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-chat",
		Payload: noPassbackPayload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("CountTokens (deepseek-chat baseline) error: %v", err)
	}
}
