// Package thinking provides DeepSeek-specific reasoning context management.
package thinking

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// FilterDeepSeekReasoningContentFromHistory applies DeepSeek's multi-turn context
// splicing rule to an OpenAI-format request body.
//
// Rule:
//   - Assistant messages without tool_calls have their reasoning_content removed.
//     If the message becomes empty (no content and no tool_calls), it is dropped
//     from the messages array.
//   - Assistant messages with tool_calls keep their reasoning_content so it is
//     passed back to the API in subsequent turns.
//
// The function is a no-op when the body is empty, invalid, or has no messages.
func FilterDeepSeekReasoningContentFromHistory(body []byte) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body
	}

	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
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

	result, errSetMessages := sjson.SetRawBytes(body, "messages", []byte("["+strings.Join(kept, ",")+"]"))
	if errSetMessages != nil {
		return body
	}
	return result
}

// filterAssistantMessage applies the DeepSeek reasoning_content rule to a single
// assistant message. It returns nil when the message should be dropped entirely.
func filterAssistantMessage(message gjson.Result) []byte {
	hasToolCalls := hasAssistantToolCalls(message)

	if !hasToolCalls {
		// Strip reasoning_content for non-tool assistant messages.
		msg, _ := sjson.DeleteBytes([]byte(message.Raw), "reasoning_content")
		content := gjson.GetBytes(msg, "content")
		if !content.Exists() || content.String() == "" {
			// No effective content left; drop the message.
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
