// Package thinking provides DeepSeek-specific reasoning context management.
package thinking

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// safeOptions forces sjson to allocate a fresh buffer for every write instead
// of performing an in-place (unsafe) overwrite of the input backing array.
// This guarantees that gjson Raws (which share the backing array with the
// input body) are never mutated out from under us, eliminating a class of
// data races and memory-corruption bugs under concurrent calls.
var safeOptions = &sjson.Options{Optimistic: false}

// FilterDeepSeekReasoningContentFromHistory applies DeepSeek's multi-turn context
// splicing rule to an OpenAI-format request body.
//
// Rule:
//   - Assistant messages without tool_calls have their reasoning_content removed.
//     If the message becomes empty (no effective content and no tool_calls), it
//     is dropped from the messages array.
//   - Assistant messages with tool_calls keep their reasoning_content so it is
//     passed back to the API in subsequent turns.
//
// Defensive guarantees:
//   - The input body is deep-copied on entry, so the caller's buffer is never
//     shared with gjson Raws or sjson writes.
//   - All sjson writes use safeOptions (Optimistic: false); no in-place unsafe
//     overwrite of shared backing arrays.
//   - Malformed/empty/non-string content is handled; invalid inputs degrade to a
//     no-op without panicking, so callers always reach their cleanup path.
//
// The function is a no-op (returning the deep-copied body) when the body is
// empty, invalid, or has no messages array.
func FilterDeepSeekReasoningContentFromHistory(body []byte) []byte {
	// Deep-copy at the very first step to sever any shared backing array with
	// the caller's request buffer. Every gjson Raw and sjson write from here on
	// operates on this private copy.
	work := append([]byte(nil), body...)
	if len(work) == 0 || !gjson.ValidBytes(work) {
		return work
	}

	messages := gjson.GetBytes(work, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return work
	}

	kept := make([]string, 0, len(messages.Array()))
	messages.ForEach(func(_, message gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(message.Get("role").String()))
		if role != "assistant" {
			kept = append(kept, message.Raw)
			return true
		}

		filtered := filterAssistantMessage(message)
		if filtered != nil {
			kept = append(kept, string(filtered))
		}
		return true
	})

	result, errSetMessages := sjson.SetRawBytesOptions(work, "messages", []byte("["+strings.Join(kept, ",")+"]"), safeOptions)
	if errSetMessages != nil {
		// Never return a half-written buffer; the deep-copied work is structurally
		// intact, so returning it keeps the caller's resource cleanup reachable.
		return work
	}
	return result
}

// filterAssistantMessage applies the DeepSeek reasoning_content rule to a single
// assistant message. It returns nil when the message should be dropped entirely.
//
// The per-message DeleteBytes operates on a copy of message.Raw (which shares the
// deep-copied work buffer, never the caller's). DeleteBytes defaults to
// Optimistic=false and never performs an in-place overwrite, so the shared
// backing array is never mutated.
func filterAssistantMessage(message gjson.Result) []byte {
	hasToolCalls := hasAssistantToolCalls(message)

	if !hasToolCalls {
		// Strip reasoning_content for non-tool assistant messages.
		msg, errDelete := sjson.DeleteBytes([]byte(message.Raw), "reasoning_content")
		if errDelete != nil {
			// Fall back to the original message rather than risk a corrupted one.
			return []byte(message.Raw)
		}
		content := gjson.GetBytes(msg, "content")
		if !content.Exists() || content.Type != gjson.String || strings.TrimSpace(content.String()) == "" {
			// No effective content left (missing, non-string, or blank content):
			// drop the message without breaking the surrounding JSON array.
			return nil
		}
		return msg
	}

	// Tool-call assistant messages keep reasoning_content unchanged.
	return []byte(message.Raw)
}

func hasAssistantToolCalls(message gjson.Result) bool {
	toolCalls := message.Get("tool_calls")
	return toolCalls.Exists() && toolCalls.IsArray() && len(toolCalls.Array()) > 0
}
