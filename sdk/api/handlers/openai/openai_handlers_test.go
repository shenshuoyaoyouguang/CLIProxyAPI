package openai

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/tidwall/gjson"
)

func TestShouldTreatAsResponsesFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "messages present wins",
			body: `{"model":"gpt-4.1","messages":[{"role":"user","content":"hi"}],"input":"x"}`,
			want: false,
		},
		{
			name: "input only",
			body: `{"model":"gpt-4.1","input":"hello"}`,
			want: true,
		},
		{
			name: "instructions only",
			body: `{"model":"gpt-4.1","instructions":"be brief"}`,
			want: true,
		},
		{
			name: "neither messages nor responses fields",
			body: `{"model":"gpt-4.1","prompt":"legacy"}`,
			want: false,
		},
		{
			name: "empty object",
			body: `{}`,
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shouldTreatAsResponsesFormat([]byte(tt.body))
			if got != tt.want {
				t.Fatalf("shouldTreatAsResponsesFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertCompletionsRequestToChatCompletions(t *testing.T) {
	t.Parallel()

	t.Run("copies sampling fields and wraps prompt", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{
			"model":"gpt-4.1",
			"prompt":"Once upon a time",
			"max_tokens":64,
			"temperature":0.7,
			"top_p":0.9,
			"frequency_penalty":0.1,
			"presence_penalty":0.2,
			"stop":["END","STOP"],
			"stream":true,
			"logprobs":true,
			"top_logprobs":3,
			"echo":true
		}`)

		out := convertCompletionsRequestToChatCompletions(in)
		root := gjson.ParseBytes(out)

		if got := root.Get("model").String(); got != "gpt-4.1" {
			t.Fatalf("model = %q, want gpt-4.1", got)
		}
		if got := root.Get("messages.0.role").String(); got != "user" {
			t.Fatalf("messages.0.role = %q, want user", got)
		}
		if got := root.Get("messages.0.content").String(); got != "Once upon a time" {
			t.Fatalf("messages.0.content = %q", got)
		}
		if got := root.Get("max_tokens").Int(); got != 64 {
			t.Fatalf("max_tokens = %d, want 64", got)
		}
		if got := root.Get("temperature").Float(); got != 0.7 {
			t.Fatalf("temperature = %v, want 0.7", got)
		}
		if got := root.Get("top_p").Float(); got != 0.9 {
			t.Fatalf("top_p = %v, want 0.9", got)
		}
		if got := root.Get("frequency_penalty").Float(); got != 0.1 {
			t.Fatalf("frequency_penalty = %v, want 0.1", got)
		}
		if got := root.Get("presence_penalty").Float(); got != 0.2 {
			t.Fatalf("presence_penalty = %v, want 0.2", got)
		}
		if got := root.Get("stop.#").Int(); got != 2 {
			t.Fatalf("stop length = %d, want 2", got)
		}
		if !root.Get("stream").Bool() {
			t.Fatal("stream should be true")
		}
		if !root.Get("logprobs").Bool() {
			t.Fatal("logprobs should be true")
		}
		if got := root.Get("top_logprobs").Int(); got != 3 {
			t.Fatalf("top_logprobs = %d, want 3", got)
		}
		if !root.Get("echo").Bool() {
			t.Fatal("echo should be true")
		}
	})

	t.Run("empty prompt defaults", func(t *testing.T) {
		t.Parallel()
		out := convertCompletionsRequestToChatCompletions([]byte(`{"model":"m"}`))
		if got := gjson.GetBytes(out, "messages.0.content").String(); got != "Complete this:" {
			t.Fatalf("default prompt content = %q, want Complete this:", got)
		}
	})
}

func TestConvertChatCompletionsResponseToCompletions(t *testing.T) {
	t.Parallel()

	in := []byte(`{
		"id":"chatcmpl-1",
		"object":"chat.completion",
		"created":1710000000,
		"model":"gpt-4.1",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":"hello world"},
			"finish_reason":"stop"
		}],
		"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
	}`)

	out := convertChatCompletionsResponseToCompletions(in)
	root := gjson.ParseBytes(out)

	if got := root.Get("object").String(); got != "text_completion" {
		t.Fatalf("object = %q, want text_completion", got)
	}
	if got := root.Get("id").String(); got != "chatcmpl-1" {
		t.Fatalf("id = %q", got)
	}
	if got := root.Get("model").String(); got != "gpt-4.1" {
		t.Fatalf("model = %q", got)
	}
	if got := root.Get("choices.0.text").String(); got != "hello world" {
		t.Fatalf("choices.0.text = %q", got)
	}
	if got := root.Get("choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("finish_reason = %q", got)
	}
	if got := root.Get("usage.total_tokens").Int(); got != 5 {
		t.Fatalf("usage.total_tokens = %d, want 5", got)
	}
}

func TestConvertChatCompletionsStreamChunkToCompletions(t *testing.T) {
	t.Parallel()

	t.Run("filters empty delta without finish or usage", func(t *testing.T) {
		t.Parallel()
		chunk := []byte(`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`)
		if got := convertChatCompletionsStreamChunkToCompletions(chunk); got != nil {
			t.Fatalf("expected nil filtered chunk, got %s", string(got))
		}
	})

	t.Run("keeps content delta", func(t *testing.T) {
		t.Parallel()
		chunk := []byte(`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`)
		out := convertChatCompletionsStreamChunkToCompletions(chunk)
		if out == nil {
			t.Fatal("expected converted chunk")
		}
		if got := gjson.GetBytes(out, "object").String(); got != "text_completion" {
			t.Fatalf("object = %q", got)
		}
		if got := gjson.GetBytes(out, "choices.0.text").String(); got != "hi" {
			t.Fatalf("text = %q", got)
		}
	})

	t.Run("keeps finish_reason only chunks", func(t *testing.T) {
		t.Parallel()
		chunk := []byte(`{"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		out := convertChatCompletionsStreamChunkToCompletions(chunk)
		if out == nil {
			t.Fatal("expected finish chunk to be kept")
		}
		if got := gjson.GetBytes(out, "choices.0.finish_reason").String(); got != "stop" {
			t.Fatalf("finish_reason = %q", got)
		}
	})

	t.Run("keeps usage-only chunks", func(t *testing.T) {
		t.Parallel()
		chunk := []byte(`{"id":"c1","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		out := convertChatCompletionsStreamChunkToCompletions(chunk)
		if out == nil {
			t.Fatal("expected usage chunk to be kept")
		}
		if got := gjson.GetBytes(out, "usage.total_tokens").Int(); got != 2 {
			t.Fatalf("usage.total_tokens = %d", got)
		}
	})
}

func TestPendingStreamError(t *testing.T) {
	t.Parallel()

	t.Run("nil channel", func(t *testing.T) {
		t.Parallel()
		msg, ok := pendingStreamError(nil)
		if ok || msg != nil {
			t.Fatalf("expected false/nil for nil channel, got ok=%v msg=%v", ok, msg)
		}
	})

	t.Run("empty channel non-blocking", func(t *testing.T) {
		t.Parallel()
		ch := make(chan *interfaces.ErrorMessage)
		msg, ok := pendingStreamError(ch)
		if ok || msg != nil {
			t.Fatalf("expected false/nil for empty channel, got ok=%v msg=%v", ok, msg)
		}
	})

	t.Run("queued error", func(t *testing.T) {
		t.Parallel()
		ch := make(chan *interfaces.ErrorMessage, 1)
		want := &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: errors.New("upstream")}
		ch <- want
		msg, ok := pendingStreamError(ch)
		if !ok || msg != want {
			t.Fatalf("expected queued error, got ok=%v msg=%v", ok, msg)
		}
	})

	t.Run("closed channel", func(t *testing.T) {
		t.Parallel()
		ch := make(chan *interfaces.ErrorMessage)
		close(ch)
		msg, ok := pendingStreamError(ch)
		if ok || msg != nil {
			t.Fatalf("expected false/nil for closed channel, got ok=%v msg=%v", ok, msg)
		}
	})
}

func newChatHandlersTestHandler(t *testing.T) (*OpenAIAPIHandler, *httptest.ResponseRecorder, *gin.Context, http.Flusher) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	h := NewOpenAIAPIHandler(base)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		t.Fatalf("expected gin writer to implement http.Flusher")
	}
	return h, recorder, c, flusher
}

func TestHandleStreamResultWritesChunksAndDone(t *testing.T) {
	h, recorder, c, flusher := newChatHandlersTestHandler(t)

	data := make(chan []byte, 2)
	errs := make(chan *interfaces.ErrorMessage)
	data <- []byte(`{"id":"c1","object":"chat.completion.chunk","choices":[{"delta":{"content":"hi"}}]}`)
	data <- []byte(`{"id":"c1","object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}]}`)
	close(data)
	close(errs)

	h.handleStreamResult(c, flusher, func(error) {}, data, errs)

	body := recorder.Body.String()
	if !strings.Contains(body, `data: {"id":"c1"`) {
		t.Fatalf("expected data chunks in body, got: %q", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") && !strings.Contains(body, "data: [DONE]\n\n") {
		t.Fatalf("expected terminal [DONE], got: %q", body)
	}
}

func TestHandleStreamResultWritesTerminalError(t *testing.T) {
	h, recorder, c, flusher := newChatHandlersTestHandler(t)

	data := make(chan []byte)
	errs := make(chan *interfaces.ErrorMessage, 1)
	errs <- &interfaces.ErrorMessage{
		StatusCode: http.StatusBadGateway,
		Error:      errors.New("upstream closed"),
	}
	close(data)
	close(errs)

	h.handleStreamResult(c, flusher, func(error) {}, data, errs)

	body := recorder.Body.String()
	if !strings.Contains(body, "upstream closed") {
		t.Fatalf("expected terminal error payload, got: %q", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("should not emit [DONE] after terminal stream error, got: %q", body)
	}
	// Chat Completions stream errors use OpenAI error envelope via BuildErrorResponseBody.
	if !strings.Contains(body, `"error"`) {
		t.Fatalf("expected error envelope in stream body, got: %q", body)
	}
}

func TestChatCompletionsInvalidContentEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	h := NewOpenAIAPIHandler(base)

	router := gin.New()
	router.POST("/v1/chat/completions", h.ChatCompletions)

	// Unsupported Content-Encoding with non-JSON body fails ReadRequestBody
	// (valid JSON bodies fall back to identity even when decode fails).
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("not-json-body"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
	if got := gjson.GetBytes(resp.Body.Bytes(), "error.type").String(); got != "invalid_request_error" {
		t.Fatalf("error.type = %q, want invalid_request_error", got)
	}
	if msg := gjson.GetBytes(resp.Body.Bytes(), "error.message").String(); !strings.Contains(msg, "Invalid request") {
		t.Fatalf("error.message = %q, want Invalid request prefix", msg)
	}
}

func TestOpenAIAPIHandlerType(t *testing.T) {
	t.Parallel()
	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	h := NewOpenAIAPIHandler(base)
	if got := h.HandlerType(); got == "" {
		t.Fatal("HandlerType should not be empty")
	}
}
