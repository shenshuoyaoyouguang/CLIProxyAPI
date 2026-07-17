package thinking

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestFilterDeepSeekReasoningContentFromHistory_NoOpCases(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty_body", ""},
		{"invalid_json", "{not json"},
		{"no_messages", `{"model":"deepseek-r1"}`},
		{"messages_not_array", `{"messages":"hello"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterDeepSeekReasoningContentFromHistory([]byte(tc.body))
			if string(got) != tc.body {
				t.Fatalf("expected no-op, got %s", got)
			}
		})
	}
}

func TestFilterDeepSeekReasoningContentFromHistory_StripsReasoningForPlainAssistant(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello","reasoning_content":"let me think"},{"role":"user","content":"next"}]}`)
	out := FilterDeepSeekReasoningContentFromHistory(body)

	if got := gjson.GetBytes(out, "messages.#").Int(); got != 3 {
		t.Fatalf("messages count = %d, want 3. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.1.reasoning_content").String(); got != "" {
		t.Fatalf("messages.1.reasoning_content = %q, want empty. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.1.content").String(); got != "hello" {
		t.Fatalf("messages.1.content = %q, want hello. output: %s", got, out)
	}
}

func TestFilterDeepSeekReasoningContentFromHistory_DropsEmptyAssistantMessage(t *testing.T) {
	// Assistant message with only reasoning_content and no content/tool_calls should be dropped.
	body := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","reasoning_content":"thinking only"},{"role":"user","content":"next"}]}`)
	out := FilterDeepSeekReasoningContentFromHistory(body)

	if got := gjson.GetBytes(out, "messages.#").Int(); got != 2 {
		t.Fatalf("messages count = %d, want 2. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "user" {
		t.Fatalf("messages.0.role = %q, want user. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.1.role").String(); got != "user" {
		t.Fatalf("messages.1.role = %q, want user. output: %s", got, out)
	}
	if gjson.GetBytes(out, "messages.2").Exists() {
		t.Fatalf("expected messages.2 to be removed. output: %s", out)
	}
}

func TestFilterDeepSeekReasoningContentFromHistory_KeepsReasoningForToolCalls(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"search"},{"role":"assistant","content":"","reasoning_content":"need a tool","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"result"},{"role":"user","content":"explain"}]}`)
	out := FilterDeepSeekReasoningContentFromHistory(body)

	if got := gjson.GetBytes(out, "messages.#").Int(); got != 4 {
		t.Fatalf("messages count = %d, want 4. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.1.reasoning_content").String(); got != "need a tool" {
		t.Fatalf("messages.1.reasoning_content = %q, want 'need a tool'. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.1.tool_calls.0.function.name").String(); got != "search" {
		t.Fatalf("tool call name = %q, want search. output: %s", got, out)
	}
}

func TestFilterDeepSeekReasoningContentFromHistory_MultiTurnSplicing(t *testing.T) {
	// Simulates a realistic multi-turn conversation:
	//   user -> assistant (plain, reasoning stripped)
	//   user -> assistant (tool call, reasoning kept)
	//   tool result -> user
	body := []byte(`{"messages":[
		{"role":"system","content":"you are helpful"},
		{"role":"user","content":"q1"},
		{"role":"assistant","content":"a1","reasoning_content":"think a1"},
		{"role":"user","content":"q2"},
		{"role":"assistant","content":"","reasoning_content":"think tool","tool_calls":[{"id":"call_1","type":"function","function":{"name":"calc","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"42"},
		{"role":"user","content":"q3"}
	]}`)
	out := FilterDeepSeekReasoningContentFromHistory(body)

	if got := gjson.GetBytes(out, "messages.#").Int(); got != 7 {
		t.Fatalf("messages count = %d, want 7. output: %s", got, out)
	}

	// System and user messages untouched.
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "system" {
		t.Fatalf("messages.0.role = %q, want system. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.1.content").String(); got != "q1" {
		t.Fatalf("messages.1.content = %q, want q1. output: %s", got, out)
	}

	// Plain assistant message: reasoning_content stripped, content retained.
	if got := gjson.GetBytes(out, "messages.2.reasoning_content").String(); got != "" {
		t.Fatalf("messages.2.reasoning_content = %q, want empty. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.2.content").String(); got != "a1" {
		t.Fatalf("messages.2.content = %q, want a1. output: %s", got, out)
	}

	// Tool-call assistant message: reasoning_content preserved.
	if got := gjson.GetBytes(out, "messages.4.reasoning_content").String(); got != "think tool" {
		t.Fatalf("messages.4.reasoning_content = %q, want 'think tool'. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.4.tool_calls.0.id").String(); got != "call_1" {
		t.Fatalf("tool call id = %q, want call_1. output: %s", got, out)
	}

	// Tool message untouched.
	if got := gjson.GetBytes(out, "messages.5.role").String(); got != "tool" {
		t.Fatalf("messages.5.role = %q, want tool. output: %s", got, out)
	}
}

func TestFilterDeepSeekReasoningContentFromHistory_DropsAssistantWithEmptyContent(t *testing.T) {
	// Assistant message with explicit empty content and reasoning_content only.
	body := []byte(`{"messages":[{"role":"assistant","content":"","reasoning_content":"silent thought"}]}`)
	out := FilterDeepSeekReasoningContentFromHistory(body)

	if gjson.GetBytes(out, "messages.0").Exists() {
		t.Fatalf("expected empty assistant message to be dropped, got %s", out)
	}
}

func TestFilterDeepSeekReasoningContentFromHistory_DropsConsecutiveEmptyAssistants(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","reasoning_content":"thinking 1"},{"role":"assistant","reasoning_content":"thinking 2"},{"role":"user","content":"q1"},{"role":"user","content":"q2"}]}`)
	out := FilterDeepSeekReasoningContentFromHistory(body)

	if got := gjson.GetBytes(out, "messages.#").Int(); got != 2 {
		t.Fatalf("messages count = %d, want 2. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.0.content").String(); got != "q1" {
		t.Fatalf("messages.0.content = %q, want q1. output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.1.content").String(); got != "q2" {
		t.Fatalf("messages.1.content = %q, want q2. output: %s", got, out)
	}
	if gjson.GetBytes(out, "messages.2").Exists() {
		t.Fatalf("expected stale tail slots to be removed. output: %s", out)
	}
}

func TestFilterDeepSeekReasoningContentFromHistory_StripsReasoningForEmptyToolCalls(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello","reasoning_content":"plain thought","tool_calls":[]},{"role":"user","content":"next"}]}`)
	out := FilterDeepSeekReasoningContentFromHistory(body)

	if got := gjson.GetBytes(out, "messages.#").Int(); got != 3 {
		t.Fatalf("messages count = %d, want 3. output: %s", got, out)
	}
	if gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("empty tool_calls must not preserve reasoning_content. output: %s", out)
	}
	if got := gjson.GetBytes(out, "messages.1.content").String(); got != "hello" {
		t.Fatalf("messages.1.content = %q, want hello. output: %s", got, out)
	}
}
