package helps

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeNonStreamContentOrder_ReorderByIndex(t *testing.T) {
	raw := []byte(`{"type":"message","content":[` +
		`{"type":"text","text":"c","index":2},` +
		`{"type":"text","text":"a","index":0},` +
		`{"type":"text","text":"b","index":1}],` +
		`"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}`)
	out := NormalizeNonStreamContentOrder(raw)
	blocks := gjson.GetBytes(out, "content").Array()
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	want := []string{"a", "b", "c"}
	for i, b := range blocks {
		if b.Get("text").String() != want[i] {
			t.Fatalf("block %d text = %q, want %q", i, b.Get("text").String(), want[i])
		}
	}
}

func TestNormalizeNonStreamContentOrder_ReorderByType(t *testing.T) {
	raw := []byte(`{"type":"message","content":[` +
		`{"type":"tool_use","id":"t1","name":"n","input":{},"index":0},` +
		`{"type":"thinking","thinking":"x","index":1},` +
		`{"type":"text","text":"hello","index":2}],` +
		`"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(raw)
	blocks := gjson.GetBytes(out, "content").Array()
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	wantTypes := []string{"text", "thinking", "tool_use"}
	for i, b := range blocks {
		if b.Get("type").String() != wantTypes[i] {
			t.Fatalf("block %d type = %q, want %q", i, b.Get("type").String(), wantTypes[i])
		}
	}
}

func TestNormalizeNonStreamContentOrder_StopReasonMissingWithToolUse(t *testing.T) {
	raw := []byte(`{"type":"message","content":[` +
		`{"type":"text","text":"hi"},` +
		`{"type":"tool_use","id":"t1","name":"n","input":{}}],` +
		`"usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(raw)
	sr := gjson.GetBytes(out, "stop_reason")
	if !sr.Exists() {
		t.Fatal("stop_reason missing after normalization")
	}
	if sr.String() != "tool_use" {
		t.Fatalf("stop_reason = %q, want \"tool_use\"", sr.String())
	}
}

func TestNormalizeNonStreamContentOrder_StopReasonMissingNoToolUse(t *testing.T) {
	raw := []byte(`{"type":"message","content":[` +
		`{"type":"text","text":"hi"}],` +
		`"usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(raw)
	sr := gjson.GetBytes(out, "stop_reason")
	if !sr.Exists() {
		t.Fatal("stop_reason missing after normalization")
	}
	if sr.String() != "stop" {
		t.Fatalf("stop_reason = %q, want \"stop\"", sr.String())
	}
}

func TestNormalizeNonStreamContentOrder_StopReasonPreserved(t *testing.T) {
	raw := []byte(`{"type":"message","content":[` +
		`{"type":"text","text":"hi"}],` +
		`"stop_reason":"length","usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(raw)
	sr := gjson.GetBytes(out, "stop_reason")
	if !sr.Exists() {
		t.Fatal("stop_reason missing after normalization")
	}
	if sr.String() != "length" {
		t.Fatalf("stop_reason = %q, want \"length\" (preserved)", sr.String())
	}
}

func TestNormalizeNonStreamContentOrder_UsagePreserved(t *testing.T) {
	raw := []byte(`{"type":"message","content":[` +
		`{"type":"text","text":"hi"}],` +
		`"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":20}}`)
	out := NormalizeNonStreamContentOrder(raw)
	usage := gjson.GetBytes(out, "usage")
	if !usage.Exists() {
		t.Fatal("usage missing after normalization")
	}
	if got := usage.Get("input_tokens").Int(); got != 10 {
		t.Fatalf("usage.input_tokens = %d, want 10", got)
	}
	if got := usage.Get("output_tokens").Int(); got != 20 {
		t.Fatalf("usage.output_tokens = %d, want 20", got)
	}
}

func TestNormalizeNonStreamContentOrder_NoContentArray(t *testing.T) {
	raw := []byte(`{"type":"message","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(raw)
	// Should be returned as-is (stop_reason already set).
	if gjson.GetBytes(out, "stop_reason").String() != "end_turn" {
		t.Fatalf("stop_reason changed unexpectedly: %q", gjson.GetBytes(out, "stop_reason").String())
	}
	if gjson.GetBytes(out, "content").Exists() {
		t.Fatal("content array should not exist")
	}
}

func TestNormalizeNonStreamContentOrder_EmptyContent(t *testing.T) {
	raw := []byte(`{"type":"message","content":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(raw)
	blocks := gjson.GetBytes(out, "content").Array()
	if len(blocks) != 0 {
		t.Fatalf("expected empty content array, got %d blocks", len(blocks))
	}
	// stop_reason should still be filled (default "stop" since no tool_use).
	sr := gjson.GetBytes(out, "stop_reason")
	if sr.String() != "stop" {
		t.Fatalf("stop_reason = %q, want \"stop\"", sr.String())
	}
}

func TestNormalizeNonStreamContentOrder_NonClaudeFormat(t *testing.T) {
	// OpenAI chat completion format - root has no "type":"message".
	raw := []byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	out := NormalizeNonStreamContentOrder(raw)
	// Should be returned byte-for-byte unchanged.
	if string(out) != string(raw) {
		t.Fatalf("non-Claude response was modified\n got: %s\nwant: %s", out, raw)
	}
}

func TestNormalizeNonStreamContentOrder_InvalidJSON(t *testing.T) {
	raw := []byte(`{"type":"message","content":[`)
	out := NormalizeNonStreamContentOrder(raw)
	if string(out) != string(raw) {
		t.Fatalf("invalid JSON was modified\n got: %s\nwant: %s", out, raw)
	}
}

func TestNormalizeNonStreamContentOrder_StableSort(t *testing.T) {
	// Multiple text blocks without index: original order must be preserved.
	raw := []byte(`{"type":"message","content":[` +
		`{"type":"text","text":"first"},` +
		`{"type":"text","text":"second"},` +
		`{"type":"text","text":"third"}],` +
		`"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(raw)
	blocks := gjson.GetBytes(out, "content").Array()
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	want := []string{"first", "second", "third"}
	for i, b := range blocks {
		if b.Get("text").String() != want[i] {
			t.Fatalf("block %d text = %q, want %q (stable order)", i, b.Get("text").String(), want[i])
		}
	}
}

func TestNormalizeNonStreamContentOrder_NullStopReason(t *testing.T) {
	raw := []byte(`{"type":"message","content":[` +
		`{"type":"text","text":"hi"}],` +
		`"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(raw)
	sr := gjson.GetBytes(out, "stop_reason")
	if !sr.Exists() {
		t.Fatal("stop_reason missing after normalization")
	}
	if sr.Type == gjson.Null {
		t.Fatal("stop_reason still null after normalization")
	}
	if sr.String() != "stop" {
		t.Fatalf("stop_reason = %q, want \"stop\"", sr.String())
	}
}
