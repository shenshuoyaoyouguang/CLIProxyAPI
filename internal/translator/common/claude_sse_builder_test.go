package common

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestClaudeSSEBuilder_AllowsConcurrentBlocksAndTerminalIsIdempotent(t *testing.T) {
	b := NewClaudeSSEBuilder(ClaudeSSEBuilderConfig{})

	var out []byte
	out = b.AppendMessageStart(out, ClaudeMessageStartParams{ID: "msg_1", Model: "claude-test"})

	var toolA, toolB int
	out, toolA = b.AppendContentBlockStart(out, []byte(`{"type":"tool_use","id":"call_a","name":"tool_a","input":{}}`))
	out, toolB = b.AppendContentBlockStart(out, []byte(`{"type":"tool_use","id":"call_b","name":"tool_b","input":{}}`))
	if toolA != 0 || toolB != 1 {
		t.Fatalf("block indexes = %d,%d; want 0,1", toolA, toolB)
	}

	out = b.AppendInputJSONDelta(out, toolA, `{"p":1}`)
	out = b.AppendInputJSONDelta(out, toolB, `{"q":2}`)
	out = b.AppendTerminal(out, ClaudeMessageDeltaParams{
		StopReason: "tool_use",
		Usage:      ClaudeUsage{InputTokens: 3, OutputTokens: 4},
	})
	out = b.AppendTerminal(out, ClaudeMessageDeltaParams{
		StopReason: "tool_use",
		Usage:      ClaudeUsage{InputTokens: 3, OutputTokens: 4},
	})

	if got := strings.Count(string(out), `"type":"content_block_stop"`); got != 2 {
		t.Fatalf("content_block_stop count = %d, want 2\n%s", got, out)
	}
	if got := strings.Count(string(out), `"type":"message_delta"`); got != 1 {
		t.Fatalf("message_delta count = %d, want 1\n%s", got, out)
	}
	if got := strings.Count(string(out), `"type":"message_stop"`); got != 1 {
		t.Fatalf("message_stop count = %d, want 1\n%s", got, out)
	}
	if bytes.Index(out, []byte(`"index":0,"content_block":{"type":"tool_use"`)) > bytes.Index(out, []byte(`"index":1,"content_block":{"type":"tool_use"`)) {
		t.Fatalf("tool blocks emitted out of order:\n%s", out)
	}
}

func TestClaudeSSEBuilder_PreservesRawContentBlockAndTrailingNewlines(t *testing.T) {
	b := NewClaudeSSEBuilder(ClaudeSSEBuilderConfig{TrailingNewlines: 3})

	var out []byte
	out, _ = b.AppendContentBlockStart(out, []byte(`{"citations":[],"type":"text","text":""}`))

	if !bytes.Contains(out, []byte(`"content_block":{"citations":[],"type":"text","text":""}`)) {
		t.Fatalf("raw content block shape not preserved:\n%s", out)
	}
	if !bytes.HasSuffix(out, []byte("\n\n\n")) {
		t.Fatalf("frame should preserve configured triple trailing newline: %q", out)
	}
}

func TestClaudeSSEBuilder_MessageDeltaUsageExtras(t *testing.T) {
	b := NewClaudeSSEBuilder(ClaudeSSEBuilderConfig{})

	out := b.AppendMessageDelta(nil, ClaudeMessageDeltaParams{
		StopReason:   "stop_sequence",
		StopSequence: "\nEND",
		Usage: ClaudeUsage{
			InputTokens:       10,
			OutputTokens:      5,
			CacheReadTokens:   3,
			WebSearchRequests: 2,
		},
	})

	payload := ssePayloadForTest(t, out, SSEEventMessageDelta)
	if got := gjson.GetBytes(payload, "delta.stop_reason").String(); got != "stop_sequence" {
		t.Fatalf("stop_reason = %q", got)
	}
	if got := gjson.GetBytes(payload, "delta.stop_sequence").String(); got != "\nEND" {
		t.Fatalf("stop_sequence = %q", got)
	}
	if got := gjson.GetBytes(payload, "usage.cache_read_input_tokens").Int(); got != 3 {
		t.Fatalf("cache_read_input_tokens = %d", got)
	}
	if got := gjson.GetBytes(payload, "usage.server_tool_use.web_search_requests").Int(); got != 2 {
		t.Fatalf("web_search_requests = %d", got)
	}
}

func ssePayloadForTest(t *testing.T, frame []byte, event string) []byte {
	t.Helper()
	for _, ev := range parseSSEEvents(frame) {
		if ev.Type == event {
			return ev.Data
		}
	}
	t.Fatalf("event %s not found in\n%s", event, frame)
	return nil
}
