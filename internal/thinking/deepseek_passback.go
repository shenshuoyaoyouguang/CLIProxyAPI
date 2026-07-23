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
// and must not receive passback injection even though models.json still lists a
// thinking block for compatibility metadata.
func RequiresDeepSeekReasoningPassback(model string) bool {
	m := NormalizeDeepSeekModelID(model)
	if m == "" || m == "deepseek-chat" {
		return false
	}
	switch m {
	case "deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v3.1", "deepseek-reasoner":
		return true
	}
	// Future V4 aliases / regional ids.
	return strings.Contains(m, "deepseek-v4")
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
// Returns ("", false) when no non-empty thinking text is found.
func LiftDeepSeekReasoningText(msg gjson.Result) (string, bool) {
	var parts []string

	if blocks := msg.Get("thinking_blocks"); blocks.Exists() && blocks.IsArray() {
		for _, block := range blocks.Array() {
			if t := block.Get("type").String(); t != "" && t != "thinking" {
				continue
			}
			text := strings.TrimSpace(block.Get("thinking").String())
			if text == "" {
				text = strings.TrimSpace(block.Get("text").String())
			}
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
			text := strings.TrimSpace(part.Get("thinking").String())
			if text == "" {
				text = strings.TrimSpace(part.Get("text").String())
			}
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
//  2. thinking.type == "enabled" is hard on.
//  3. reasoning_effort that is not inactive is on.
//  4. History with non-empty reasoning_content or liftable thinking is on
//     (only when not hard-off).
//
// Empty-only history reasoning_content does not alone activate the gate.
func DeepSeekThinkingActive(body []byte, messages gjson.Result) bool {
	tt := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "thinking.type").String()))
	if tt == "disabled" {
		return false
	}
	if tt == "enabled" {
		return true
	}

	if effort := gjson.GetBytes(body, "reasoning_effort"); effort.Exists() {
		if !IsInactiveReasoningEffort(effort.String()) {
			return true
		}
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
	return false
}
