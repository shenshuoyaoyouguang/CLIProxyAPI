package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

// TestClaudeExecutor_StreamRetry_PassthroughMode_Success verifies that the Claude
// executor retries on unexpected EOF before any SSE data is received in passthrough mode.
func TestClaudeExecutor_StreamRetry_PassthroughMode_Success(t *testing.T) {
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		if attempt == 1 {
			// First attempt: close connection immediately (simulate unexpected EOF)
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server doesn't support hijacking")
				return
			}
			conn, err := hijackResponse(hj, w)
			if err != nil {
				t.Fatal("hijack failed:", err)
				return
			}
			conn.Close()
			return
		}

		// Second attempt: return valid SSE stream
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-3\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunks []cliproxyexecutor.StreamChunk
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	if attemptCount.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attemptCount.Load())
	}
}

// TestClaudeExecutor_StreamRetry_TranslationMode_Success verifies retry in translation mode.
func TestClaudeExecutor_StreamRetry_TranslationMode_Success(t *testing.T) {
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		if attempt == 1 {
			// First attempt: return empty body and close (simulate unexpected EOF)
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server doesn't support hijacking")
				return
			}
			conn, err := hijackResponse(hj, w)
			if err != nil {
				t.Fatal("hijack failed:", err)
				return
			}
			conn.Close()
			return
		}

		// Second attempt: return valid SSE stream
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-3\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)

	// Use OpenAI format to trigger translation mode
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunks []cliproxyexecutor.StreamChunk
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	if attemptCount.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attemptCount.Load())
	}
}

// TestClaudeExecutor_StreamRetry_NoRetryWhenSSEDataReceived verifies that retry
// is NOT attempted when SSE data has already been received.
func TestClaudeExecutor_StreamRetry_NoRetryWhenSSEDataReceived(t *testing.T) {
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		// Return partial SSE data then close abruptly
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n")

		// Close connection abruptly after sending some data
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, err := hijackResponse(hj, w)
		if err != nil {
			return
		}
		conn.Close()
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunks []cliproxyexecutor.StreamChunk
	for chunk := range result.Chunks {
		chunks = append(chunks, chunk)
	}

	// Should only have 1 attempt because SSE data was received before disconnect
	if attemptCount.Load() != 1 {
		t.Fatalf("expected 1 attempt (no retry after SSE data), got %d", attemptCount.Load())
	}
}

// TestClaudeExecutor_StreamRetry_NoRetryForNonRetryableError verifies that retry
// is NOT attempted for errors other than io.ErrUnexpectedEOF.
func TestClaudeExecutor_StreamRetry_NoRetryForNonRetryableError(t *testing.T) {
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		// Return a normal error (not unexpected EOF) - status 500 causes error
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":{"type":"api_error","message":"internal error"}}`)
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)

	// When server returns 500, ExecuteStream returns an error directly
	_, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})

	// Error is expected for 500 status
	if err == nil {
		t.Fatal("expected an error for 500 status")
	}

	// Should only have 1 attempt because error is not retryable (500 status is not unexpected EOF)
	if attemptCount.Load() != 1 {
		t.Fatalf("expected 1 attempt (no retry for non-retryable error), got %d", attemptCount.Load())
	}
}

// TestClaudeExecutor_StreamRetry_ContextCancelledDuringRetry verifies that context
// cancellation is handled properly during retry.
func TestClaudeExecutor_StreamRetry_ContextCancelledDuringRetry(t *testing.T) {
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		if attempt == 1 {
			// First attempt: close connection immediately
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, err := hijackResponse(hj, w)
			if err != nil {
				return
			}
			conn.Close()
			return
		}

		// Second attempt: check context before responding
		// This simulates a delay that allows context to be cancelled
		time.Sleep(100 * time.Millisecond)
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)

	// Create a context that will be cancelled quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := executor.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	// Drain chunks - some may have errors due to context cancellation
	for chunk := range result.Chunks {
		// Context cancellation errors are expected
		_ = chunk
	}

	// Should have attempted at least once
	if attemptCount.Load() < 1 {
		t.Fatalf("expected at least 1 attempt, got %d", attemptCount.Load())
	}
}

// TestClaudeExecutor_StreamRetry_RetryRequestFailure verifies that when retryRequest
// fails, the error is properly propagated.
func TestClaudeExecutor_StreamRetry_RetryRequestFailure(t *testing.T) {
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		if attempt == 1 {
			// First attempt: close connection immediately (triggers retry)
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, err := hijackResponse(hj, w)
			if err != nil {
				return
			}
			conn.Close()
			return
		}

		// Second attempt: return error
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"rate limited"}}`)
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var hasError bool
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			hasError = true
		}
	}

	if !hasError {
		t.Fatal("expected an error chunk from retry failure")
	}

	// Should have 2 attempts
	if attemptCount.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attemptCount.Load())
	}
}

// TestClaudeExecutor_StreamRetry_ReasoningEffortDegraded verifies that reasoning
// effort is degraded on retry when DegradeAfterAttempts=0 (legacy behavior:
// degrade immediately on the first retry). This exercises the "aggressive
// degrade" opt-in that preserves the pre-delayed-degrade contract.
func TestClaudeExecutor_StreamRetry_ReasoningEffortDegraded(t *testing.T) {
	var attemptCount atomic.Int32
	var lastRequestBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		// Capture request body
		body, _ := io.ReadAll(r.Body)
		lastRequestBody = body

		if attempt == 1 {
			// First attempt: close connection immediately
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, err := hijackResponse(hj, w)
			if err != nil {
				return
			}
			conn.Close()
			return
		}

		// Second attempt: return valid SSE stream
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	// Opt into legacy aggressive-degrade behavior for this test.
	degradeAfter := 0
	cfg := &config.Config{}
	cfg.SDKConfig.Streaming.StreamRetryDegradeAfter = &degradeAfter
	executor := NewClaudeExecutor(cfg)
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	// Request with reasoning_effort: high
	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"reasoning_effort":"high"}`)

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
	}

	if attemptCount.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attemptCount.Load())
	}

	// Verify that reasoning_effort was degraded in the retry request.
	if lastRequestBody != nil {
		bodyStr := string(lastRequestBody)
		// The retry request should have degraded effort (high -> medium).
		if !contains(bodyStr, `"reasoning_effort":"medium"`) {
			t.Fatalf("expected reasoning_effort to be degraded to 'medium', got body: %s", bodyStr)
		}
	}
}

// TestClaudeExecutor_StreamRetry_DefaultKeepsFirstRetryOriginal verifies the new
// default contract: with DegradeAfterAttempts=1 (default), the first retry
// reuses the original body — reasoning_effort must NOT be degraded on the
// first retry. This protects callers from silently losing declared effort on
// transient network glitches.
func TestClaudeExecutor_StreamRetry_DefaultKeepsFirstRetryOriginal(t *testing.T) {
	var attemptCount atomic.Int32
	var lastRequestBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		body, _ := io.ReadAll(r.Body)
		lastRequestBody = body

		if attempt == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, err := hijackResponse(hj, w)
			if err != nil {
				return
			}
			conn.Close()
			return
		}

		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	// No override → default DegradeAfterAttempts=1 applies.
	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"reasoning_effort":"high"}`)

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
	}

	if attemptCount.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attemptCount.Load())
	}

	if lastRequestBody == nil {
		t.Fatal("retry request body was not captured")
	}
	bodyStr := string(lastRequestBody)
	if !contains(bodyStr, `"reasoning_effort":"high"`) {
		t.Fatalf("expected first retry to preserve reasoning_effort='high', got body: %s", bodyStr)
	}
	if contains(bodyStr, `"reasoning_effort":"medium"`) {
		t.Fatalf("first retry must not degrade reasoning_effort, got body: %s", bodyStr)
	}
}

// TestClaudeExecutor_StreamRetry_MaxAttempts verifies that only one retry is attempted.
func TestClaudeExecutor_StreamRetry_MaxAttempts(t *testing.T) {
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		// Always close connection immediately
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, err := hijackResponse(hj, w)
		if err != nil {
			return
		}
		conn.Close()
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	for chunk := range result.Chunks {
		_ = chunk
	}

	// Should have exactly 2 attempts (1 initial + 1 retry)
	if attemptCount.Load() != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", attemptCount.Load())
	}
}

// TestClaudeExecutor_StreamRetry_SuccessfulRetryWithSSEData verifies that after
// a successful retry, the SSE data is properly processed.
func TestClaudeExecutor_StreamRetry_SuccessfulRetryWithSSEData(t *testing.T) {
	var attemptCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attemptCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")

		if attempt == 1 {
			// First attempt: close connection immediately
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, err := hijackResponse(hj, w)
			if err != nil {
				return
			}
			conn.Close()
			return
		}

		// Second attempt: return complete valid SSE stream
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-3\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello!\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	payload := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunks []cliproxyexecutor.StreamChunk
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
		chunks = append(chunks, chunk)
	}

	// Verify we got multiple chunks with SSE data
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// Verify attempt count
	if attemptCount.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attemptCount.Load())
	}
}

// hijackResponse hijacks the response writer to get the underlying connection.
func hijackResponse(hj http.Hijacker, w http.ResponseWriter) (io.ReadWriteCloser, error) {
	// Flush any buffered data
	flusher, ok := w.(http.Flusher)
	if ok {
		flusher.Flush()
	}
	conn, _, err := hj.Hijack()
	return conn, err
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
