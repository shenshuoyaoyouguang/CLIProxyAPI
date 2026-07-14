package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator" // register translators
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestOpenAICompatExecutorChatCompletionsFormatsAuthenticatedRequest(t *testing.T) {
	var gotPath string
	var gotMethod string
	var gotAuth string
	var gotContentType string
	var gotUserAgent string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotUserAgent = r.Header.Get("User-Agent")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":123,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "sk-test-secret",
		},
	}
	payload := []byte(`{"model":"client-model","messages":[{"role":"system","content":"Be concise."},{"role":"user","content":"Say hello"}],"temperature":0.3,"max_tokens":64,"max_completion_tokens":128,"top_p":0.9,"frequency_penalty":0.1,"presence_penalty":0.2,"stop":["END","DONE"]}`)

	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-4.1-mini",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai"),
		ResponseFormat:  sdktranslator.FromString("openai"),
		OriginalRequest: payload,
		Stream:          false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test-secret" {
		t.Fatalf("Authorization = %q, want bearer API key", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotUserAgent != "cli-proxy-openai-compat" {
		t.Fatalf("User-Agent = %q, want cli-proxy-openai-compat", gotUserAgent)
	}

	requireJSONField(t, gotBody, "model", "gpt-4.1-mini")
	requireJSONField(t, gotBody, "messages.0.role", "system")
	requireJSONField(t, gotBody, "messages.0.content", "Be concise.")
	requireJSONField(t, gotBody, "messages.1.role", "user")
	requireJSONField(t, gotBody, "messages.1.content", "Say hello")
	requireJSONNumber(t, gotBody, "temperature", 0.3)
	requireJSONNumber(t, gotBody, "max_tokens", 64)
	requireJSONNumber(t, gotBody, "max_completion_tokens", 128)
	requireJSONNumber(t, gotBody, "top_p", 0.9)
	requireJSONNumber(t, gotBody, "frequency_penalty", 0.1)
	requireJSONNumber(t, gotBody, "presence_penalty", 0.2)
	requireJSONField(t, gotBody, "stop.0", "END")
	requireJSONField(t, gotBody, "stop.1", "DONE")

	if got := gjson.GetBytes(resp.Payload, "choices.0.message.content").String(); got != "hello" {
		t.Fatalf("response content = %q, want hello; payload=%s", got, string(resp.Payload))
	}
}

func TestOpenAICompatExecutorChatCompletionsStreamsSSE(t *testing.T) {
	var gotAuth string
	var gotAccept string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":123,"model":"gpt-4.1-mini","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":123,"model":"gpt-4.1-mini","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":123,"model":"gpt-4.1-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "sk-stream-secret",
		},
	}
	payload := []byte(`{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-4.1-mini",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai"),
		ResponseFormat:  sdktranslator.FromString("openai"),
		OriginalRequest: payload,
		Stream:          true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	raw := collectStreamChunks(t, result)

	if gotAuth != "Bearer sk-stream-secret" {
		t.Fatalf("Authorization = %q, want bearer API key", gotAuth)
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", gotAccept)
	}
	if !gjson.GetBytes(gotBody, "stream").Bool() {
		t.Fatalf("stream flag missing from upstream body: %s", string(gotBody))
	}
	if !gjson.GetBytes(gotBody, "stream_options.include_usage").Bool() {
		t.Fatalf("stream_options.include_usage missing from upstream body: %s", string(gotBody))
	}
	if !bytes.Contains(raw, []byte(`"content":"hello"`)) {
		t.Fatalf("stream output missing content chunk: %s", string(raw))
	}
	if bytes.Contains(raw, []byte("[DONE]")) {
		t.Fatalf("OpenAI-format executor output should not include raw DONE marker: %s", string(raw))
	}
}

func TestOpenAICompatExecutorChatCompletionsPropagatesCommonAPIErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`,
			want:       "Incorrect API key provided",
		},
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"message":"Rate limit reached","type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
			want:       "Rate limit reached",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
			auth := &cliproxyauth.Auth{Attributes: map[string]string{
				"base_url": server.URL + "/v1",
				"api_key":  "sk-error-secret",
			}}
			_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
				Model:   "gpt-4.1-mini",
				Payload: []byte(`{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"hi"}]}`),
			}, cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FromString("openai"),
				Stream:       false,
			})
			if err == nil {
				t.Fatal("Execute error = nil, want upstream API error")
			}
			statusErr, ok := err.(interface{ StatusCode() int })
			if !ok {
				t.Fatalf("error does not expose StatusCode(): %T", err)
			}
			if got := statusErr.StatusCode(); got != tt.statusCode {
				t.Fatalf("StatusCode() = %d, want %d", got, tt.statusCode)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestOpenAICompatExecutorChatCompletionsCapturesRequestResponseLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_log","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"logged"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), "gin", ginCtx)

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		SDKConfig: config.SDKConfig{RequestLog: true},
	})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		ID:       "auth-log",
		Label:    "primary",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "sk-log-secret",
		},
	}
	payload := []byte(`{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"log this"}]}`)

	_, err := executor.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-4.1-mini",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai"),
		ResponseFormat:  sdktranslator.FromString("openai"),
		OriginalRequest: payload,
		Stream:          false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	requestLog := ginContextBytes(t, ginCtx, "API_REQUEST")
	responseLog := ginContextBytes(t, ginCtx, "API_RESPONSE")
	requestText := string(requestLog)
	responseText := string(responseLog)

	for _, want := range []string{
		"=== API REQUEST 1 ===",
		"Upstream URL: " + server.URL + "/v1/chat/completions",
		"HTTP Method: POST",
		"Authorization: Bearer ",
		`"messages":[{"role":"user","content":"log this"}]`,
	} {
		if !strings.Contains(requestText, want) {
			t.Fatalf("request log missing %q:\n%s", want, requestText)
		}
	}
	if strings.Contains(requestText, "sk-log-secret") {
		t.Fatalf("request log leaked raw API key:\n%s", requestText)
	}
	for _, want := range []string{
		"=== API RESPONSE 1 ===",
		"Status: 200",
		`"content":"logged"`,
	} {
		if !strings.Contains(responseText, want) {
			t.Fatalf("response log missing %q:\n%s", want, responseText)
		}
	}
}

func requireJSONField(t *testing.T, body []byte, path string, want string) {
	t.Helper()
	if got := gjson.GetBytes(body, path).String(); got != want {
		t.Fatalf("%s = %q, want %q; body=%s", path, got, want, string(body))
	}
}

func requireJSONNumber(t *testing.T, body []byte, path string, want float64) {
	t.Helper()
	if got := gjson.GetBytes(body, path).Float(); got != want {
		t.Fatalf("%s = %v, want %v; body=%s", path, got, want, string(body))
	}
}

func ginContextBytes(t *testing.T, ginCtx *gin.Context, key string) []byte {
	t.Helper()
	value, exists := ginCtx.Get(key)
	if !exists {
		t.Fatalf("gin context key %q missing", key)
	}
	data, ok := value.([]byte)
	if !ok {
		t.Fatalf("gin context key %q has type %T, want []byte", key, value)
	}
	return data
}
