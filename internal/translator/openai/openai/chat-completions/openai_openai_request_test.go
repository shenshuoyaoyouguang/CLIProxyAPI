package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToOpenAIUpdatesModel(t *testing.T) {
	out := ConvertOpenAIRequestToOpenAI("gpt-test", []byte(`{"model":"old-model","messages":[{"role":"user","content":"hi"}]}`), false)
	if got := gjson.GetBytes(out, "model").String(); got != "gpt-test" {
		t.Fatalf("model = %q, want gpt-test. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIRequestToOpenAIDeepSeekStripsPlainReasoning(t *testing.T) {
	out := ConvertOpenAIRequestToOpenAI("deepseek-r1", []byte(`{"model":"deepseek-r1","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello","reasoning_content":"let me think"},{"role":"user","content":"next"}]}`), false)
	if got := gjson.GetBytes(out, "messages.#").Int(); got != 3 {
		t.Fatalf("messages count = %d, want 3. Output: %s", got, string(out))
	}
	if gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("plain assistant reasoning_content must be stripped. Output: %s", string(out))
	}
	if got := gjson.GetBytes(out, "messages.1.content").String(); got != "hello" {
		t.Fatalf("messages.1.content = %q, want hello. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIRequestToOpenAIDeepSeekKeepsToolReasoning(t *testing.T) {
	out := ConvertOpenAIRequestToOpenAI("deepseek-r1", []byte(`{"model":"deepseek-r1","messages":[{"role":"user","content":"search"},{"role":"assistant","content":"","reasoning_content":"need a tool","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"result"},{"role":"user","content":"explain"}]}`), false)
	if got := gjson.GetBytes(out, "messages.#").Int(); got != 4 {
		t.Fatalf("messages count = %d, want 4. Output: %s", got, string(out))
	}
	if got := gjson.GetBytes(out, "messages.1.reasoning_content").String(); got != "need a tool" {
		t.Fatalf("messages.1.reasoning_content = %q, want 'need a tool'. Output: %s", got, string(out))
	}
	if got := gjson.GetBytes(out, "messages.1.tool_calls.0.function.name").String(); got != "search" {
		t.Fatalf("tool call name = %q, want search. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIRequestToOpenAIDropsEmptyAssistantAfterStripping(t *testing.T) {
	out := ConvertOpenAIRequestToOpenAI("deepseek-r1", []byte(`{"model":"deepseek-r1","messages":[{"role":"user","content":"hi"},{"role":"assistant","reasoning_content":"silent thought"},{"role":"user","content":"next"}]}`), false)
	if got := gjson.GetBytes(out, "messages.#").Int(); got != 2 {
		t.Fatalf("messages count = %d, want 2. Output: %s", got, string(out))
	}
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "user" {
		t.Fatalf("messages.0.role = %q, want user. Output: %s", got, string(out))
	}
	if got := gjson.GetBytes(out, "messages.1.role").String(); got != "user" {
		t.Fatalf("messages.1.role = %q, want user. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIRequestToOpenAINonDeepSeekLeavesReasoningIntact(t *testing.T) {
	out := ConvertOpenAIRequestToOpenAI("gpt-test", []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello","reasoning_content":"let me think"},{"role":"user","content":"next"}]}`), false)
	if got := gjson.GetBytes(out, "messages.1.reasoning_content").String(); got != "let me think" {
		t.Fatalf("messages.1.reasoning_content = %q, want 'let me think'. Output: %s", got, string(out))
	}
}
