package executor

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// MiMo locked parameters when deep thinking (thinking.type="enabled") is active.
// Per MiMo docs, these models do not support custom temperature/top_p in deep
// thinking mode, so we force the documented defaults.
const (
	mimoThinkingTemperature = 1.0
	mimoThinkingTopP        = 0.95
)

// normalizeMimoToolMessageReasoning ensures that assistant messages containing
// tool_calls also carry a reasoning_content field, which MiMo's API requires
// for multi-turn tool-calling conversations. Missing reasoning_content causes
// HTTP 400 errors.
//
// This mirrors Kimi's normalizeKimiToolMessageLinks pattern but is simplified
// to only backfill reasoning_content (MiMo does not need tool_call_id repair).
// If no modification is needed, the original body slice is returned unchanged.
func normalizeMimoToolMessageReasoning(body []byte) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body
	}

	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}

	msgs := messages.Array()
	out := body
	patched := 0
	for msgIdx := range msgs {
		msg := msgs[msgIdx]
		role := strings.TrimSpace(msg.Get("role").String())
		if role != "assistant" {
			continue
		}

		toolCalls := msg.Get("tool_calls")
		if !toolCalls.Exists() || !toolCalls.IsArray() || len(toolCalls.Array()) == 0 {
			continue
		}

		reasoning := msg.Get("reasoning_content")
		if reasoning.Exists() && strings.TrimSpace(reasoning.String()) != "" {
			continue
		}

		reasoningText := mimoFallbackReasoning(msg)
		path := fmt.Sprintf("messages.%d.reasoning_content", msgIdx)
		next, err := sjson.SetBytes(out, path, reasoningText)
		if err != nil {
			log.WithError(err).Debug("mimo executor: failed to set assistant reasoning_content")
			return body
		}
		out = next
		patched++
	}

	if patched > 0 {
		log.WithField("patched_reasoning_messages", patched).Debug("mimo executor: backfilled reasoning_content for tool-calling assistant messages")
	}
	return out
}

// mimoFallbackReasoning selects a reasoning_content value for an assistant
// message that lacks one. It prefers the message's "reasoning" field, then
// falls back to its "content" field (string or array of text parts), and
// finally returns a placeholder string.
func mimoFallbackReasoning(msg gjson.Result) string {
	if reasoning := msg.Get("reasoning"); reasoning.Exists() {
		if text := strings.TrimSpace(reasoning.String()); text != "" {
			return text
		}
	}

	content := msg.Get("content")
	if content.Type == gjson.String {
		if text := strings.TrimSpace(content.String()); text != "" {
			return text
		}
	}
	if content.IsArray() {
		parts := make([]string, 0, len(content.Array()))
		for _, item := range content.Array() {
			text := strings.TrimSpace(item.Get("text").String())
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	return "[reasoning unavailable]"
}

// mimoLockThinkingParams forces temperature=1.0 and top_p=0.95 when MiMo's
// thinking mode is enabled. MiMo's deep-thinking mode does not accept custom
// temperature/top_p values and may reject requests or behave unexpectedly.
//
// This must run AFTER ApplyPayloadConfigWithRequest so that config-level
// overrides are themselves overwritten by the locked values.
func mimoLockThinkingParams(body []byte) []byte {
	thinkingType := gjson.GetBytes(body, "thinking.type")
	if !thinkingType.Exists() || thinkingType.String() != "enabled" {
		return body
	}
	body, _ = sjson.SetBytes(body, "temperature", mimoThinkingTemperature)
	body, _ = sjson.SetBytes(body, "top_p", mimoThinkingTopP)
	return body
}
