package executor

// Dimension D — streaming reasoning-chain fault injection.
//
// These tests characterize how each provider's ExecuteStream reasoning pipeline
// behaves under adverse upstream conditions that directly affect reasoning-chain
// integrity:
//
//   - mid-stream truncation (upstream closes the TCP body before a terminal
//     event) — "is the reasoning process silently truncated?"
//   - client disconnect (ctx cancelled while producing) — "does the producer
//     goroutine leak?"
//   - malformed chunk in the middle of a reasoning stream — "does a parse error
//     corrupt/abort the whole chain?"
//
// They are CHARACTERIZATION tests: they lock in the current, observed behavior
// (including the provider asymmetry documented in the accompanying plan) so that
// any future change to the streaming reasoning path is caught. Where a provider
// silently treats an incomplete stream as success, the test asserts that fact
// explicitly and references the plan, rather than pretending the behavior is
// correct.
//
// All tests are hermetic: they use httptest / a fake body and never touch the
// network or consume tokens.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

// drainStream consumes a stream result, returning whether any chunk carried a
// terminal error and how many payload chunks were produced.
func drainStream(t *testing.T, result *cliproxyexecutor.StreamResult) (sawErr bool, payloads int) {
	t.Helper()
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			sawErr = true
			continue
		}
		payloads++
	}
	return sawErr, payloads
}

// waitGoroutinesSettle polls until the goroutine count drops to at most baseline+slack
// or the deadline elapses. Returns the final observed count.
func waitGoroutinesSettle(baseline, slack int, deadline time.Duration) int {
	end := time.Now().Add(deadline)
	var n int
	for time.Now().Before(end) {
		n = runtime.NumGoroutine()
		if n <= baseline+slack {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n
}

// --- Gemini: mid-stream truncation is surfaced as an error ------------------

// TestGeminiStream_TruncatedUpstreamSurfacesError verifies that the Gemini
// streaming path now detects an incomplete reasoning chain: when the upstream
// closes the body after emitting partial reasoning/content WITHOUT a terminal
// finishReason, the executor surfaces a stream error instead of forging a
// synthetic [DONE]. This closes the silent-truncation gap that previously left
// Gemini as the weakest provider (contrast: Codex codexIncompleteStreamError).
func TestGeminiStream_TruncatedUpstreamSurfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Emit a partial candidate WITHOUT any finishReason, then close the body.
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"partial reasoning"}]}}]}` + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		// Handler returns here -> body closed mid-stream, no finishReason seen.
	}))
	defer server.Close()

	exec := NewGeminiExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "test-key",
		"base_url": server.URL,
	}}

	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gemini-2.5-pro",
		Payload: []byte(`{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("gemini"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	sawErr, payloads := drainStream(t, result)
	// The partial reasoning payload is still forwarded (the client keeps what was
	// produced), but the missing terminal must now surface as an error.
	if payloads == 0 {
		t.Fatalf("expected the partial reasoning payload to be forwarded, got 0")
	}
	if !sawErr {
		t.Fatalf("gemini stream silently completed a truncated reasoning chain; "+
			"expected an incomplete-stream error. payloads=%d", payloads)
	}
}

// TestGeminiStream_FinishReasonDoesNotErrorOnClose verifies the complement: a
// well-formed stream that ends with a non-empty finishReason must NOT be flagged
// as truncated, guarding against false positives from the new detection.
func TestGeminiStream_FinishReasonDoesNotErrorOnClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}` + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer server.Close()

	exec := NewGeminiExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "test-key",
		"base_url": server.URL,
	}}

	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gemini-2.5-pro",
		Payload: []byte(`{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("gemini"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	sawErr, payloads := drainStream(t, result)
	if sawErr {
		t.Fatal("gemini stream with a terminal finishReason must not surface a truncation error (false positive)")
	}
	if payloads == 0 {
		t.Fatal("expected reasoning payloads to be forwarded")
	}
}

// --- Gemini: client disconnect must not leak the producer goroutine ---------

// TestGeminiStream_ClientDisconnectDoesNotLeakGoroutine verifies that cancelling
// the context while the upstream is still streaming causes the producer
// goroutine to exit (via the ctx.Done() guards on every channel send), rather
// than blocking forever on an unread channel.
func TestGeminiStream_ClientDisconnectDoesNotLeakGoroutine(t *testing.T) {
	// Upstream keeps sending chunks slowly and never terminates on its own.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for {
			select {
			case <-release:
				return
			default:
			}
			if _, errWrite := w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"x"}]}}]}` + "\n\n")); errWrite != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer server.Close()
	defer close(release)

	runtime.GC()
	baseline := runtime.NumGoroutine()

	exec := NewGeminiExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "test-key",
		"base_url": server.URL,
	}}

	ctx, cancel := context.WithCancel(context.Background())
	result, err := exec.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "gemini-2.5-pro",
		Payload: []byte(`{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("gemini"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	// Read one chunk to prove the producer is live, then cancel (client hang-up).
	select {
	case <-result.Chunks:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for first stream chunk")
	}
	cancel()

	// Drain remaining chunks; the channel must close after cancellation.
	drained := make(chan struct{})
	go func() {
		//nolint:revive // intentional drain
		for range result.Chunks {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(3 * time.Second):
		t.Fatal("stream channel did not close after context cancellation (producer goroutine leak)")
	}

	runtime.GC()
	// Allow the producer goroutine and the httptest handler goroutine to unwind.
	if got := waitGoroutinesSettle(baseline, 2, 3*time.Second); got > baseline+2 {
		t.Fatalf("goroutine leak suspected: baseline=%d, after=%d", baseline, got)
	}
}

// --- Codex: incomplete stream is surfaced as a retryable error --------------

// TestCodexStream_IncompleteIsRequestScopedError contrasts Codex against Gemini:
// an upstream that closes before response.completed yields a request-scoped
// error (codexIncompleteStreamError, 408) so the reasoning turn can be retried
// rather than silently truncated. This asymmetry is the core finding of the
// reasoning-stability analysis.
func TestCodexStream_IncompleteIsRequestScopedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Emit response.created only, then close: reasoning chain never completes.
		_, _ = w.Write([]byte(`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.5"}}` + "\n\n"))
	}))
	defer server.Close()

	exec := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL,
		"api_key":  "test",
	}}

	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5.5",
		Payload: []byte(`{"model":"gpt-5.5","input":"hello"}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	var streamErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			streamErr = chunk.Err
		}
	}
	if streamErr == nil {
		t.Fatal("expected codex incomplete-stream error, got nil (reasoning chain would be silently truncated)")
	}
	if got := statusCodeFromTestError(t, streamErr); got != http.StatusRequestTimeout {
		t.Fatalf("status code = %d, want %d (request-scoped incomplete)", got, http.StatusRequestTimeout)
	}
	assertRequestScopedTestError(t, streamErr)
}

// --- Codex: transport EOF before terminal is retryable, not a fake success --

// TestCodexStream_TransportEOFBeforeTerminalIsRequestScoped ensures that a raw
// transport failure (unexpected EOF) mid-reasoning-stream is surfaced as a
// request-scoped error rather than dropped, protecting reasoning-chain
// completeness at the socket level.
func TestCodexStream_TransportEOFBeforeTerminalIsRequestScoped(t *testing.T) {
	created := []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.5\"}}\n\n")
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(io.MultiReader(bytes.NewReader(created), unexpectedEOFReader{})),
			Request:    req,
		}, nil
	}))

	exec := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": "http://codex.test",
		"api_key":  "test",
	}}

	result, err := exec.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-5.5",
		Payload: []byte(`{"model":"gpt-5.5","input":"hello"}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	var streamErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			streamErr = chunk.Err
		}
	}
	if streamErr == nil {
		t.Fatal("expected request-scoped transport error before terminal event")
	}
	if got := statusCodeFromTestError(t, streamErr); got != http.StatusRequestTimeout {
		t.Fatalf("status code = %d, want %d", got, http.StatusRequestTimeout)
	}
	assertRequestScopedTestError(t, streamErr)
}

// --- Claude: translated path forwards malformed chunk without aborting chain -

// TestClaudeStream_MalformedChunkDoesNotAbortStream characterizes the Claude
// translated streaming path: a syntactically invalid SSE data line in the
// middle of the reasoning stream must not panic or prematurely terminate the
// producer; surrounding valid chunks continue to flow. This guards the
// stateful TranslateStream `param` accumulator against corruption from a single
// bad chunk.
func TestClaudeStream_MalformedChunkDoesNotAbortStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			_, _ = w.Write([]byte(s))
			if flusher != nil {
				flusher.Flush()
			}
		}
		write("event: message_start\n")
		write(`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5","role":"assistant","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n")
		write("event: content_block_delta\n")
		write(`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"step 1"}}` + "\n\n")
		// Malformed JSON in the middle of the reasoning stream.
		write("event: content_block_delta\n")
		write(`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta",` + "\n\n")
		write("event: content_block_delta\n")
		write(`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"step 2"}}` + "\n\n")
		write("event: message_delta\n")
		write(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}` + "\n\n")
		write("event: message_stop\n")
		write(`data: {"type":"message_stop"}` + "\n\n")
	}))
	defer server.Close()

	exec := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL,
		"api_key":  "test",
	}}

	// Use claude source format -> claude target => direct passthrough path,
	// which forwards complete SSE events verbatim (malformed line included).
	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-sonnet-4-5",
		Payload: []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	// Must not panic and must forward payloads; the malformed chunk should not
	// prevent later valid reasoning chunks from being delivered.
	_, payloads := drainStream(t, result)
	if payloads == 0 {
		t.Fatal("expected reasoning payloads to be forwarded despite a malformed middle chunk")
	}
}

// --- xAI: mid-stream truncation is now surfaced as an error ------------------

// TestXAIStream_TruncatedUpstreamSurfacesError verifies that after the
// completion-detection fix, the xAI streaming path reports an incomplete-stream
// error when the upstream closes the body without emitting response.completed,
// instead of forging a silent successful close that would drop an incomplete
// reasoning chain.
func TestXAIStream_TruncatedUpstreamSurfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Emit response.created plus a partial reasoning item, then close the body
		// WITHOUT response.completed.
		_, _ = w.Write([]byte(`data: {"type":"response.created","response":{"id":"resp_1","model":"grok-4.5"}}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_item.done","item":{"type":"reasoning","summary":[]},"output_index":0}` + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer server.Close()

	exec := NewXAIExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider:   "xai",
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "xai-token"},
	}

	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "grok-4.5",
		Payload: []byte(`{"model":"grok-4.5","input":"hi"}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	var streamErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			streamErr = chunk.Err
		}
	}
	if streamErr == nil {
		t.Fatal("expected xai incomplete-stream error, got nil (reasoning chain would be silently truncated)")
	}
	if got := statusCodeFromTestError(t, streamErr); got != http.StatusBadGateway {
		t.Fatalf("status code = %d, want %d (incomplete stream)", got, http.StatusBadGateway)
	}
	// xAI's incomplete-stream error is deliberately NOT request-scoped (unlike
	// Codex's 408 codexIncompleteStreamError): the conductor decides retry vs
	// surface based on bootstrap state, not on the error type. Lock this in so
	// a future change doesn't accidentally make xAI retry like Codex.
	assertNotRequestScopedTestError(t, streamErr)
}

// TestXAIStream_CompletedDoesNotErrorOnClose is the counterpart guard: a stream
// that ends WITH response.completed must close cleanly with no error, so the
// completion-detection fix does not create false positives on well-formed
// reasoning streams.
func TestXAIStream_CompletedDoesNotErrorOnClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","created_at":0,"status":"completed","model":"grok-4.5","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"))
	}))
	defer server.Close()

	exec := NewXAIExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider:   "xai",
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "xai-token"},
	}

	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "grok-4.5",
		Payload: []byte(`{"model":"grok-4.5","input":"hi"}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	sawErr, payloads := drainStream(t, result)
	if sawErr {
		t.Fatal("well-formed xai stream (response.completed) must not surface an error")
	}
	if payloads == 0 {
		t.Fatal("expected completed reasoning payloads to be forwarded")
	}
}

func TestXAIStream_IncompleteDoesNotErrorOnClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"response.incomplete","response":{"id":"resp_1","object":"response","created_at":0,"status":"incomplete","model":"grok-4.5","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"))
	}))
	defer server.Close()

	exec := NewXAIExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider:   "xai",
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "xai-token"},
	}

	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "grok-4.5",
		Payload: []byte(`{"model":"grok-4.5","input":"hi"}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	sawErr, payloads := drainStream(t, result)
	if sawErr {
		t.Fatal("well-formed xai stream (response.incomplete) must not surface a truncation error")
	}
	if payloads == 0 {
		t.Fatal("expected incomplete response payloads to be forwarded")
	}
}
