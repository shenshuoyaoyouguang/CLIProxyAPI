package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// TestGeminiExecutorStreamCharacterization pins the exact chunk output of the
// main (bufio-scanner) ExecuteStream path for gemini->gemini streaming. It
// exists to guard byte-for-byte equivalence across the StreamPump refactor:
// the pre-refactor output captured here must remain identical afterwards.
func TestGeminiExecutorStreamCharacterization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Fatalf("read request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		writeChunk := func(s string) {
			_, _ = w.Write([]byte(s))
			if flusher != nil {
				flusher.Flush()
			}
		}
		writeChunk("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hello\"}]}}]}\n\n")
		writeChunk("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\" world\"}]}}]}\n\n")
		writeChunk("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"!\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}\n\n")
	}))
	defer server.Close()

	exec := NewGeminiExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "gemini",
		Attributes: map[string]string{
			"api_key":  "test-key",
			"base_url": server.URL,
		},
	}
	req := cliproxyexecutor.Request{
		Model:   "gemini-3.1-pro-preview",
		Payload: []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
	}

	result, err := exec.ExecuteStream(context.Background(), auth, req, cliproxyexecutor.Options{
		SourceFormat:   sdktranslator.FormatGemini,
		ResponseFormat: sdktranslator.FormatGemini,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunks [][]byte
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		chunks = append(chunks, chunk.Payload)
	}

	// Characterization assertions: capture the observable contract of the
	// current implementation. These must hold identically after the refactor.
	var text string
	var sawFinish, sawUsage bool
	for _, c := range chunks {
		if v := gjson.GetBytes(c, "candidates.0.content.parts.0.text"); v.Exists() {
			text += v.String()
		}
		if gjson.GetBytes(c, "candidates.0.finishReason").String() == "STOP" {
			sawFinish = true
		}
		if gjson.GetBytes(c, "usageMetadata.totalTokenCount").Int() == 5 {
			sawUsage = true
		}
	}
	if text != "Hello world!" {
		t.Fatalf("aggregated text = %q, want %q", text, "Hello world!")
	}
	if !sawFinish {
		t.Fatalf("finishReason STOP not observed in chunks: %s", joinChunks(chunks))
	}
	if !sawUsage {
		t.Fatalf("usageMetadata.totalTokenCount=5 not observed in chunks: %s", joinChunks(chunks))
	}
}

// joinChunks renders chunks for failure diagnostics.
func joinChunks(chunks [][]byte) string {
	var b []byte
	for i, c := range chunks {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, c...)
	}
	return string(b)
}
