package executor

import (
	"bytes"
	"testing"

	"github.com/tidwall/gjson"
)

// TestConvertReasoningToThinkingContent_EmptyPayloadReturnsNilError ensures the
// early-return path for empty payload does not produce an error.
func TestConvertReasoningToThinkingContent_EmptyPayloadReturnsNilError(t *testing.T) {
	out, err := convertReasoningToThinkingContent(nil)
	if err != nil {
		t.Fatalf("nil payload: unexpected error %v", err)
	}
	if out != nil {
		t.Fatalf("nil payload: want nil output, got %v", out)
	}

	out, err = convertReasoningToThinkingContent([]byte{})
	if err != nil {
		t.Fatalf("empty slice payload: unexpected error %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("empty slice payload: want empty output, got %q", string(out))
	}
}

// TestConvertReasoningToThinkingContent_InvalidJSONReturnsPayloadUnchanged verifies
// that invalid JSON input is returned as-is without an error.
func TestConvertReasoningToThinkingContent_InvalidJSONReturnsPayloadUnchanged(t *testing.T) {
	invalid := []byte(`not json at all {{{`)
	out, err := convertReasoningToThinkingContent(invalid)
	if err != nil {
		t.Fatalf("invalid JSON: unexpected error %v", err)
	}
	if !bytes.Equal(out, invalid) {
		t.Fatalf("invalid JSON: output %q differs from input %q", string(out), string(invalid))
	}
}

// TestConvertReasoningToThinkingContent_NoMessagesFieldReturnsPayloadUnchanged
// verifies that a payload without a "messages" field is returned as-is.
func TestConvertReasoningToThinkingContent_NoMessagesFieldReturnsPayloadUnchanged(t *testing.T) {
	payload := []byte(`{"reasoning_effort":"high","model":"gpt-4"}`)
	out, err := convertReasoningToThinkingContent(payload)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !bytes.Equal(out, payload) {
		t.Fatalf("output %q differs from input %q", string(out), string(payload))
	}
}

// TestConvertReasoningToThinkingContent_EmptyMessagesArrayNoOp verifies that an
// empty messages array is returned as-is without modification.
func TestConvertReasoningToThinkingContent_EmptyMessagesArrayNoOp(t *testing.T) {
	payload := []byte(`{"reasoning_effort":"high","messages":[]}`)
	out, err := convertReasoningToThinkingContent(payload)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	msgs := gjson.GetBytes(out, "messages")
	if !msgs.IsArray() || len(msgs.Array()) != 0 {
		t.Fatalf("messages should remain empty array, got %s", msgs.Raw)
	}
}

// TestConvertReasoningToThinkingContent_OriginalPayloadReturnedOnSuccess verifies
// the error-contract: when the conversion succeeds, the returned payload is NOT
// the original pointer (it has been rewritten), and no error is returned.
func TestConvertReasoningToThinkingContent_OriginalPayloadReturnedOnSuccess(t *testing.T) {
	payload := []byte(`{
		"reasoning_effort":"high",
		"messages":[
			{"role":"assistant","content":"result","reasoning_content":"my thought"}
		]
	}`)

	out, err := convertReasoningToThinkingContent(payload)
	if err != nil {
		t.Fatalf("convertReasoningToThinkingContent() error = %v", err)
	}
	// The output should differ from the input (new content array was built).
	if bytes.Equal(out, payload) {
		t.Fatal("expected output to differ from input after conversion")
	}
	// Spot-check: thinking block should be present.
	if gjson.GetBytes(out, "messages.0.content.0.type").String() != "thinking" {
		t.Fatalf("expected thinking block in content.0, got %s",
			gjson.GetBytes(out, "messages.0.content.0").Raw)
	}
}

// TestConvertReasoningToThinkingContent_StringContentWrappedAsTextBlock verifies
// that an assistant message with string "content" and a reasoning_content is
// converted so that the string becomes a {"type":"text","text":"..."} block
// following the thinking block.
func TestConvertReasoningToThinkingContent_StringContentWrappedAsTextBlock(t *testing.T) {
	payload := []byte(`{
		"reasoning_effort":"high",
		"messages":[
			{"role":"assistant","content":"my response","reasoning_content":"deep thought"}
		]
	}`)

	out, err := convertReasoningToThinkingContent(payload)
	if err != nil {
		t.Fatalf("convertReasoningToThinkingContent() error = %v", err)
	}

	contentArr := gjson.GetBytes(out, "messages.0.content").Array()
	if len(contentArr) != 2 {
		t.Fatalf("content should have 2 parts (thinking + text), got %d", len(contentArr))
	}

	if contentArr[0].Get("type").String() != "thinking" {
		t.Errorf("content[0].type = %q, want %q", contentArr[0].Get("type").String(), "thinking")
	}
	if contentArr[0].Get("thinking").String() != "deep thought" {
		t.Errorf("content[0].thinking = %q, want %q", contentArr[0].Get("thinking").String(), "deep thought")
	}

	if contentArr[1].Get("type").String() != "text" {
		t.Errorf("content[1].type = %q, want %q", contentArr[1].Get("type").String(), "text")
	}
	if contentArr[1].Get("text").String() != "my response" {
		t.Errorf("content[1].text = %q, want %q", contentArr[1].Get("text").String(), "my response")
	}
}

// TestConvertReasoningToThinkingContent_EmptyStringContentOnlyThinkingBlock verifies
// that when string content is empty, only the thinking block is emitted (no text block).
func TestConvertReasoningToThinkingContent_EmptyStringContentOnlyThinkingBlock(t *testing.T) {
	payload := []byte(`{
		"reasoning_effort":"high",
		"messages":[
			{"role":"assistant","content":"","reasoning_content":"thought only"}
		]
	}`)

	out, err := convertReasoningToThinkingContent(payload)
	if err != nil {
		t.Fatalf("convertReasoningToThinkingContent() error = %v", err)
	}

	contentArr := gjson.GetBytes(out, "messages.0.content").Array()
	if len(contentArr) != 1 {
		t.Fatalf("empty string content should yield only the thinking block, got %d parts", len(contentArr))
	}
	if contentArr[0].Get("type").String() != "thinking" {
		t.Errorf("content[0].type = %q, want %q", contentArr[0].Get("type").String(), "thinking")
	}
}

// TestConvertReasoningToThinkingContent_MiMo_EnabledConverts verifies that a
// MiMo payload with thinking.type=enabled converts reasoning_content into
// content-array thinking blocks while preserving the top-level reasoning_content.
func TestConvertReasoningToThinkingContent_MiMo_EnabledConverts(t *testing.T) {
	payload := []byte(`{
		"thinking":{"type":"enabled"},
		"model":"mimo-v2.5-pro",
		"messages":[
			{"role":"assistant","content":"result","reasoning_content":"deep mimo thinking"}
		]
	}`)

	out, err := convertReasoningToThinkingContent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bytes.Equal(out, payload) {
		t.Fatal("expected payload to be rewritten with thinking blocks")
	}

	// thinking block should be prepended to content
	if gjson.GetBytes(out, "messages.0.content.0.type").String() != "thinking" {
		t.Fatalf("expected thinking block, got %s", gjson.GetBytes(out, "messages.0.content.0").Raw)
	}
	if gjson.GetBytes(out, "messages.0.content.0.thinking").String() != "deep mimo thinking" {
		t.Fatalf("expected thinking text, got %s", gjson.GetBytes(out, "messages.0.content.0.thinking").Raw)
	}
	// reasoning_content should still be preserved
	if rc := gjson.GetBytes(out, "messages.0.reasoning_content").String(); rc != "deep mimo thinking" {
		t.Fatalf("reasoning_content = %q, want %q", rc, "deep mimo thinking")
	}
}

// TestConvertReasoningToThinkingContent_MiMo_DisabledNoConvert verifies that
// a MiMo payload with thinking.type=disabled does NOT convert reasoning_content.
func TestConvertReasoningToThinkingContent_MiMo_DisabledNoConvert(t *testing.T) {
	payload := []byte(`{
		"thinking":{"type":"disabled"},
		"model":"mimo-v2.5-pro",
		"messages":[
			{"role":"assistant","content":"result","reasoning_content":"old reasoning"}
		]
	}`)

	out, err := convertReasoningToThinkingContent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, payload) {
		t.Fatalf("expected payload unchanged for thinking.type=disabled, got diff")
	}
}

// TestConvertReasoningToThinkingContent_MiMo_MultiTurnToolCalls verifies the
// MiMo-specific scenario: multi-turn with tool calls where reasoning_content
// must be preserved across all assistant messages.
func TestConvertReasoningToThinkingContent_MiMo_MultiTurnToolCalls(t *testing.T) {
	payload := []byte(`{
		"thinking":{"type":"enabled"},
		"model":"mimo-v2.5-pro",
		"messages":[
			{"role":"user","content":"What is the weather in Beijing?"},
			{"role":"assistant","content":"","reasoning_content":"User asks about Beijing weather, I need to call the tool.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"Sunny 25\u00b0C"},
			{"role":"assistant","content":"Beijing is sunny, 25\u00b0C.","reasoning_content":"Got the result, now I'll respond."}
		]
	}`)

	out, err := convertReasoningToThinkingContent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First assistant message should have thinking block
	if gjson.GetBytes(out, "messages.1.content").IsArray() {
		if gjson.GetBytes(out, "messages.1.content.0.type").String() != "thinking" {
			t.Fatalf("first assistant should have thinking block, got %s", gjson.GetBytes(out, "messages.1.content.0").Raw)
		}
	}

	// Both assistant messages should retain reasoning_content
	if rc1 := gjson.GetBytes(out, "messages.1.reasoning_content").String(); rc1 == "" {
		t.Fatal("first assistant reasoning_content should be preserved")
	}
	if rc3 := gjson.GetBytes(out, "messages.3.reasoning_content").String(); rc3 == "" {
		t.Fatal("second assistant reasoning_content should be preserved")
	}
}

// TestPreserveReasoningContent_MiMo_ToolCallScenario verifies the MiMo-specific
// multi-turn tool-call preservation scenario: reasoning_content from the original
// assistant message with tool_calls is re-injected into translated output that
// lost it during translation.
func TestPreserveReasoningContent_MiMo_ToolCallScenario(t *testing.T) {
	original := []byte(`{
		"thinking":{"type":"enabled"},
		"model":"mimo-v2.5-pro",
		"messages":[
			{"role":"user","content":"What is the weather in Beijing?"},
			{"role":"assistant","content":"","reasoning_content":"User asks about weather, calling tool.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"Sunny 25C"},
			{"role":"assistant","content":"Beijing is sunny, 25C.","reasoning_content":"Got the result, now responding."}
		]
	}`)
	translated := []byte(`{
		"thinking":{"type":"enabled"},
		"model":"mimo-v2.5-pro",
		"messages":[
			{"role":"user","content":"What is the weather in Beijing?"},
			{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"Sunny 25C"},
			{"role":"assistant","content":"Beijing is sunny, 25C."}
		]
	}`)

	out, err := preserveReasoningContent(original, translated)
	if err != nil {
		t.Fatalf("preserveReasoningContent() error = %v", err)
	}

	// First assistant (tool call) should have reasoning_content restored
	rc1 := gjson.GetBytes(out, "messages.1.reasoning_content").String()
	if rc1 != "User asks about weather, calling tool." {
		t.Fatalf("messages.1.reasoning_content = %q, want %q", rc1, "User asks about weather, calling tool.")
	}

	// Second assistant (final answer) should have reasoning_content restored
	rc3 := gjson.GetBytes(out, "messages.3.reasoning_content").String()
	if rc3 != "Got the result, now responding." {
		t.Fatalf("messages.3.reasoning_content = %q, want %q", rc3, "Got the result, now responding.")
	}
}
