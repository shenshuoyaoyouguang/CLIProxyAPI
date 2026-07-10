package executor

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeMimoToolMessageReasoning_NoToolCallsUnchanged(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"hi there"}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body should be unchanged when no tool_calls present, got %s", string(out))
	}
}

func TestNormalizeMimoToolMessageReasoning_ExistingReasoningUnchanged(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":"keep me"}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body should be unchanged when reasoning_content already present, got %s", string(out))
	}
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "keep me" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "keep me")
	}
}

func TestNormalizeMimoToolMessageReasoning_UsesReasoningField(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","reasoning":"my reasoning trace","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "my reasoning trace" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "my reasoning trace")
	}
}

func TestNormalizeMimoToolMessageReasoning_UsesPreviousReasoningFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"plan","reasoning_content":"previous reasoning"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	got := gjson.GetBytes(out, "messages.1.reasoning_content").String()
	if got != "previous reasoning" {
		t.Fatalf("messages.1.reasoning_content = %q, want %q", got, "previous reasoning")
	}
}

func TestNormalizeMimoToolMessageReasoning_InterveningAssistantClearsPreviousReasoningFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"plan","reasoning_content":"previous reasoning"},
			{"role":"assistant","content":"plain follow-up"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.2.reasoning_content").String(); got != "[reasoning unavailable]" {
		t.Fatalf("messages.2.reasoning_content = %q, want %q", got, "[reasoning unavailable]")
	}
}

func TestNormalizeMimoToolMessageReasoning_UsesContentStringFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"assistant summary","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "assistant summary" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "assistant summary")
	}
}

func TestNormalizeMimoToolMessageReasoning_UsesContentArrayFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"text","text":"first line"},{"type":"text","text":"second line"}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "first line\nsecond line" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "first line\nsecond line")
	}
}

func TestNormalizeMimoToolMessageReasoning_FallbackPlaceholder(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	reasoning := gjson.GetBytes(out, "messages.0.reasoning_content")
	if !reasoning.Exists() {
		t.Fatalf("messages.0.reasoning_content should exist")
	}
	if reasoning.String() != "[reasoning unavailable]" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", reasoning.String(), "[reasoning unavailable]")
	}
}

func TestNormalizeMimoToolMessageReasoning_MixedMessagesOnlyPatchesNeeded(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":"start"},
			{"role":"assistant","content":"plain reply"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":"keep me"},
			{"role":"tool","tool_call_id":"call_1","content":"[]"},
			{"role":"assistant","content":"from content","tool_calls":[{"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
			{"role":"assistant","tool_calls":[{"id":"call_3","type":"function","function":{"name":"write_file","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)

	// messages.0: user, untouched
	if got := gjson.GetBytes(out, "messages.0.content").String(); got != "start" {
		t.Fatalf("messages.0.content = %q, want %q", got, "start")
	}
	// messages.1: assistant without tool_calls, untouched (no reasoning_content added)
	if gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("messages.1.reasoning_content should not be added when no tool_calls")
	}
	// messages.2: already has reasoning_content, untouched
	if got := gjson.GetBytes(out, "messages.2.reasoning_content").String(); got != "keep me" {
		t.Fatalf("messages.2.reasoning_content = %q, want %q", got, "keep me")
	}
	// messages.4: backfilled from content
	if got := gjson.GetBytes(out, "messages.4.reasoning_content").String(); got != "from content" {
		t.Fatalf("messages.4.reasoning_content = %q, want %q", got, "from content")
	}
	// messages.5: backfilled with placeholder
	if got := gjson.GetBytes(out, "messages.5.reasoning_content").String(); got != "[reasoning unavailable]" {
		t.Fatalf("messages.5.reasoning_content = %q, want %q", got, "[reasoning unavailable]")
	}
}

func TestNormalizeMimoToolMessageReasoning_EmptyReasoningContentReplaced(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"summary","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":""}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "summary" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "summary")
	}
}

func TestNormalizeMimoToolMessageReasoning_InvalidJSONUnchanged(t *testing.T) {
	body := []byte(`{not valid json`)

	out := normalizeMimoToolMessageReasoning(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("invalid JSON body should be returned unchanged")
	}
}

func TestNormalizeMimoToolMessageReasoning_NoMessagesUnchanged(t *testing.T) {
	body := []byte(`{"model":"mimo-v2.5"}`)

	out := normalizeMimoToolMessageReasoning(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body without messages should be returned unchanged, got %s", string(out))
	}
}

func TestMimoLockThinkingParams_EnabledLocksParams(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5",
		"thinking":{"type":"enabled"}
	}`)

	out := mimoLockThinkingParams(body)
	if got := gjson.GetBytes(out, "temperature").Float(); got != 1.0 {
		t.Fatalf("temperature = %v, want 1.0", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.95 {
		t.Fatalf("top_p = %v, want 0.95", got)
	}
}

func TestMimoLockThinkingParams_DisabledUnchanged(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5",
		"thinking":{"type":"disabled"},
		"temperature":0.5,
		"top_p":0.8
	}`)

	out := mimoLockThinkingParams(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body should be unchanged when thinking.type=disabled, got %s", string(out))
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.5 {
		t.Fatalf("temperature = %v, want 0.5", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.8 {
		t.Fatalf("top_p = %v, want 0.8", got)
	}
}

func TestMimoLockThinkingParams_NoThinkingTypeUnchanged(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5",
		"temperature":0.7
	}`)

	out := mimoLockThinkingParams(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body should be unchanged when thinking.type absent, got %s", string(out))
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.7 {
		t.Fatalf("temperature = %v, want 0.7", got)
	}
}

func TestMimoLockThinkingParams_OverwritesExistingTemperature(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5",
		"thinking":{"type":"enabled"},
		"temperature":0.5,
		"top_p":0.7
	}`)

	out := mimoLockThinkingParams(body)
	if got := gjson.GetBytes(out, "temperature").Float(); got != 1.0 {
		t.Fatalf("temperature = %v, want 1.0 (should overwrite 0.5)", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.95 {
		t.Fatalf("top_p = %v, want 0.95 (should overwrite 0.7)", got)
	}
}

// ---------------------------------------------------------------------------
// Risk-focused coverage: multi-turn tool calls & temperature/top_p locking
// ---------------------------------------------------------------------------

// Multi-turn tool loop: assistant->tool->assistant->tool->assistant, three
// separate assistant messages carrying tool_calls, all needing reasoning_content
// backfill from different sources (reasoning field, content, fallback).
func TestNormalizeMimoToolMessageReasoning_MultiTurnToolCallLoop(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":"list files then read one"},
			{"role":"assistant","reasoning":"planning list","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"[\"a.txt\"]"},
			{"role":"assistant","content":"picking a.txt","tool_calls":[{"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]},
			{"role":"tool","tool_call_id":"call_2","content":"hello world"},
			{"role":"assistant","tool_calls":[{"id":"call_3","type":"function","function":{"name":"write_file","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)

	if got := gjson.GetBytes(out, "messages.1.reasoning_content").String(); got != "planning list" {
		t.Fatalf("messages.1.reasoning_content = %q, want %q", got, "planning list")
	}
	if got := gjson.GetBytes(out, "messages.3.reasoning_content").String(); got != "picking a.txt" {
		t.Fatalf("messages.3.reasoning_content = %q, want %q", got, "picking a.txt")
	}
	if got := gjson.GetBytes(out, "messages.5.reasoning_content").String(); got != "[reasoning unavailable]" {
		t.Fatalf("messages.5.reasoning_content = %q, want %q", got, "[reasoning unavailable]")
	}
	// Tool messages must be left alone; no reasoning_content should be added there.
	if gjson.GetBytes(out, "messages.2.reasoning_content").Exists() {
		t.Fatalf("tool message at index 2 must not receive reasoning_content")
	}
	if gjson.GetBytes(out, "messages.4.reasoning_content").Exists() {
		t.Fatalf("tool message at index 4 must not receive reasoning_content")
	}
}

// Empty tool_calls array (len==0) must be treated the same as no tool_calls
// at all: the message is left untouched.
func TestNormalizeMimoToolMessageReasoning_EmptyToolCallsArrayUnchanged(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[],"content":"nothing to do"}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body should be unchanged when tool_calls array is empty, got %s", string(out))
	}
	if gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
		t.Fatalf("empty tool_calls array must not trigger reasoning_content backfill")
	}
}

// Role match is case-sensitive: an "Assistant" (capital A) message with
// tool_calls should NOT be modified. This guards against upstreams that
// accidentally send capitalized role names.
func TestNormalizeMimoToolMessageReasoning_RoleCaseSensitive(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"Assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
		t.Fatalf(`role "Assistant" (capital A) must not be treated as assistant`)
	}
}

// Role is trimmed of whitespace before comparison: " assistant " must still
// trigger the backfill (mirrors the TrimSpace call in the implementation).
func TestNormalizeMimoToolMessageReasoning_RoleWithWhitespaceTrimmed(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"  assistant  ","content":"trim me","tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "trim me" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "trim me")
	}
}

// Whitespace-only reasoning_content must be replaced (not treated as
// "already set"). The implementation calls strings.TrimSpace before the
// non-empty check.
func TestNormalizeMimoToolMessageReasoning_WhitespaceReasoningContentReplaced(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"backfill","tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}],"reasoning_content":"   \t  "}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "backfill" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "backfill")
	}
}

// When the "reasoning" field is present but only whitespace, the fallback
// must proceed to the content field rather than emit an empty string.
func TestNormalizeMimoToolMessageReasoning_WhitespaceReasoningFallsToContent(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","reasoning":"   ","content":"used content","tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "used content" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "used content")
	}
}

// Content array whose text parts are all empty/whitespace must fall through
// to the "[reasoning unavailable]" placeholder — not produce an empty join.
func TestNormalizeMimoToolMessageReasoning_ContentArrayAllEmptyTextsFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"text","text":""},{"type":"text","text":"   "}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "[reasoning unavailable]" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "[reasoning unavailable]")
	}
}

// Content array carrying non-text parts (image_url, tool_use etc.) must be
// ignored — only text parts are joined. Non-text-only content should still
// fall to the placeholder.
func TestNormalizeMimoToolMessageReasoning_ContentArrayIgnoresNonTextParts(t *testing.T) {
	// Mixed: one text part + one non-text -> text is used.
	mixed := []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"http://example/a.png"}},{"type":"text","text":"only text kept"}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]}
		]
	}`)
	out := normalizeMimoToolMessageReasoning(mixed)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "only text kept" {
		t.Fatalf("mixed content: reasoning_content = %q, want %q", got, "only text kept")
	}

	// Non-text only: no text parts -> placeholder.
	nonText := []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"http://example/a.png"}}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]}
		]
	}`)
	out = normalizeMimoToolMessageReasoning(nonText)
	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "[reasoning unavailable]" {
		t.Fatalf("non-text-only content: reasoning_content = %q, want %q", got, "[reasoning unavailable]")
	}
}

// Non-assistant roles that happen to carry a tool_calls array (e.g. malformed
// upstream state) must be left completely untouched.
func TestNormalizeMimoToolMessageReasoning_NonAssistantWithToolCallsIgnored(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","tool_calls":[{"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","tool_calls":[{"id":"call_2","type":"function","function":{"name":"noop","arguments":"{}"}}],"content":"result"},
			{"role":"system","tool_calls":[{"id":"call_3","type":"function","function":{"name":"noop","arguments":"{}"}}]}
		]
	}`)

	out := normalizeMimoToolMessageReasoning(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("non-assistant messages must not be modified, got %s", string(out))
	}
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("messages.%d.reasoning_content", i)
		if gjson.GetBytes(out, path).Exists() {
			t.Fatalf("messages.%d.reasoning_content must not be set", i)
		}
	}
}

// If tool_calls is not a JSON array (string, object) IsArray() rejects it and
// the message must be left untouched — no panic, no partial mutation.
func TestNormalizeMimoToolMessageReasoning_ToolCallsNotArrayIgnored(t *testing.T) {
	bodies := [][]byte{
		[]byte(`{"messages":[{"role":"assistant","tool_calls":"not-an-array","content":"c"}]}`),
		[]byte(`{"messages":[{"role":"assistant","tool_calls":{"id":"call_1"},"content":"c"}]}`),
		[]byte(`{"messages":[{"role":"assistant","tool_calls":42,"content":"c"}]}`),
	}
	for i, body := range bodies {
		out := normalizeMimoToolMessageReasoning(body)
		if !bytes.Equal(out, body) {
			t.Fatalf("case %d: body should be unchanged for non-array tool_calls, got %s", i, string(out))
		}
		if gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
			t.Fatalf("case %d: reasoning_content must not be set", i)
		}
	}
}

// thinking.type comparison is case-sensitive: "ENABLED" does not lock params.
// This documents the current contract so a future refactor that lowercases the
// value has to acknowledge the change.
func TestMimoLockThinkingParams_UppercaseTypeUnchanged(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5",
		"thinking":{"type":"ENABLED"},
		"temperature":0.5,
		"top_p":0.7
	}`)

	out := mimoLockThinkingParams(body)
	if !bytes.Equal(out, body) {
		t.Fatalf(`thinking.type "ENABLED" (uppercase) must not lock params, got %s`, string(out))
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.5 {
		t.Fatalf("temperature = %v, want 0.5 (unchanged)", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.7 {
		t.Fatalf("top_p = %v, want 0.7 (unchanged)", got)
	}
}

// thinking.type surrounded by whitespace is NOT trimmed by the lock helper
// (mirrors current impl: exact string equality). This test pins that
// contract; if the executor upstream ever trims, this test will need to
// change alongside.
func TestMimoLockThinkingParams_WhitespaceTypeUnchanged(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5",
		"thinking":{"type":" enabled "},
		"temperature":0.5
	}`)

	out := mimoLockThinkingParams(body)
	if !bytes.Equal(out, body) {
		t.Fatalf(`thinking.type " enabled " with whitespace must not lock params, got %s`, string(out))
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.5 {
		t.Fatalf("temperature = %v, want 0.5 (unchanged)", got)
	}
}

// Other thinking.type values ("auto", "adaptive", unknown) must not trigger
// the lock, only the exact string "enabled" does.
func TestMimoLockThinkingParams_NonEnabledTypesUnchanged(t *testing.T) {
	cases := []string{"auto", "adaptive", "on", "true", "1", ""}
	for _, v := range cases {
		body := []byte(fmt.Sprintf(`{"model":"mimo-v2.5","thinking":{"type":%q},"temperature":0.42,"top_p":0.42}`, v))
		out := mimoLockThinkingParams(body)
		if !bytes.Equal(out, body) {
			t.Fatalf("thinking.type=%q must not lock params, got %s", v, string(out))
		}
		if got := gjson.GetBytes(out, "temperature").Float(); got != 0.42 {
			t.Fatalf("thinking.type=%q: temperature = %v, want 0.42", v, got)
		}
	}
}

// Invalid JSON must not panic and must be returned as-is (no fields set,
// no partial write). gjson gracefully treats the path as absent.
func TestMimoLockThinkingParams_InvalidJSONReturnsUnchanged(t *testing.T) {
	body := []byte(`{not valid json`)
	out := mimoLockThinkingParams(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("invalid JSON must be returned unchanged, got %s", string(out))
	}
}

// When lock activates, unrelated fields (model, messages, tools,
// max_completion_tokens) must survive untouched. This guards against a
// sjson misuse regressing the payload shape.
func TestMimoLockThinkingParams_PreservesOtherFields(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5",
		"thinking":{"type":"enabled"},
		"temperature":0.1,
		"top_p":0.2,
		"max_completion_tokens":4096,
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"type":"function","function":{"name":"list_directory","parameters":{}}}]
	}`)

	out := mimoLockThinkingParams(body)

	if got := gjson.GetBytes(out, "temperature").Float(); got != 1.0 {
		t.Fatalf("temperature = %v, want 1.0", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.95 {
		t.Fatalf("top_p = %v, want 0.95", got)
	}
	if got := gjson.GetBytes(out, "model").String(); got != "mimo-v2.5" {
		t.Fatalf("model = %q, want mimo-v2.5", got)
	}
	if got := gjson.GetBytes(out, "max_completion_tokens").Int(); got != 4096 {
		t.Fatalf("max_completion_tokens = %v, want 4096", got)
	}
	if got := gjson.GetBytes(out, "messages.0.content").String(); got != "hi" {
		t.Fatalf("messages.0.content = %q, want %q", got, "hi")
	}
	if got := gjson.GetBytes(out, "tools.0.function.name").String(); got != "list_directory" {
		t.Fatalf("tools.0.function.name = %q, want %q", got, "list_directory")
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled", got)
	}
}

// Order preservation test: the executor calls normalizeMimoToolMessageReasoning
// first, then mimoLockThinkingParams. Neither step should discard the other's
// work, and the final payload should carry both patches.
func TestMimoNormalization_OrderPreservesBothEffects(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5",
		"thinking":{"type":"enabled"},
		"temperature":0.3,
		"top_p":0.4,
		"messages":[
			{"role":"user","content":"do it"},
			{"role":"assistant","content":"planning","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"[]"},
			{"role":"assistant","tool_calls":[{"id":"call_2","type":"function","function":{"name":"write_file","arguments":"{}"}}]}
		]
	}`)

	// Mirror executor order: normalize -> lock.
	out := normalizeMimoToolMessageReasoning(body)
	out = mimoLockThinkingParams(out)

	if got := gjson.GetBytes(out, "temperature").Float(); got != 1.0 {
		t.Fatalf("temperature = %v, want 1.0", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.95 {
		t.Fatalf("top_p = %v, want 0.95", got)
	}
	if got := gjson.GetBytes(out, "messages.1.reasoning_content").String(); got != "planning" {
		t.Fatalf("messages.1.reasoning_content = %q, want %q", got, "planning")
	}
	if got := gjson.GetBytes(out, "messages.3.reasoning_content").String(); got != "[reasoning unavailable]" {
		t.Fatalf("messages.3.reasoning_content = %q, want %q", got, "[reasoning unavailable]")
	}
	// Swap order: lock -> normalize. Result must be equivalent for these two
	// orthogonal patches so the executor is not order-fragile.
	swap := mimoLockThinkingParams(body)
	swap = normalizeMimoToolMessageReasoning(swap)
	if got := gjson.GetBytes(swap, "temperature").Float(); got != 1.0 {
		t.Fatalf("swap order: temperature = %v, want 1.0", got)
	}
	if got := gjson.GetBytes(swap, "messages.1.reasoning_content").String(); got != "planning" {
		t.Fatalf("swap order: messages.1.reasoning_content = %q, want %q", got, "planning")
	}
}
