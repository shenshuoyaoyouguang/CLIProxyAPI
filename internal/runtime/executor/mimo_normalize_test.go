package executor

import (
	"bytes"
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
