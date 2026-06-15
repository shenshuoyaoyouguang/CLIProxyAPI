package helps

import (
	"testing"
)

func TestExtractResponseText_ChatCompletions(t *testing.T) {
	body := []byte(`{"id":"chatcmpl_1","choices":[{"index":0,"message":{"role":"assistant","content":"Hello, world!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	got := ExtractResponseText(body)
	if got != "Hello, world!" {
		t.Fatalf("ExtractResponseText = %q, want %q", got, "Hello, world!")
	}
}

func TestExtractResponseText_ChatCompletionsMultiChoice(t *testing.T) {
	body := []byte(`{"id":"chatcmpl_1","choices":[
		{"index":0,"message":{"role":"assistant","content":"First"},"finish_reason":"stop"},
		{"index":1,"message":{"role":"assistant","content":"Second"},"finish_reason":"stop"}
	]}`)
	got := ExtractResponseText(body)
	if got != "First\nSecond" {
		t.Fatalf("ExtractResponseText = %q, want %q", got, "First\nSecond")
	}
}

func TestExtractResponseText_Responses(t *testing.T) {
	body := []byte(`{"id":"resp_1","response":{"output":[
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello from responses"}]}
	]}}`)
	got := ExtractResponseText(body)
	if got != "Hello from responses" {
		t.Fatalf("ExtractResponseText = %q, want %q", got, "Hello from responses")
	}
}

func TestExtractResponseText_ResponsesMultiContent(t *testing.T) {
	body := []byte(`{"id":"resp_1","response":{"output":[
		{"type":"message","role":"assistant","content":[
			{"type":"output_text","text":"Part A"},
			{"type":"output_text","text":"Part B"}
		]}
	]}}`)
	got := ExtractResponseText(body)
	if got != "Part A\nPart B" {
		t.Fatalf("ExtractResponseText = %q, want %q", got, "Part A\nPart B")
	}
}

func TestExtractResponseText_CompletionsLegacy(t *testing.T) {
	body := []byte(`{"id":"cmpl_1","choices":[{"index":0,"text":"Legacy completion text"}]}`)
	got := ExtractResponseText(body)
	if got != "Legacy completion text" {
		t.Fatalf("ExtractResponseText = %q, want %q", got, "Legacy completion text")
	}
}

func TestExtractResponseText_Empty(t *testing.T) {
	if got := ExtractResponseText(nil); got != "" {
		t.Fatalf("ExtractResponseText(nil) = %q, want empty", got)
	}
	if got := ExtractResponseText([]byte(`{}`)); got != "" {
		t.Fatalf("ExtractResponseText({}) = %q, want empty", got)
	}
	if got := ExtractResponseText([]byte(`{"choices":[]}`)); got != "" {
		t.Fatalf("ExtractResponseText(empty choices) = %q, want empty", got)
	}
}

func TestEstimateResponseOutputTokens(t *testing.T) {
	body := []byte(`{"id":"chatcmpl_1","choices":[{"index":0,"message":{"role":"assistant","content":"short text"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	got := EstimateResponseOutputTokens(body)
	if got < 1 || got > 5 {
		t.Fatalf("EstimateResponseOutputTokens = %d, want roughly 2-3 tokens for 'short text'", got)
	}
}

func TestEstimateResponseOutputTokens_EmptyBody(t *testing.T) {
	if got := EstimateResponseOutputTokens(nil); got != 0 {
		t.Fatalf("EstimateResponseOutputTokens(nil) = %d, want 0", got)
	}
	if got := EstimateResponseOutputTokens([]byte(`{}`)); got != 0 {
		t.Fatalf("EstimateResponseOutputTokens({}) = %d, want 0", got)
	}
}

func TestExtractResponseText_ContentArray(t *testing.T) {
	// Chat Completions with array-style content (e.g. vision/tool responses)
	body := []byte(`{"id":"chatcmpl_1","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"text","text":"Hello from array"}]},"finish_reason":"stop"}]}`)
	got := ExtractResponseText(body)
	if got != "Hello from array" {
		t.Fatalf("ExtractResponseText = %q, want %q", got, "Hello from array")
	}
}

func TestExtractResponseText_ContentArrayMultiPart(t *testing.T) {
	// Multiple text parts in content array
	body := []byte(`{"id":"chatcmpl_1","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"text","text":"Part 1"},{"type":"text","text":"Part 2"}]},"finish_reason":"stop"}]}`)
	got := ExtractResponseText(body)
	if got != "Part 1\nPart 2" {
		t.Fatalf("ExtractResponseText = %q, want %q", got, "Part 1\nPart 2")
	}
}

func TestExtractResponseText_DeltaContent(t *testing.T) {
	// Streaming chunk format with delta.content
	body := []byte(`{"id":"chunk_1","choices":[{"index":0,"delta":{"content":"streamed text"},"finish_reason":null}]}`)
	got := ExtractResponseText(body)
	if got != "streamed text" {
		t.Fatalf("ExtractResponseText = %q, want %q", got, "streamed text")
	}
}

func TestExtractResponseText_InvalidJSON(t *testing.T) {
	if got := ExtractResponseText([]byte(`not json`)); got != "" {
		t.Fatalf("ExtractResponseText(invalid json) = %q, want empty", got)
	}
}
