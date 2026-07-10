package executor

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizeDeepSeekToolMessageReasoning ensures that assistant messages
// containing tool_calls also carry a reasoning_content field, which DeepSeek's
// thinking-mode API requires for multi-turn tool-calling conversations.
// Missing reasoning_content can cause HTTP 400 errors.
//
// If no modification is needed, the original body slice is returned unchanged.
func normalizeDeepSeekToolMessageReasoning(body []byte) []byte {
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
	latestReasoning := ""
	hasLatestReasoning := false
	for msgIdx := range msgs {
		msg := msgs[msgIdx]
		role := strings.TrimSpace(msg.Get("role").String())
		if role != "assistant" {
			continue
		}

		reasoning := msg.Get("reasoning_content")
		fallbackLatestReasoning := latestReasoning
		fallbackHasLatestReasoning := hasLatestReasoning
		// Only the most recent assistant turn with a real reasoning_content can
		// seed later tool-call fallback. Once a newer assistant turn omits it,
		// the older reasoning must not bleed into that newer turn.
		if reasoning.Exists() {
			reasoningText := reasoning.String()
			if strings.TrimSpace(reasoningText) != "" {
				latestReasoning = reasoningText
				hasLatestReasoning = true
			} else {
				latestReasoning = ""
				hasLatestReasoning = false
			}
		} else {
			latestReasoning = ""
			hasLatestReasoning = false
		}

		toolCalls := msg.Get("tool_calls")
		if !toolCalls.Exists() || !toolCalls.IsArray() || len(toolCalls.Array()) == 0 {
			continue
		}

		if reasoning.Exists() && strings.TrimSpace(reasoning.String()) != "" {
			continue
		}

		reasoningText := deepSeekFallbackReasoning(msg, fallbackHasLatestReasoning, fallbackLatestReasoning)
		path := fmt.Sprintf("messages.%d.reasoning_content", msgIdx)
		next, err := sjson.SetBytes(out, path, reasoningText)
		if err != nil {
			log.WithError(err).Debug("deepseek executor: failed to set assistant reasoning_content")
			return body
		}
		out = next
		patched++
	}

	if patched > 0 {
		log.WithField("patched_reasoning_messages", patched).Debug("deepseek executor: backfilled reasoning_content for tool-calling assistant messages")
	}
	return out
}

// deepSeekFallbackReasoning selects a reasoning_content value for an assistant
// message that lacks one. It prefers the message's "reasoning" field, then
// falls back to its own "content" field (string or array of text parts), then
// the most recent non-empty reasoning_content from prior assistant messages,
// and finally returns a placeholder string.
func deepSeekFallbackReasoning(msg gjson.Result, hasLatest bool, latest string) string {
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

	if hasLatest && strings.TrimSpace(latest) != "" {
		return latest
	}

	return "[reasoning unavailable]"
}
