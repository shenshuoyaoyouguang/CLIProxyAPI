package executor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/ratelimit"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

type openAICompatRewriteRoundTripper struct {
	target *url.URL
}

func (rt openAICompatRewriteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestOpenAICompatExecutorCompactPassthrough(t *testing.T) {
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response.compaction","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	payload := []byte(`{"model":"gpt-5.1-codex-max","input":[{"role":"user","content":"hi"}]}`)
	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5.1-codex-max",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Alt:          "responses/compact",
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/v1/responses/compact" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/responses/compact")
	}
	if !gjson.GetBytes(gotBody, "input").Exists() {
		t.Fatalf("expected input in body")
	}
	if gjson.GetBytes(gotBody, "messages").Exists() {
		t.Fatalf("unexpected messages in body")
	}
	if string(resp.Payload) != `{"id":"resp_1","object":"response.compaction","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}` {
		t.Fatalf("payload = %s", string(resp.Payload))
	}
}

func TestOpenAICompatExecutorPayloadOverrideWinsOverThinkingSuffix(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		Payload: config.PayloadConfig{
			Override: []config.PayloadRule{
				{
					Models: []config.PayloadModelRule{
						{Name: "custom-openai", Protocol: "openai"},
					},
					Params: map[string]any{
						"reasoning_effort": "low",
					},
				},
			},
		},
	})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	payload := []byte(`{"model":"custom-openai(high)","messages":[{"role":"user","content":"hi"}]}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "custom-openai(high)",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := gjson.GetBytes(gotBody, "reasoning_effort").String(); got != "low" {
		t.Fatalf("reasoning_effort = %q, want %q; body=%s", got, "low", string(gotBody))
	}
}

func TestOpenAICompatExecutorDeepSeekInjectsStableUser(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	authA := &cliproxyauth.Auth{
		ID: "deepseek-auth-a",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "key-a",
		},
	}
	authB := &cliproxyauth.Auth{
		ID: "deepseek-auth-b",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "key-b",
		},
	}
	payload := []byte(`{"model":"deepseek-r1","messages":[{"role":"user","content":"hi"}]}`)
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: false}

	if _, err := executor.Execute(context.Background(), authA, cliproxyexecutor.Request{Model: "deepseek-r1", Payload: payload}, opts); err != nil {
		t.Fatalf("execute A: %v", err)
	}
	if _, err := executor.Execute(context.Background(), authA, cliproxyexecutor.Request{Model: "deepseek-r1", Payload: payload}, opts); err != nil {
		t.Fatalf("execute A again: %v", err)
	}
	if _, err := executor.Execute(context.Background(), authB, cliproxyexecutor.Request{Model: "deepseek-r1", Payload: payload}, opts); err != nil {
		t.Fatalf("execute B: %v", err)
	}

	if len(bodies) != 3 {
		t.Fatalf("got %d bodies, want 3", len(bodies))
	}
	userA1 := gjson.GetBytes(bodies[0], "user").String()
	userA2 := gjson.GetBytes(bodies[1], "user").String()
	userB := gjson.GetBytes(bodies[2], "user").String()
	if userA1 == "" || !strings.HasPrefix(userA1, "cliproxy-") {
		t.Fatalf("user A = %q, want cliproxy- prefix", userA1)
	}
	if userA1 != userA2 {
		t.Fatalf("same auth user must be stable: %q vs %q", userA1, userA2)
	}
	if userA1 == userB {
		t.Fatalf("different auth keys must produce different user values: %q", userA1)
	}
}

func TestOpenAICompatExecutorDeepSeekPreservesClientUser(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	// Use an auth with an ID so deepSeekUserID returns a non-empty value.
	// This ensures the test verifies that the existing client "user" field
	// is preserved even when an injectable userID is available, rather than
	// passing simply because no userID could be generated.
	auth := &cliproxyauth.Auth{
		ID: "deepseek-auth-preserve",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "test",
		},
	}
	payload := []byte(`{"model":"deepseek-r1","user":"client-user","messages":[{"role":"user","content":"hi"}]}`)
	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-r1",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := gjson.GetBytes(gotBody, "user").String(); got != "client-user" {
		t.Fatalf("user = %q, want client-user; body=%s", got, gotBody)
	}
}

func TestOpenAICompatExecutorDeepSeekGatewayOverridesClientUser(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	targetURL, errParse := url.Parse(server.URL)
	if errParse != nil {
		t.Fatalf("parse server URL: %v", errParse)
	}
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", openAICompatRewriteRoundTripper{target: targetURL})
	mgr := ratelimit.NewDeepSeekLimiterManager(ratelimit.DeepSeekLimiterConfig{
		GlobalMaxConcurrency:    1,
		PerUserIDMaxConcurrency: 1,
	})
	hook := ratelimit.NewDeepSeekGatewayHook(config.DeepSeekGatewayConfig{Enabled: true}, mgr, ratelimit.NewUserIDResolver(ratelimit.StrategyFixed, "", "gateway-user", ""))
	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{}).WithDeepSeekGatewayHook(hook)
	auth := &cliproxyauth.Auth{
		ID: "deepseek-auth-gateway",
		Attributes: map[string]string{
			"base_url": "https://api.deepseek.com/v1",
			"api_key":  "test",
		},
	}
	payload := []byte(`{"model":"deepseek-r1","user":"client-user","messages":[{"role":"user","content":"hi"}]}`)
	if _, errExecute := executor.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "deepseek-r1",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	}); errExecute != nil {
		t.Fatalf("Execute error: %v", errExecute)
	}
	if got := gjson.GetBytes(gotBody, "user").String(); got != "gateway-user" {
		t.Fatalf("user = %q, want gateway-user; body=%s", got, gotBody)
	}
}

func TestOpenAICompatExecutorDeepSeekGatewayStreamHoldsSlotUntilStreamEnds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	targetURL, errParse := url.Parse(server.URL)
	if errParse != nil {
		t.Fatalf("parse server URL: %v", errParse)
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "cliproxy.roundtripper", openAICompatRewriteRoundTripper{target: targetURL}))
	defer cancel()

	mgr := ratelimit.NewDeepSeekLimiterManager(ratelimit.DeepSeekLimiterConfig{
		GlobalMaxConcurrency:    1,
		PerUserIDMaxConcurrency: 1,
	})
	hook := ratelimit.NewDeepSeekGatewayHook(config.DeepSeekGatewayConfig{Enabled: true}, mgr, ratelimit.NewUserIDResolver(ratelimit.StrategyFixed, "", "gateway-user", ""))
	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{}).WithDeepSeekGatewayHook(hook)
	auth := &cliproxyauth.Auth{
		ID: "deepseek-auth-gateway-stream",
		Attributes: map[string]string{
			"base_url": "https://api.deepseek.com/v1",
			"api_key":  "test",
		},
	}
	payload := []byte(`{"model":"deepseek-r1","messages":[{"role":"user","content":"hi"}]}`)
	result, errExecute := executor.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "deepseek-r1",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       true,
	})
	if errExecute != nil {
		t.Fatalf("ExecuteStream error: %v", errExecute)
	}
	if result == nil || result.Chunks == nil {
		t.Fatalf("ExecuteStream returned nil result")
	}

	verifyStats := func(expectedActive int, msg string) {
		deadline := time.Now().Add(time.Second)
		for {
			stats, ok := mgr.GetShardStats("gateway-user")
			if ok && stats.Active == expectedActive {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: expected Active=%d, got stats=%+v ok=%v", msg, expectedActive, stats, ok)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	verifyStats(1, "slot must remain held while stream is active")

	cancel()

	verifyStats(0, "slot must be released after stream ends")
}

func TestOpenAICompatExecutorNonDeepSeekSkipsUserInjection(t *testing.T) {
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
	payload := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-4o-mini",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gjson.GetBytes(gotBody, "user").Exists() {
		t.Fatalf("non-DeepSeek requests must not inject user: %s", gotBody)
	}
}

func TestOpenAICompatExecutorDeepSeekThinkingAndHistoryFilter(t *testing.T) {
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
		"model":"deepseek-r1",
		"thinking":{"type":"enabled","effort":"max"},
		"messages":[
			{"role":"user","content":"q1"},
			{"role":"assistant","content":"a1","reasoning_content":"plain thought","tool_calls":[]},
			{"role":"user","content":"q2"},
			{"role":"assistant","content":"","reasoning_content":"tool thought","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"result"},
			{"role":"user","content":"q3"}
		]
	}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-r1",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gjson.GetBytes(gotBody, "thinking").Exists() {
		t.Fatalf("native thinking object must be translated away: %s", gotBody)
	}
	if got := gjson.GetBytes(gotBody, "reasoning_effort").String(); got != "max" {
		t.Fatalf("reasoning_effort = %q, want max. body=%s", got, gotBody)
	}
	if gjson.GetBytes(gotBody, "messages.1.reasoning_content").Exists() {
		t.Fatalf("plain assistant reasoning_content must be stripped: %s", gotBody)
	}
	if got := gjson.GetBytes(gotBody, "messages.3.reasoning_content").String(); got != "tool thought" {
		t.Fatalf("tool assistant reasoning_content = %q, want tool thought. body=%s", got, gotBody)
	}
}

func TestOpenAICompatExecutorDeepSeekNormalizesNestedFunctionToolChoice(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_number","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	payload := []byte(`{
		"model":"DeepSeek-V4-Flash",
		"messages":[{"role":"user","content":"call lookup"}],
		"tools":[{"type":"function","function":{"name":"lookup_number","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"lookup_number"}}
	}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "DeepSeek-V4-Flash",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := gjson.GetBytes(gotBody, "tool_choice.type").String(); got != "function" {
		t.Fatalf("tool_choice.type = %q, want function. body=%s", got, gotBody)
	}
	// DeepSeek expects the standard OpenAI nested format: tool_choice.function.name
	// 验证嵌套格式：{"type":"function","function":{"name":"lookup_number"}}
	if got := gjson.GetBytes(gotBody, "tool_choice.function.name").String(); got != "lookup_number" {
		t.Fatalf("tool_choice.function.name = %q, want lookup_number (nested form per DeepSeek API docs). body=%s", got, gotBody)
	}
	// 扁平化字段不应存在
	if gjson.GetBytes(gotBody, "tool_choice.name").Exists() {
		t.Fatalf("tool_choice.name must NOT exist at top level; DeepSeek requires nested function.name. body=%s", gotBody)
	}
	// function 嵌套对象必须保留
	if !gjson.GetBytes(gotBody, "tool_choice.function").Exists() {
		t.Fatalf("tool_choice.function must exist (nested form per DeepSeek API docs). body=%s", gotBody)
	}
}

// TestOpenAICompatExecutorDeepSeekPreservesNestedToolChoice 验证 DeepSeek
// tool_choice 保持标准 OpenAI 嵌套格式，符合 DeepSeek 官方 API 文档要求。
//
// 官方文档格式（https://api-docs.deepseek.com/api/create-chat-completion）：
//
//	{"type": "function", "function": {"name": "my_function"}}
//
// 扁平化格式 {"type":"function","name":"x"} 不被 DeepSeek 接受。
func TestOpenAICompatExecutorDeepSeekPreservesNestedToolChoice(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_number","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	payload := []byte(`{
		"model":"DeepSeek-V4-Flash",
		"messages":[{"role":"user","content":"call lookup"}],
		"tools":[{"type":"function","function":{"name":"lookup_number","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"lookup_number"}}
	}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "DeepSeek-V4-Flash",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// 验证 tool_choice 保持嵌套格式：{"type":"function","function":{"name":"lookup_number"}}
	if got := gjson.GetBytes(gotBody, "tool_choice.type").String(); got != "function" {
		t.Fatalf("tool_choice.type = %q, want function. body=%s", got, gotBody)
	}
	if got := gjson.GetBytes(gotBody, "tool_choice.function.name").String(); got != "lookup_number" {
		t.Fatalf("tool_choice.function.name = %q, want lookup_number (nested form per DeepSeek API docs). body=%s", got, gotBody)
	}

	// 扁平化字段不应存在 —— DeepSeek 不接受 {"type":"function","name":"x"}
	if gjson.GetBytes(gotBody, "tool_choice.name").Exists() {
		t.Fatalf("tool_choice.name must NOT exist at top level; DeepSeek requires nested function.name. body=%s", gotBody)
	}

	// function 嵌套对象必须保留
	if !gjson.GetBytes(gotBody, "tool_choice.function").Exists() {
		t.Fatalf("tool_choice.function must exist (nested form per DeepSeek API docs). body=%s", gotBody)
	}
}

func TestOpenAICompatExecutorImagesGenerationsPassthrough(t *testing.T) {
	var gotPath string
	var gotBody []byte
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":123,"data":[{"b64_json":"AA=="}],"usage":{"total_tokens":1}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "upstream-image",
		Payload: []byte(`{"model":"compat-image","prompt":"draw"}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-image"),
		Stream:       false,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Metadata: map[string]any{
			cliproxyexecutor.RequestPathMetadataKey: "/v1/images/generations",
		},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/v1/images/generations" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/images/generations")
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", gotContentType)
	}
	if got := gjson.GetBytes(gotBody, "model").String(); got != "upstream-image" {
		t.Fatalf("model = %q, want upstream-image; body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(resp.Payload, "data.0.b64_json").String(); got != "AA==" {
		t.Fatalf("response payload = %s", string(resp.Payload))
	}
}

func TestOpenAICompatExecutorImagesGenerationsStreamsUpstream(t *testing.T) {
	var gotPath string
	var gotBody []byte
	var gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: image_generation.partial\ndata: {\"type\":\"image_generation.partial\"}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	streamResult, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "upstream-image",
		Payload: []byte(`{"model":"compat-image","prompt":"draw","stream":true}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-image"),
		Stream:       true,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Metadata: map[string]any{
			cliproxyexecutor.RequestPathMetadataKey: "/v1/images/generations",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	var streamed bytes.Buffer
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		streamed.Write(chunk.Payload)
	}
	if gotPath != "/v1/images/generations" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/images/generations")
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("accept = %q, want text/event-stream", gotAccept)
	}
	if got := gjson.GetBytes(gotBody, "model").String(); got != "upstream-image" {
		t.Fatalf("model = %q, want upstream-image; body=%s", got, string(gotBody))
	}
	if !gjson.GetBytes(gotBody, "stream").Bool() {
		t.Fatalf("stream flag missing from upstream body: %s", string(gotBody))
	}
	if !strings.Contains(streamed.String(), "event: image_generation.partial") || !strings.Contains(streamed.String(), "data: [DONE]") {
		t.Fatalf("streamed body = %q", streamed.String())
	}
}

func TestOpenAICompatExecutorImagesEditsMultipartRewritesModel(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if errWrite := writer.WriteField("model", "compat-image"); errWrite != nil {
		t.Fatalf("write model field: %v", errWrite)
	}
	if errWrite := writer.WriteField("prompt", "edit"); errWrite != nil {
		t.Fatalf("write prompt field: %v", errWrite)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipart.FileContentDisposition("image", "image.png"))
	header.Set("Content-Type", "image/png")
	part, errCreate := writer.CreatePart(header)
	if errCreate != nil {
		t.Fatalf("create image field: %v", errCreate)
	}
	if _, errWrite := part.Write([]byte("png-data")); errWrite != nil {
		t.Fatalf("write image field: %v", errWrite)
	}
	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("close multipart writer: %v", errClose)
	}
	contentType := writer.FormDataContentType()

	var gotPath string
	var gotModel string
	var gotPrompt string
	var gotFile string
	var gotFileContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if errParse := r.ParseMultipartForm(32 << 20); errParse != nil {
			t.Fatalf("parse multipart form: %v", errParse)
		}
		gotModel = r.FormValue("model")
		gotPrompt = r.FormValue("prompt")
		file, fileHeader, errFile := r.FormFile("image")
		if errFile != nil {
			t.Fatalf("read image file: %v", errFile)
		}
		gotFileContentType = fileHeader.Header.Get("Content-Type")
		data, errRead := io.ReadAll(file)
		if errClose := file.Close(); errClose != nil {
			t.Fatalf("close image file: %v", errClose)
		}
		if errRead != nil {
			t.Fatalf("read image file: %v", errRead)
		}
		gotFile = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":123,"data":[{"b64_json":"AA=="}]}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "upstream-image",
		Payload: body.Bytes(),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-image"),
		Stream:       false,
		Headers: http.Header{
			"Content-Type": []string{contentType},
		},
		Metadata: map[string]any{
			cliproxyexecutor.RequestPathMetadataKey: "/v1/images/edits",
		},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/v1/images/edits" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/images/edits")
	}
	if gotModel != "upstream-image" {
		t.Fatalf("model = %q, want upstream-image", gotModel)
	}
	if gotPrompt != "edit" {
		t.Fatalf("prompt = %q, want edit", gotPrompt)
	}
	if gotFile != "png-data" {
		t.Fatalf("file = %q, want png-data", gotFile)
	}
	if gotFileContentType != "image/png" {
		t.Fatalf("file content type = %q, want image/png", gotFileContentType)
	}
}

func TestRewriteOpenAICompatImagesMultipartPayloadPreservesStreamAndFileContentType(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if errWrite := writer.WriteField("model", "compat-image"); errWrite != nil {
		t.Fatalf("write model field: %v", errWrite)
	}
	if errWrite := writer.WriteField("stream", "false"); errWrite != nil {
		t.Fatalf("write stream field: %v", errWrite)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipart.FileContentDisposition("image", "image.webp"))
	header.Set("Content-Type", "image/webp")
	part, errCreate := writer.CreatePart(header)
	if errCreate != nil {
		t.Fatalf("create image field: %v", errCreate)
	}
	if _, errWrite := part.Write([]byte("webp-data")); errWrite != nil {
		t.Fatalf("write image field: %v", errWrite)
	}
	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("close multipart writer: %v", errClose)
	}

	out, contentType, err := prepareOpenAICompatImagesPayload(body.Bytes(), "upstream-image", writer.FormDataContentType(), true)
	if err != nil {
		t.Fatalf("prepareOpenAICompatImagesPayload error: %v", err)
	}
	mediaType, params, errParse := mime.ParseMediaType(contentType)
	if errParse != nil {
		t.Fatalf("parse content type: %v", errParse)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("media type = %q, want multipart/form-data", mediaType)
	}
	reader := multipart.NewReader(bytes.NewReader(out), params["boundary"])
	form, errRead := reader.ReadForm(32 << 20)
	if errRead != nil {
		t.Fatalf("read rewritten form: %v", errRead)
	}
	defer func() {
		if errRemove := form.RemoveAll(); errRemove != nil {
			t.Fatalf("remove form files: %v", errRemove)
		}
	}()
	if got := form.Value["model"]; len(got) != 1 || got[0] != "upstream-image" {
		t.Fatalf("model values = %#v, want upstream-image", got)
	}
	if got := form.Value["stream"]; len(got) != 1 || got[0] != "true" {
		t.Fatalf("stream values = %#v, want true", got)
	}
	if got := form.File["image"]; len(got) != 1 || got[0].Header.Get("Content-Type") != "image/webp" {
		t.Fatalf("image headers = %#v, want image/webp", got)
	}
}

func TestOpenAICompatExecutorStreamRejectsPlainJSONAfterBlankLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("\n\n: openrouter processing\n\nevent: error\n"))
		_, _ = w.Write([]byte(`{"error":{"message":"upstream failed","type":"server_error"}}` + "\n"))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "openrouter-model",
		Payload: []byte(`{"model":"openrouter-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	var gotErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			gotErr = chunk.Err
			break
		}
	}
	if gotErr == nil {
		t.Fatalf("expected plain JSON stream error")
	}
	if status, ok := gotErr.(interface{ StatusCode() int }); !ok || status.StatusCode() != http.StatusBadGateway {
		t.Fatalf("stream error status = %v, want %d", gotErr, http.StatusBadGateway)
	}
	if !strings.Contains(gotErr.Error(), "upstream failed") {
		t.Fatalf("stream error = %v", gotErr)
	}
}

func TestOpenAICompatExecutorStreamSkipsKeepAliveUntilDataLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("\n\n: openrouter processing\n\nevent: ping\nid: 1\nretry: 1000\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}` + "\n"))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "test",
	}}
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "openrouter-model",
		Payload: []byte(`{"model":"openrouter-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	var got strings.Builder
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		got.Write(chunk.Payload)
	}
	if gjson.Get(got.String(), "choices.0.delta.content").String() != "hello" {
		t.Fatalf("stream payload = %s", got.String())
	}
}

func TestOpenAICompatHttpRequestAcquiresDeepSeekGatewaySlot(t *testing.T) {
	hook := newTestDeepSeekGatewayHook(t, 1, 1)
	release, err := hook.AcquireSlot(context.Background(), "cred:ds-auth")
	if err != nil {
		t.Fatalf("seed AcquireSlot: %v", err)
	}
	defer release()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer server.Close()

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	ctx = context.WithValue(ctx, "cliproxy.roundtripper", openAICompatRewriteRoundTripper{target: target})

	exec := NewOpenAICompatExecutor("deepseek", &config.Config{}).WithDeepSeekGatewayHook(hook)
	auth := &cliproxyauth.Auth{
		ID: "ds-auth",
		Attributes: map[string]string{
			"base_url": "https://api.deepseek.com",
			"api_key":  "k",
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/v1/chat/completions", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	_, err = exec.HttpRequest(ctx, auth, req)
	if err == nil {
		t.Fatal("HttpRequest should block on gateway slot and fail when context expires")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context") {
		t.Fatalf("HttpRequest error = %v, want context deadline/cancel from AcquireSlot", err)
	}
}

func TestOpenAICompatHttpRequestHoldsDeepSeekGatewaySlotUntilBodyClosed(t *testing.T) {
	hook := newTestDeepSeekGatewayHook(t, 1, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer server.Close()

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	auth := &cliproxyauth.Auth{
		ID: "ds-auth-hold",
		Attributes: map[string]string{
			"base_url": "https://api.deepseek.com",
			"api_key":  "k",
		},
	}
	makeRequest := func(ctx context.Context) (*http.Response, error) {
		ctx = context.WithValue(ctx, "cliproxy.roundtripper", openAICompatRewriteRoundTripper{target: target})
		exec := NewOpenAICompatExecutor("deepseek", &config.Config{}).WithDeepSeekGatewayHook(hook)
		req, errReq := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/v1/chat/completions", strings.NewReader(`{}`))
		if errReq != nil {
			return nil, errReq
		}
		return exec.HttpRequest(ctx, auth, req)
	}

	// First request acquires the slot; it must stay held until Body.Close.
	resp1, err := makeRequest(context.Background())
	if err != nil {
		t.Fatalf("first HttpRequest: %v", err)
	}

	// Second request: slot still held by the open body → must time out on AcquireSlot.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel2()
	if _, errSecond := makeRequest(ctx2); errSecond == nil {
		_ = resp1.Body.Close()
		t.Fatal("second HttpRequest should block while first response body is open")
	}

	// Close the first body to release the slot.
	if errClose := resp1.Body.Close(); errClose != nil {
		t.Fatalf("close first body: %v", errClose)
	}

	// Third request: slot free again → must succeed.
	resp3, err := makeRequest(context.Background())
	if err != nil {
		t.Fatalf("third HttpRequest after body closed: %v", err)
	}
	_ = resp3.Body.Close()
}

func TestOpenAICompatExecuteImagesAcquiresDeepSeekGatewaySlot(t *testing.T) {
	hook := newTestDeepSeekGatewayHook(t, 1, 1)
	release, err := hook.AcquireSlot(context.Background(), "cred:ds-auth-img")
	if err != nil {
		t.Fatalf("seed AcquireSlot: %v", err)
	}
	defer release()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	ctx = context.WithValue(ctx, "cliproxy.roundtripper", openAICompatRewriteRoundTripper{target: target})

	exec := NewOpenAICompatExecutor("deepseek", &config.Config{}).WithDeepSeekGatewayHook(hook)
	auth := &cliproxyauth.Auth{
		ID: "ds-auth-img",
		Attributes: map[string]string{
			"base_url": "https://api.deepseek.com",
			"api_key":  "k",
		},
	}
	_, err = exec.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "deepseek-image",
		Payload: []byte(`{"model":"deepseek-image","prompt":"a cat"}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString(openAICompatImageHandlerType),
		Metadata: map[string]any{
			cliproxyexecutor.RequestPathMetadataKey: "/v1/images/generations",
		},
	})
	if err == nil {
		t.Fatal("Execute images should block on gateway slot and fail when context expires")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context") {
		t.Fatalf("Execute images error = %v, want context deadline/cancel from AcquireSlot", err)
	}
}

func newTestDeepSeekGatewayHook(t *testing.T, globalMax, perUserMax int) *ratelimit.DeepSeekGatewayHook {
	t.Helper()
	mgr := ratelimit.NewDeepSeekLimiterManager(ratelimit.DeepSeekLimiterConfig{
		GlobalMaxConcurrency:    globalMax,
		PerUserIDMaxConcurrency: perUserMax,
	})
	resolver := ratelimit.NewUserIDResolver(ratelimit.StrategyPerCredential, "", "", "cred")
	hook := ratelimit.NewDeepSeekGatewayHook(config.DeepSeekGatewayConfig{
		Enabled:                 true,
		GlobalMaxConcurrency:    globalMax,
		PerUserIDMaxConcurrency: perUserMax,
		RetryMaxAttempts:        1,
		UserIDStrategy:          "per_credential",
	}, mgr, resolver)
	if !hook.Enabled() {
		t.Fatal("hook should be enabled")
	}
	return hook
}
