package executor

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator" // register translators
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// sseEvent represents a parsed SSE event with its type and data payload.
type sseEvent struct {
	Type    string
	Payload string
}

func TestOpenAICompatExecutor_StreamRetryDefaultDisabled(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("response writer does not implement http.Hijacker")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_ = conn.Close()
	}))
	defer server.Close()

	payload := []byte(`{"model":"test-model","max_tokens":1024,"messages":[{"role":"user","content":"Say ok"}],"stream":true}`)
	executor, auth, req, opts := newOpenAICompatTestExecutor(t, server, payload, true, sdktranslator.FormatClaude, sdktranslator.FormatClaude)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	_, streamErr := collectStreamChunksAllowError(t, result)
	if streamErr == nil {
		t.Fatal("stream error = nil, want disconnect error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 when OpenAI-compatible stream retry is disabled by default", attempts)
	}
}

func TestOpenAICompatExecutor_StreamRetryExplicitlyEnabled(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("response writer does not implement http.Hijacker")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_ = conn.Close()
	}))
	defer server.Close()

	payload := []byte(`{"model":"test-model","max_tokens":1024,"messages":[{"role":"user","content":"Say ok"}],"stream":true}`)
	executor, auth, req, opts := newOpenAICompatTestExecutor(t, server, payload, true, sdktranslator.FormatClaude, sdktranslator.FormatClaude)
	executor.cfg.Streaming.StreamRetryEnabled = true
	executor.cfg.Streaming.StreamRetryCount = 2

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	_, streamErr := collectStreamChunksAllowError(t, result)
	if streamErr == nil {
		t.Fatal("stream error = nil, want disconnect error")
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 when OpenAI-compatible stream retry is explicitly enabled", attempts)
	}
}

// parseSSEEvents parses SSE bytes into a list of events. Each SSE frame is
// separated by a blank line ("\n\n"). Within a frame, "event:" sets the type
// and "data:" sets the payload. Frames without an event type are skipped.
func parseSSEEvents(data []byte) []sseEvent {
	var events []sseEvent
	frames := bytes.Split(data, []byte("\n\n"))
	for _, frame := range frames {
		frame = bytes.TrimSpace(frame)
		if len(frame) == 0 {
			continue
		}
		var eventType string
		var payload string
		lines := bytes.Split(frame, []byte("\n"))
		for _, line := range lines {
			line = bytes.TrimRight(line, "\r")
			if et, ok := bytes.CutPrefix(line, []byte("event:")); ok {
				eventType = strings.TrimSpace(string(et))
				continue
			}
			if d, ok := bytes.CutPrefix(line, []byte("data:")); ok {
				payload = strings.TrimSpace(string(d))
			}
		}
		if eventType != "" {
			events = append(events, sseEvent{Type: eventType, Payload: payload})
		}
	}
	return events
}

// collectStreamChunks drains the stream result channel and returns the joined
// payload bytes. Fails the test if any chunk carries an error.
func collectStreamChunks(t *testing.T, result *cliproxyexecutor.StreamResult) []byte {
	t.Helper()
	var buf bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		buf.Write(chunk.Payload)
	}
	return buf.Bytes()
}

func collectStreamChunksAllowError(t *testing.T, result *cliproxyexecutor.StreamResult) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	var streamErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			streamErr = chunk.Err
			continue
		}
		buf.Write(chunk.Payload)
	}
	return buf.Bytes(), streamErr
}

// newOpenAICompatTestExecutor builds an executor backed by a mock upstream
// server together with the auth and request scaffolding used by every test.
func newOpenAICompatTestExecutor(t *testing.T, server *httptest.Server, payload []byte, stream bool, sourceFormat, responseFormat sdktranslator.Format) (*OpenAICompatExecutor, *cliproxyauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) {
	t.Helper()
	executor := NewOpenAICompatExecutor("test-provider", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL,
		"api_key":  "test-key",
	}}
	req := cliproxyexecutor.Request{Model: "test-model", Payload: payload}
	opts := cliproxyexecutor.Options{
		SourceFormat:    sourceFormat,
		ResponseFormat:  responseFormat,
		Stream:          stream,
		OriginalRequest: payload,
	}
	return executor, auth, req, opts
}

// TestOpenAICompatExecutor_StreamNormalizer_Passthrough verifies that a
// well-formed OpenAI SSE stream is translated to Claude SSE events in the
// correct Anthropic protocol order, with no events lost.
func TestOpenAICompatExecutor_StreamNormalizer_Passthrough(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// First chunk establishes the assistant role (emits message_start).
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`)
		// Content chunks emit content_block_start + content_block_delta.
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"content":"hello"}}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"content":" world"}}]}`)
		// Final chunk carries finish_reason + usage, emitting content_block_stop,
		// message_delta and message_stop.
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`)
		fmt.Fprintf(w, "data: %s\n\n", "[DONE]")
	}))
	defer server.Close()

	payload := []byte(`{"model":"test-model","max_tokens":1024,"messages":[{"role":"user","content":"Say ok"}],"stream":true}`)
	executor, auth, req, opts := newOpenAICompatTestExecutor(t, server, payload, true, sdktranslator.FormatClaude, sdktranslator.FormatClaude)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	raw := collectStreamChunks(t, result)
	events := parseSSEEvents(raw)

	if len(events) == 0 {
		t.Fatalf("no SSE events parsed from stream output; raw=%q", string(raw))
	}

	// Expected Anthropic protocol order. content_block_delta may appear
	// multiple times, so we verify the remaining types appear as a
	// subsequence.
	expectedOrder := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}

	var seenTypes []string
	for _, ev := range events {
		seenTypes = append(seenTypes, ev.Type)
	}
	idx := 0
	for _, et := range expectedOrder {
		found := false
		for ; idx < len(seenTypes); idx++ {
			if seenTypes[idx] == et {
				found = true
				idx++
				break
			}
		}
		if !found {
			t.Fatalf("event %s not found in protocol order; seen=%v", et, seenTypes)
		}
	}

	// Verify no events were lost: at least one content_block_delta must carry
	// text content.
	hasTextDelta := false
	for _, ev := range events {
		if ev.Type == "content_block_delta" && strings.Contains(ev.Payload, "text_delta") {
			hasTextDelta = true
			break
		}
	}
	if !hasTextDelta {
		t.Fatalf("no content_block_delta with text_delta in output; events=%v", seenTypes)
	}

	// message_stop must be the final event (no leaked post-terminal events).
	if last := events[len(events)-1]; last.Type != "message_stop" {
		t.Fatalf("expected message_stop as last event, got %s; seen=%v", last.Type, seenTypes)
	}
}

func TestOpenAICompatExecutor_StreamScannerErrorDoesNotFlushNormalCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"content":"partial"}}]}`)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 52_428_801))
	}))
	defer server.Close()

	payload := []byte(`{"model":"test-model","max_tokens":1024,"messages":[{"role":"user","content":"Say ok"}],"stream":true}`)
	executor, auth, req, opts := newOpenAICompatTestExecutor(t, server, payload, true, sdktranslator.FormatClaude, sdktranslator.FormatClaude)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	raw, streamErr := collectStreamChunksAllowError(t, result)
	if streamErr == nil {
		t.Fatal("stream error = nil, want scanner error")
	}
	events := parseSSEEvents(raw)
	for _, ev := range events {
		if ev.Type == "message_stop" {
			t.Fatalf("scanner error stream flushed normal message_stop event; events=%v raw=%q", events, string(raw))
		}
	}
}

// TestOpenAICompatExecutor_NonStreamNormalizer_ReordersContent verifies that
// the non-stream executor pipeline applies NormalizeNonStreamContentOrder to
// the translated Claude response. We feed an OpenAI response whose array
// content has reasoning before text: the translator emits [thinking, text] in
// that order, and the normalizer must reorder to [text, thinking].
func TestOpenAICompatExecutor_NonStreamNormalizer_ReordersContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
  "id": "chatcmpl-1",
  "object": "chat.completion",
  "created": 1000,
  "model": "test-model",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": [
          {"type": "reasoning", "text": "thinking..."},
          {"type": "text", "text": "answer"}
        ]
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
}`))
	}))
	defer server.Close()

	payload := []byte(`{"model":"test-model","max_tokens":1024,"messages":[{"role":"user","content":"Say ok"}]}`)
	executor, auth, req, opts := newOpenAICompatTestExecutor(t, server, payload, false, sdktranslator.FormatClaude, sdktranslator.FormatClaude)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := executor.Execute(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Response must be a Claude message.
	if got := gjson.GetBytes(resp.Payload, "type").String(); got != "message" {
		t.Fatalf("response type = %q, want %q; payload=%s", got, "message", string(resp.Payload))
	}

	// content array must be reordered to [text, thinking] (text first).
	content := gjson.GetBytes(resp.Payload, "content")
	if !content.IsArray() {
		t.Fatalf("content is not an array; payload=%s", string(resp.Payload))
	}
	blocks := content.Array()
	if len(blocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d; payload=%s", len(blocks), string(resp.Payload))
	}
	if got := blocks[0].Get("type").String(); got != "text" {
		t.Fatalf("content[0].type = %q, want %q; payload=%s", got, "text", string(resp.Payload))
	}
	if got := blocks[1].Get("type").String(); got != "thinking" {
		t.Fatalf("content[1].type = %q, want %q; payload=%s", got, "thinking", string(resp.Payload))
	}

	// stop_reason must be present (translator sets "end_turn" from
	// finish_reason="stop"; normalizer preserves it).
	sr := gjson.GetBytes(resp.Payload, "stop_reason")
	if !sr.Exists() {
		t.Fatalf("stop_reason missing; payload=%s", string(resp.Payload))
	}
	if got := sr.String(); got == "" {
		t.Fatalf("stop_reason is empty; payload=%s", string(resp.Payload))
	}
}

// TestOpenAICompatExecutor_StreamNormalizer_FlushesOnMissingTerminal verifies
// that when the upstream OpenAI SSE stream is missing finish_reason and usage,
// the SSENormalizer.Flush synthesizes the missing terminal events
// (message_delta and message_stop) so the client still observes a complete
// Anthropic protocol stream.
func TestOpenAICompatExecutor_StreamNormalizer_FlushesOnMissingTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// First chunk establishes the assistant role (message_start).
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`)
		// Content chunk emits content_block_start + content_block_delta.
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"content":"hello"}}]}`)
		// Stream ends abruptly: no finish_reason, no usage, no [DONE].
	}))
	defer server.Close()

	payload := []byte(`{"model":"test-model","max_tokens":1024,"messages":[{"role":"user","content":"Say ok"}],"stream":true}`)
	executor, auth, req, opts := newOpenAICompatTestExecutor(t, server, payload, true, sdktranslator.FormatClaude, sdktranslator.FormatClaude)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	raw := collectStreamChunks(t, result)
	events := parseSSEEvents(raw)

	if len(events) == 0 {
		t.Fatalf("no SSE events parsed from stream output; raw=%q", string(raw))
	}

	var seenTypes []string
	for _, ev := range events {
		seenTypes = append(seenTypes, ev.Type)
	}

	// message_delta must be present. The upstream never sent finish_reason or
	// usage, so the translator's [DONE] handler cannot emit message_delta
	// (FinishReason is empty). The SSENormalizer.Flush must synthesize it.
	hasMessageDelta := false
	for _, ev := range events {
		if ev.Type == "message_delta" {
			hasMessageDelta = true
			if !strings.Contains(ev.Payload, "stop_reason") {
				t.Fatalf("synthesized message_delta missing stop_reason: %s", ev.Payload)
			}
		}
	}
	if !hasMessageDelta {
		t.Fatalf("message_delta not found in output (Flush should synthesize it); events=%v", seenTypes)
	}

	// message_stop must be present (either from translator's [DONE] handler or
	// synthesized by Flush).
	hasMessageStop := false
	for _, ev := range events {
		if ev.Type == "message_stop" {
			hasMessageStop = true
			break
		}
	}
	if !hasMessageStop {
		t.Fatalf("message_stop not found in output; events=%v", seenTypes)
	}

	// content_block_stop must be present (emitted by translator's [DONE]
	// handler or by normalizer.Flush for still-active blocks).
	hasContentBlockStop := false
	for _, ev := range events {
		if ev.Type == "content_block_stop" {
			hasContentBlockStop = true
			break
		}
	}
	if !hasContentBlockStop {
		t.Fatalf("content_block_stop not found in output; events=%v", seenTypes)
	}

	// message_start and content_block_start must also be present (sanity check
	// that the stream was not empty).
	hasMessageStart := false
	hasContentBlockStart := false
	for _, ev := range events {
		switch ev.Type {
		case "message_start":
			hasMessageStart = true
		case "content_block_start":
			hasContentBlockStart = true
		}
	}
	if !hasMessageStart {
		t.Fatalf("message_start not found in output; events=%v", seenTypes)
	}
	if !hasContentBlockStart {
		t.Fatalf("content_block_start not found in output; events=%v", seenTypes)
	}
}

// TestOpenAICompatExecutor_StreamNormalizer_NoRegressionForOpenAIFormat
// verifies that when ResponseFormat is OpenAI (not Claude), the SSENormalizer
// is not enabled. The output should be raw OpenAI JSON payloads, not
// Claude-protocol SSE events.
func TestOpenAICompatExecutor_StreamNormalizer_NoRegressionForOpenAIFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{"content":"hello"}}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1000,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`)
		fmt.Fprintf(w, "data: %s\n\n", "[DONE]")
	}))
	defer server.Close()

	payload := []byte(`{"model":"test-model","messages":[{"role":"user","content":"Say ok"}],"stream":true}`)
	executor, auth, req, opts := newOpenAICompatTestExecutor(t, server, payload, true, sdktranslator.FormatOpenAI, sdktranslator.FormatOpenAI)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	raw := collectStreamChunks(t, result)

	// The OpenAI-to-OpenAI translator strips the "data:" prefix and returns raw
	// JSON payloads. The output must contain OpenAI-format chunks (with the
	// "choices" field).
	if !bytes.Contains(raw, []byte(`"choices"`)) {
		t.Fatalf("output missing OpenAI choices field; raw=%q", string(raw))
	}

	// The SSENormalizer must NOT have wrapped the output in Claude SSE events.
	// Assert that no Anthropic event markers are present.
	for _, marker := range []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Fatalf("output contains Claude SSE marker %q, normalizer should be disabled for OpenAI format; raw=%q", marker, string(raw))
		}
	}

	// The output must contain the streamed text content ("hello").
	if !bytes.Contains(raw, []byte("hello")) {
		t.Fatalf("output missing streamed content 'hello'; raw=%q", string(raw))
	}
}
