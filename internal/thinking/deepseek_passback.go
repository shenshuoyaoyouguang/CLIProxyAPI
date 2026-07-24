package thinking

import (
	"strings"

	"github.com/tidwall/gjson"
)

// NormalizeDeepSeekModelID trims, lowercases, strips thinking suffixes like
// "(high)", and strips optional provider prefixes (e.g. "deepseek/deepseek-v4-pro").
func NormalizeDeepSeekModelID(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return ""
	}
	if idx := strings.Index(model, "("); idx > 0 {
		model = model[:idx]
	}
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	return strings.TrimSpace(model)
}

// RequiresDeepSeekReasoningPassback reports whether the model requires
// reasoning_content multi-turn passback (DeepSeek V4 / V3.1 thinking mode, and
// the deprecated deepseek-reasoner alias that maps to v4-flash thinking mode).
//
// deepseek-chat is intentionally excluded: it maps to v4-flash non-thinking mode
// and must not receive passback injection. models.json also omits the thinking
// block for this id so ApplyThinking does not advertise levels for chat mode.
func RequiresDeepSeekReasoningPassback(model string) bool {
	m := NormalizeDeepSeekModelID(model)
	if m == "" || m == "deepseek-chat" {
		return false
	}
	switch m {
	case "deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v3.1", "deepseek-reasoner":
		return true
	}
	// Future V4 aliases / regional ids. Use prefix match (stricter than
	// strings.Contains) so unrelated IDs like "not-deepseek-v4" don't match.
	return strings.HasPrefix(m, "deepseek-v4")
}

// IsInactiveReasoningEffort reports whether a reasoning_effort value means
// thinking is off (or unset). OpenAI-family appliers write "none" for ModeNone;
// treating it as active would pollute non-thinking DeepSeek chats (review P1).
func IsInactiveReasoningEffort(effort string) bool {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "", "none", "off", "disabled":
		return true
	default:
		return false
	}
}

// ShouldReplaceDeepSeekReasoningContent is true when reasoning_content is
// missing or empty/whitespace so real CoT may be lifted into it. Non-empty
// values must never be overwritten.
func ShouldReplaceDeepSeekReasoningContent(rc gjson.Result) bool {
	if !rc.Exists() {
		return true
	}
	return strings.TrimSpace(rc.String()) == ""
}

// LiftDeepSeekReasoningText extracts chain-of-thought text from shapes that may
// appear after cross-protocol translation:
//   - thinking_blocks: [{type:"thinking", thinking:"..."}] (LiteLLM / Anthropic adapter)
//   - content as array with {type:"thinking", thinking:"..."}
//
// Uses GetThinkingText to correctly parse wrapped object thinking structures
// (e.g. {"thinking": {"text": "..."}}) that direct .String() access would miss.
//
// Returns ("", false) when no non-empty thinking text is found.
func LiftDeepSeekReasoningText(msg gjson.Result) (string, bool) {
	var parts []string

	if blocks := msg.Get("thinking_blocks"); blocks.Exists() && blocks.IsArray() {
		for _, block := range blocks.Array() {
			if t := block.Get("type").String(); t != "" && t != "thinking" {
				continue
			}
			text := strings.TrimSpace(GetThinkingText(block))
			if text != "" {
				parts = append(parts, text)
			}
		}
	}

	if content := msg.Get("content"); content.Exists() && content.IsArray() {
		for _, part := range content.Array() {
			if part.Get("type").String() != "thinking" {
				continue
			}
			text := strings.TrimSpace(GetThinkingText(part))
			if text != "" {
				parts = append(parts, text)
			}
		}
	}

	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n\n"), true
}

// DeepSeekThinkingActive reports whether this request should run L4 passback
// (lift + empty placeholder). Semantics:
//
//  1. thinking.type == "disabled" is a hard off (covers history pollution).
//  2. thinking.type == "enabled" is hard on (wins over inactive effort).
//  3. reasoning_effort that is inactive (none/off/disabled/"") is a soft off:
//     the gate only re-opens when history shows multi-tool reasoning continuity
//     (an assistant turn carried reasoning_content, even empty, followed by tool
//     calls). Liftable thinking_blocks alone do not re-open the gate — that would
//     re-pollute chat mode. This keeps tool-call chains working (issue #3) without
//     letting chat turns inherit placeholder injection.
//  4. reasoning_effort that is active is on.
//  5. Otherwise history with non-empty reasoning_content or liftable thinking
//     is on (multi-turn tool passback when the client omitted effort flags).
//  6. History with only empty-string reasoning_content re-opens the gate when
//     tool calls follow (issue #5): the empty field marks a prior thinking turn
//     whose chain-of-thought must be preserved for subsequent tool calls.
func DeepSeekThinkingActive(body []byte, messages gjson.Result) bool {
	tt := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "thinking.type").String()))
	if tt == "disabled" {
		return false
	}
	if tt == "enabled" {
		return true
	}

	if effort := gjson.GetBytes(body, "reasoning_effort"); effort.Exists() {
		if IsInactiveReasoningEffort(effort.String()) {
			// Explicit inactive effort: only re-open for multi-tool reasoning
			// continuity (real reasoning_content field + tool calls). Liftable
			// thinking_blocks alone must not re-pollute chat mode.
			return hasDeepSeekMultiToolReasoningContinuity(messages)
		}
		return true
	}

	if !messages.IsArray() {
		return false
	}
	for _, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if rc := msg.Get("reasoning_content"); rc.Exists() {
			if strings.TrimSpace(rc.String()) != "" {
				return true
			}
		}
		if _, ok := LiftDeepSeekReasoningText(msg); ok {
			return true
		}
	}
	// Issue #5: empty-string reasoning_content marks a prior thinking turn.
	// Re-open the gate when tool calls follow so the chain stays intact.
	return hasDeepSeekMultiToolReasoningContinuity(messages)
}

// hasDeepSeekMultiToolReasoningContinuity reports whether the message history
// shows a multi-turn tool scenario that requires reasoning_content passback
// continuity: at least one assistant message carries a reasoning_content field
// (even empty, which marks a prior thinking turn) AND at least one tool-call
// turn exists (role=tool or assistant with tool_calls).
//
// This is the narrow signal that re-opens the L4 gate under inactive effort
// (issue #3) or empty-only reasoning_content history (issue #5) without
// re-polluting plain chat turns.
func hasDeepSeekMultiToolReasoningContinuity(messages gjson.Result) bool {
	if !messages.IsArray() {
		return false
	}
	hasAssistantReasoningContent := false
	hasToolCall := false
	for _, msg := range messages.Array() {
		role := msg.Get("role").String()
		if role == "assistant" {
			if msg.Get("reasoning_content").Exists() {
				hasAssistantReasoningContent = true
			}
			if tc := msg.Get("tool_calls"); tc.Exists() && tc.IsArray() && len(tc.Array()) > 0 {
				hasToolCall = true
			}
		}
		if role == "tool" {
			hasToolCall = true
		}
	}
	return hasAssistantReasoningContent && hasToolCall
}
