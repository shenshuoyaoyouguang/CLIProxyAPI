package claude

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// TestMultiTurnThinkingLift_FromDeepSeekReasoningContent verifies the L4 fix for
// the "reasoning_content trap": when a thinking block originates from DeepSeek's
// reasoning_content (via the response translator, which emits NO signature), the
// request translator must LIFT the thinking text back into reasoning_content on
// the next turn instead of dropping it.
//
// This is the exact scenario described by DeepSeek's docs and LiteLLM #28057:
//   - Round 1: DeepSeek returns reasoning_content + tool_calls
//   - Response translator converts reasoning_content → type="thinking" (no signature)
//   - Round 2: Client sends back the thinking block in messages history
//   - Request translator lifts thinking → reasoning_content (DeepSeek target bypass)
//
// Previously this dropped the thinking text, causing either HTTP 400 ("reasoning_content
// must be passed back") or — after the executor's empty-string fallback — silent CoT
// loss across tool turns.
func TestMultiTurnThinkingLift_FromDeepSeekReasoningContent(t *testing.T) {
	// This simulates Round 1's response translator output from DeepSeek reasoning_content.
	// The assistant message has:
	//   1. type=thinking (from reasoning_content) — NO SIGNATURE
	//   2. type=text
	//   3. type=tool_use (tool call)
	//
	// In Round 2, the client sends this entire history back to the proxy.
	const thinkingText = "The user asks about Hangzhou weather. I need to call get_weather tool."
	inputJSON := `{
		"model": "deepseek-v4-pro",
		"messages": [
			{
				"role": "user",
				"content": [{"type": "text", "text": "What's the weather in Hangzhou?"}]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "` + thinkingText + `"},
					{"type": "text", "text": "Let me check the weather for you."},
					{"type": "tool_use", "id": "toolu_abc123", "name": "get_weather", "input": {"city": "Hangzhou"}}
				]
			}
		]
	}`

	result := ConvertClaudeRequestToOpenAI("deepseek-v4-pro", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	messages := resultJSON.Get("messages").Array()
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(messages))
	}

	assistantMsg := messages[1]
	if assistantMsg.Get("role").String() != "assistant" {
		t.Fatalf("expected messages[1] to be assistant, got %s", assistantMsg.Get("role").String())
	}

	// LIFT ASSERTION: reasoning_content MUST be present and equal the original thinking text.
	gotReasoningContent := assistantMsg.Get("reasoning_content").String()
	if gotReasoningContent != thinkingText {
		t.Errorf("reasoning_content lift failed:\n  got:  %q\n  want: %q\n  exists: %v",
			gotReasoningContent, thinkingText, assistantMsg.Get("reasoning_content").Exists())
	}

	// tool_calls MUST still be present (lift must not strip them).
	gotToolCalls := assistantMsg.Get("tool_calls").Exists()
	if !gotToolCalls {
		t.Errorf("tool_calls should survive the lift, but it's missing")
	}

	// content MUST still be present (the text block).
	gotContent := assistantMsg.Get("content").String()
	if gotContent == "" {
		t.Errorf("content should survive the lift (text block), but it's empty")
	}

	t.Logf("LIFT OK: reasoning_content=%q, tool_calls=%v, content present=%v",
		gotReasoningContent, gotToolCalls, gotContent != "")
}

// TestMultiTurnThinkingLift_NonToolTurn verifies that thinking is lifted even
// for non-tool-calling turns. DeepSeek's docs say non-tool reasoning_content is
// optional, but lifting it preserves CoT quality and improves prefix-cache hit
// rate (LiteLLM community finding). It does NOT cause 400s.
func TestMultiTurnThinkingLift_NonToolTurn(t *testing.T) {
	const thinkingText = "Comparing 9.11 and 9.8..."
	inputJSON := `{
		"model": "deepseek-v4-pro",
		"messages": [
			{
				"role": "user",
				"content": [{"type": "text", "text": "9.11 and 9.8, which is greater?"}]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "` + thinkingText + `"},
					{"type": "text", "text": "9.11 is greater than 9.8."}
				]
			},
			{
				"role": "user",
				"content": [{"type": "text", "text": "How many Rs in strawberry?"}]
			}
		]
	}`

	result := ConvertClaudeRequestToOpenAI("deepseek-v4-pro", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	messages := resultJSON.Get("messages").Array()

	for _, msg := range messages {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		gotReasoning := msg.Get("reasoning_content").String()
		if gotReasoning != thinkingText {
			t.Errorf("non-tool turn: reasoning_content lift failed:\n  got:  %q\n  want: %q",
				gotReasoning, thinkingText)
		}
	}
}

// TestMultiTurnThinkingLift_NonDeepSeekTarget_StillDrops is a regression guard:
// non-DeepSeek targets (e.g. GPT/Claude) must continue to drop unsigned thinking
// blocks. The DeepSeek bypass must NOT leak into the GPT signature-gated path.
func TestMultiTurnThinkingLift_NonDeepSeekTarget_StillDrops(t *testing.T) {
	inputJSON := `{
		"model": "gpt-5",
		"messages": [
			{
				"role": "user",
				"content": [{"type": "text", "text": "hi"}]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "unsigned thinking should be dropped for GPT"},
					{"type": "text", "text": "hello"}
				]
			}
		]
	}`

	result := ConvertClaudeRequestToOpenAI("gpt-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	for _, msg := range resultJSON.Get("messages").Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if msg.Get("reasoning_content").Exists() {
			t.Errorf("GPT target: unsigned thinking must be dropped, but reasoning_content exists: %s",
				msg.Get("reasoning_content").String())
		}
	}
}

// TestMultiTurnThinkingLift_MultipleThinkingBlocks verifies that multiple
// thinking blocks in one assistant message are joined into a single
// reasoning_content string (matches the existing join behavior with "\n\n").
func TestMultiTurnThinkingLift_MultipleThinkingBlocks(t *testing.T) {
	inputJSON := `{
		"model": "deepseek-v4-pro",
		"messages": [
			{
				"role": "user",
				"content": [{"type": "text", "text": "solve it"}]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "step 1"},
					{"type": "thinking", "thinking": "step 2"},
					{"type": "text", "text": "answer"}
				]
			}
		]
	}`

	result := ConvertClaudeRequestToOpenAI("deepseek-v4-pro", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	for _, msg := range resultJSON.Get("messages").Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		got := msg.Get("reasoning_content").String()
		want := "step 1\n\nstep 2"
		if got != want {
			t.Errorf("multiple thinking blocks: reasoning_content = %q, want %q", got, want)
		}
	}
}

// TestMultiTurnThinkingLift_DeprecatedReasoner verifies that the deprecated
// deepseek-reasoner model (removed from models.json) does NOT receive passback.
func TestMultiTurnThinkingLift_DeprecatedReasoner(t *testing.T) {
	const thinkingText = "reasoning via deprecated alias"
	inputJSON := `{
		"model": "deepseek-reasoner",
		"messages": [
			{
				"role": "user",
				"content": [{"type": "text", "text": "hi"}]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "` + thinkingText + `"},
					{"type": "text", "text": "hello"}
				]
			}
		]
	}`

	result := ConvertClaudeRequestToOpenAI("deepseek-reasoner", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	for _, msg := range resultJSON.Get("messages").Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if msg.Get("reasoning_content").Exists() {
			t.Errorf("deepseek-reasoner: must not receive passback after removal, got reasoning_content=%q",
				msg.Get("reasoning_content").String())
		}
	}
}

// TestMultiTurnThinkingLift_V31Model verifies deepseek-v3.1 (removed from models.json)
// does NOT receive passback.
func TestMultiTurnThinkingLift_V31Model(t *testing.T) {
	const thinkingText = "v3.1 multi-turn cot"
	inputJSON := `{
		"model": "deepseek-v3.1",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "hi"}]},
			{"role": "assistant", "content": [
				{"type": "thinking", "thinking": "` + thinkingText + `"},
				{"type": "text", "text": "hello"}
			]}
		]
	}`
	result := ConvertClaudeRequestToOpenAI("deepseek-v3.1", []byte(inputJSON), false)
	for _, msg := range gjson.ParseBytes(result).Get("messages").Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if msg.Get("reasoning_content").Exists() {
			t.Errorf("deepseek-v3.1: must not receive passback after removal, got reasoning_content=%q",
				msg.Get("reasoning_content").String())
		}
	}
}

// TestMultiTurnThinkingLift_ModelSuffix verifies that model names with a thinking
// suffix (e.g. deepseek-v4-pro(high)) still trigger the lift.
func TestMultiTurnThinkingLift_ModelSuffix(t *testing.T) {
	const thinkingText = "suffix model lift"
	inputJSON := `{
		"model": "deepseek-v4-pro(high)",
		"messages": [
			{
				"role": "user",
				"content": [{"type": "text", "text": "hi"}]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "` + thinkingText + `"},
					{"type": "text", "text": "hello"}
				]
			}
		]
	}`

	result := ConvertClaudeRequestToOpenAI("deepseek-v4-pro(high)", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	for _, msg := range resultJSON.Get("messages").Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if got := msg.Get("reasoning_content").String(); got != thinkingText {
			t.Errorf("model with suffix: reasoning_content = %q, want %q", got, thinkingText)
		}
	}
}

// TestMultiTurnThinkingLift_EmptyThinkingDropped verifies that empty/whitespace-only
// thinking blocks are NOT lifted (no point injecting empty reasoning_content from
// the translator side; the executor L2 fallback handles placeholder injection).
func TestMultiTurnThinkingLift_EmptyThinkingDropped(t *testing.T) {
	inputJSON := `{
		"model": "deepseek-v4-pro",
		"messages": [
			{
				"role": "user",
				"content": [{"type": "text", "text": "hi"}]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "   "},
					{"type": "text", "text": "hello"}
				]
			}
		]
	}`

	result := ConvertClaudeRequestToOpenAI("deepseek-v4-pro", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	for _, msg := range resultJSON.Get("messages").Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if rc := msg.Get("reasoning_content"); rc.Exists() && strings.TrimSpace(rc.String()) != "" {
			t.Errorf("empty thinking should not produce non-empty reasoning_content: %q", rc.String())
		}
	}
}

// TestResponseTranslatorThinkingOutput_NoSignature proves that the response
// translator generates type="thinking" blocks WITHOUT a signature field.
// This documents why the request-side DeepSeek bypass is required: GPT-style
// signature gating would always drop these blocks.
func TestResponseTranslatorThinkingOutput_NoSignature(t *testing.T) {
	// Simulate DeepSeek streaming response with reasoning_content
	deepSeekChunk := `{"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"The user asks about weather. I need to call get_weather."},"finish_reason":null}]}`

	var param any
	chunks := ConvertOpenAIResponseToClaude(
		nil, "",
		[]byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		nil,
		[]byte("data: "+deepSeekChunk),
		&param,
	)

	for _, chunk := range chunks {
		s := string(chunk)
		if gjson.Get(s, "content_block.type").String() == "thinking" {
			signature := gjson.Get(s, "content_block.signature").String()
			hasSignature := gjson.Get(s, "content_block.signature").Exists()

			t.Logf("Thinking block content_block: %s", gjson.Get(s, "content_block").Raw)

			if hasSignature {
				t.Errorf("generated thinking block has unexpected signature field: %s", signature)
			} else {
				t.Logf("CONFIRMED: Response translator generates type='thinking' WITHOUT signature")
				t.Logf("   This is expected — DeepSeek reasoning_content has no cryptographically signed provenance")
			}
		}
	}
}

// TestMultiTurnThinkingLift_RespectsDisabledThinking verifies that when the
// client explicitly disables thinking (Claude top-level thinking.type=disabled),
// historical thinking blocks are NOT lifted into reasoning_content. This keeps
// the translator lift symmetric with the executor-side hard-off gate
// (thinking.DeepSeekThinkingActive) so user intent to turn thinking off is
// honored and no CoT is uploaded for a non-thinking request.
func TestMultiTurnThinkingLift_RespectsDisabledThinking(t *testing.T) {
	const thinkingText = "CoT from a previous enabled turn."
	inputJSON := `{
		"model": "deepseek-v4-pro",
		"thinking": {"type": "disabled"},
		"messages": [
			{
				"role": "user",
				"content": [{"type": "text", "text": "hi"}]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "` + thinkingText + `"},
					{"type": "text", "text": "ok"}
				]
			}
		]
	}`

	result := ConvertClaudeRequestToOpenAI("deepseek-v4-pro", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	assistantMsg := resultJSON.Get("messages.1")
	if got := assistantMsg.Get("reasoning_content"); got.Exists() {
		t.Errorf("thinking.type=disabled must not lift thinking into reasoning_content, got %q", got.String())
	}
	// The thinking block is consumed without lifting (it never appears in the
	// OpenAI-format content array); only reasoning_content must stay absent.
	if got := assistantMsg.Get("content.0.thinking"); got.Exists() {
		t.Errorf("thinking block should not survive in content, got %q", got.String())
	}
}
