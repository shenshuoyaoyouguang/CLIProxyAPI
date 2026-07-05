package helps

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// This file contains unit tests for the protocol compliance fixes made in
// the latest round of changes. Each test targets a specific fix.

// ---------- SSE duplicate newline fix ----------

// TestCompliance_CRLFStreamNoDuplicateNewlines verifies that a CRLF-terminated
// SSE stream produces output with no residual \r or triple newlines.
func TestCompliance_CRLFStreamNoDuplicateNewlines(t *testing.T) {
	n := NewSSENormalizer()
	// Build a CRLF-terminated SSE stream — this is what triggers duplicate
	// newlines when bytes.Split(..., "\n") leaves a stray '\r'.
	chunk := []byte(
		"event: message_start\r\n" +
			`data: {"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","content":[],"model":"c","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}` + "\r\n" +
			"\r\n" +
			"event: content_block_start\r\n" +
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\r\n" +
			"\r\n" +
			"event: content_block_delta\r\n" +
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}` + "\r\n" +
			"\r\n" +
			"event: content_block_stop\r\n" +
			`data: {"type":"content_block_stop","index":0}` + "\r\n" +
			"\r\n" +
			"event: message_delta\r\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}` + "\r\n" +
			"\r\n" +
			"event: message_stop\r\n" +
			`data: {"type":"message_stop"}` + "\r\n" +
			"\r\n")

	out := n.Process(chunk)
	flush := n.Flush()
	all := append(append([]byte(nil), out...), flush...)

	// No residual \r.
	if bytes.Contains(all, []byte("\r")) {
		t.Errorf("output contains residual \\r: %q", all)
	}
	// No triple newlines (duplicate newline injection).
	if bytes.Contains(all, []byte("\n\n\n")) {
		t.Errorf("output contains triple newlines (duplicate newline): %q", all)
	}
	// All 6 event types must be present.
	for _, et := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
		if !bytes.Contains(all, []byte(et)) {
			t.Errorf("event type %s missing from output", et)
		}
	}
}

// ---------- message_delta buffering (scenario 3) ----------

// TestCompliance_MessageDeltaBufferedUntilContentBlockStop verifies that
// message_delta arriving before content_block_stop is buffered and only
// released after the block is stopped.
func TestCompliance_MessageDeltaBufferedUntilContentBlockStop(t *testing.T) {
	n := NewSSENormalizer()
	chunk := []byte(
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n" +
			// message_delta arrives BEFORE content_block_stop — must be buffered.
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	out := n.Process(chunk)
	flush := n.Flush()
	all := append(append([]byte(nil), out...), flush...)

	cbStopIdx := bytes.Index(all, []byte("event: content_block_stop"))
	mdIdx := bytes.Index(all, []byte("event: message_delta"))
	msIdx := bytes.Index(all, []byte("event: message_stop"))

	if cbStopIdx < 0 || mdIdx < 0 || msIdx < 0 {
		t.Fatalf("missing events in output: %q", all)
	}
	if mdIdx <= cbStopIdx {
		t.Errorf("message_delta (idx=%d) must appear AFTER content_block_stop (idx=%d)", mdIdx, cbStopIdx)
	}
	if msIdx <= mdIdx {
		t.Errorf("message_stop (idx=%d) must appear AFTER message_delta (idx=%d)", msIdx, mdIdx)
	}
}

// ---------- message_stop buffering when message_delta not yet sent ----------

// TestCompliance_MessageStopBufferedUntilMessageDelta verifies that
// message_stop is buffered when message_delta hasn't been emitted yet,
// ensuring protocol order: message_delta before message_stop.
func TestCompliance_MessageStopBufferedUntilMessageDelta(t *testing.T) {
	n := NewSSENormalizer()
	// Both message_delta and message_stop arrive while activeBlocks is non-empty.
	// message_delta gets buffered; message_stop must also be buffered.
	chunk := []byte(
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
			// message_delta and message_stop both arrive before content_block_stop.
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")

	out := n.Process(chunk)
	flush := n.Flush()
	all := append(append([]byte(nil), out...), flush...)

	// Verify order: content_block_stop -> message_delta -> message_stop
	cbStopIdx := bytes.Index(all, []byte("event: content_block_stop"))
	mdIdx := bytes.Index(all, []byte("event: message_delta"))
	msIdx := bytes.Index(all, []byte("event: message_stop"))

	if cbStopIdx < 0 || mdIdx < 0 || msIdx < 0 {
		t.Fatalf("missing events in output: %q", all)
	}
	if mdIdx <= cbStopIdx {
		t.Errorf("message_delta (idx=%d) must appear AFTER content_block_stop (idx=%d)", mdIdx, cbStopIdx)
	}
	if msIdx <= mdIdx {
		t.Errorf("message_stop (idx=%d) must appear AFTER message_delta (idx=%d)", msIdx, mdIdx)
	}
	// Flush should produce nothing — stream is complete.
	if len(flush) != 0 {
		t.Errorf("Flush should produce nothing for complete stream, got %d bytes", len(flush))
	}
}

// ---------- duplicate message_delta deduplication (scenario 11) ----------

// TestCompliance_DuplicateMessageDeltaDeduplicated verifies that a second
// message_delta is suppressed (only usage state is updated, no event emitted).
func TestCompliance_DuplicateMessageDeltaDeduplicated(t *testing.T) {
	n := NewSSENormalizer()
	chunk := []byte(
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			// First message_delta — should be emitted.
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":5,\"output_tokens\":3}}\n\n" +
			// Second message_delta — must be suppressed.
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":5,\"output_tokens\":10}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	out := n.Process(chunk)
	flush := n.Flush()
	all := append(append([]byte(nil), out...), flush...)

	count := strings.Count(string(all), "event: message_delta")
	if count != 1 {
		t.Errorf("expected exactly 1 message_delta, got %d; output: %q", count, all)
	}
	// Flush should not re-emit message_delta.
	if len(flush) != 0 {
		t.Errorf("Flush should produce nothing, got %d bytes: %q", len(flush), flush)
	}
}

// ---------- Flush default stop_reason = "end_turn" (scenario 7) ----------

// TestCompliance_FlushDefaultStopReasonEndTurn verifies that the default
// stop_reason synthesized by Flush is "end_turn", not the invalid "stop".
func TestCompliance_FlushDefaultStopReasonEndTurn(t *testing.T) {
	n := NewSSENormalizer()
	// Stream with content_block but no message_delta and no message_stop.
	chunk := []byte(
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n")
	_ = n.Process(chunk)
	all := n.Flush()

	if !bytes.Contains(all, []byte(`"stop_reason":"end_turn"`)) {
		t.Errorf("Flush should use end_turn as default stop_reason, got: %q", all)
	}
	if bytes.Contains(all, []byte(`"stop_reason":"stop"`)) {
		t.Errorf("Flush should NOT use invalid 'stop' as stop_reason, got: %q", all)
	}
}

// ---------- tool_use stop_reason correction (scenario 8) ----------

// TestCompliance_ToolUseStopReasonCorrected verifies that when a tool_use
// block is present and stop_reason is a non-tool value (e.g. "end_turn"),
// it is corrected to "tool_use".
func TestCompliance_ToolUseStopReasonCorrected(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "end_turn corrected to tool_use",
			input: `{"type":"message","content":[` +
				`{"type":"tool_use","id":"t1","name":"n","input":{}}],` +
				`"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
			expected: "tool_use",
		},
		{
			name: "null corrected to tool_use",
			input: `{"type":"message","content":[` +
				`{"type":"tool_use","id":"t1","name":"n","input":{}}],` +
				`"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}`,
			expected: "tool_use",
		},
		{
			name: "missing filled with tool_use",
			input: `{"type":"message","content":[` +
				`{"type":"tool_use","id":"t1","name":"n","input":{}}],` +
				`"usage":{"input_tokens":1,"output_tokens":1}}`,
			expected: "tool_use",
		},
		{
			name: "tool_use preserved",
			input: `{"type":"message","content":[` +
				`{"type":"tool_use","id":"t1","name":"n","input":{}}],` +
				`"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`,
			expected: "tool_use",
		},
		{
			name: "max_tokens preserved with tool_use",
			input: `{"type":"message","content":[` +
				`{"type":"tool_use","id":"t1","name":"n","input":{}}],` +
				`"stop_reason":"max_tokens","usage":{"input_tokens":1,"output_tokens":1}}`,
			expected: "max_tokens",
		},
		{
			name: "stop_sequence preserved with tool_use",
			input: `{"type":"message","content":[` +
				`{"type":"tool_use","id":"t1","name":"n","input":{}}],` +
				`"stop_reason":"stop_sequence","usage":{"input_tokens":1,"output_tokens":1}}`,
			expected: "stop_sequence",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := NormalizeNonStreamContentOrder([]byte(tc.input))
			sr := gjson.GetBytes(out, "stop_reason")
			if !sr.Exists() {
				t.Fatalf("stop_reason missing: %s", out)
			}
			if sr.String() != tc.expected {
				t.Errorf("stop_reason = %q, want %q", sr.String(), tc.expected)
			}
		})
	}
}

// TestCompliance_EndTurnWithoutToolUseNotCorrected verifies that end_turn
// is preserved when there is NO tool_use block.
func TestCompliance_EndTurnWithoutToolUseNotCorrected(t *testing.T) {
	input := `{"type":"message","content":[` +
		`{"type":"text","text":"hi"}],` +
		`"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	out := NormalizeNonStreamContentOrder([]byte(input))
	sr := gjson.GetBytes(out, "stop_reason")
	if sr.String() != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn (no tool_use present)", sr.String())
	}
}

// ---------- redacted_thinking sorting (scenario 13) ----------

// TestCompliance_RedactedThinkingSortedWithThinking verifies that
// redacted_thinking blocks are sorted at the same priority as thinking
// (before tool_use, after text).
func TestCompliance_RedactedThinkingSortedWithThinking(t *testing.T) {
	input := []byte(`{"type":"message","content":[` +
		`{"type":"tool_use","id":"t1","name":"n","input":{},"index":0},` +
		`{"type":"redacted_thinking","data":"encrypted","index":1},` +
		`{"type":"text","text":"result","index":2},` +
		`{"type":"thinking","thinking":"reasoning","index":3}],` +
		`"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(input)
	blocks := gjson.GetBytes(out, "content").Array()
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(blocks))
	}
	// Expected order: text -> redacted_thinking/thinking (same priority,
	// stable sort by index: redacted_thinking index=1, thinking index=3)
	// -> tool_use
	wantTypes := []string{"text", "redacted_thinking", "thinking", "tool_use"}
	for i, wt := range wantTypes {
		if got := blocks[i].Get("type").String(); got != wt {
			t.Errorf("block[%d].type = %q, want %q", i, got, wt)
		}
	}
}

// TestCompliance_RedactedThinkingBeforeToolUse verifies that even when
// redacted_thinking has a higher index than tool_use, it sorts before it.
func TestCompliance_RedactedThinkingBeforeToolUse(t *testing.T) {
	input := []byte(`{"type":"message","content":[` +
		`{"type":"tool_use","id":"t1","name":"n","input":{},"index":0},` +
		`{"type":"redacted_thinking","data":"enc","index":1}],` +
		`"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	out := NormalizeNonStreamContentOrder(input)
	blocks := gjson.GetBytes(out, "content").Array()
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Get("type").String() != "redacted_thinking" {
		t.Errorf("block[0].type = %q, want redacted_thinking", blocks[0].Get("type").String())
	}
	if blocks[1].Get("type").String() != "tool_use" {
		t.Errorf("block[1].type = %q, want tool_use", blocks[1].Get("type").String())
	}
}

// ---------- message_stop released by releaseReady sets messageStopSent ----------

// TestCompliance_ReleaseReadyMessageStopSetsFlag verifies that when
// releaseReady releases a buffered message_stop, the messageStopSent flag
// is set so Flush does not synthesize a duplicate.
func TestCompliance_ReleaseReadyMessageStopSetsFlag(t *testing.T) {
	n := NewSSENormalizer()
	// Both message_delta and message_stop buffered, then content_block_stop
	// arrives to unblock them.
	chunk := []byte(
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")

	out := n.Process(chunk)
	flush := n.Flush()

	// Flush must be empty — stream was complete.
	if len(flush) != 0 {
		t.Errorf("Flush should produce nothing, got %d chunks: %q", len(flush), flush)
	}
	// Exactly one message_stop in output.
	count := strings.Count(string(out), "event: message_stop")
	if count != 1 {
		t.Errorf("expected 1 message_stop, got %d: %q", count, out)
	}
}

// ---------- message_delta in messageStopSent branch sets messageDeltaSent ----------

// TestCompliance_MessageDeltaInStopBranchSetsFlag verifies that when
// message_delta is released via the messageStopSent fast-path in
// releaseReady, the messageDeltaSent flag is set so Flush does not
// synthesize a duplicate.
func TestCompliance_MessageDeltaInStopBranchSetsFlag(t *testing.T) {
	n := NewSSENormalizer()
	// message_delta arrives after message_stop (both in same chunk).
	// message_stop gets buffered (messageDeltaSent==false), then
	// releaseReady processes both via the normal path.
	chunk := []byte(
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n")

	out := n.Process(chunk)
	flush := n.Flush()
	all := append(out, flush...)

	// Exactly one message_delta.
	deltaCount := strings.Count(string(all), "event: message_delta")
	if deltaCount != 1 {
		t.Errorf("expected 1 message_delta, got %d: %q", deltaCount, all)
	}
	// Exactly one message_stop.
	stopCount := strings.Count(string(all), "event: message_stop")
	if stopCount != 1 {
		t.Errorf("expected 1 message_stop, got %d: %q", stopCount, all)
	}
}
