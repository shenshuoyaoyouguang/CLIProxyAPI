package helps

import (
	"sort"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// contentTypePriority maps a content block's "type" to its sort priority.
// Lower values sort earlier. text first, then thinking/redacted_thinking,
// then tool_use; any other type sorts last.
func contentTypePriority(t string) int {
	switch t {
	case "text":
		return 0
	case "thinking", "redacted_thinking":
		return 1
	case "tool_use":
		return 2
	default:
		return 3
	}
}

// NormalizeNonStreamContentOrder standardizes the ordering of the "content"
// array in a non-stream Claude response JSON and ensures stop_reason is set.
//
// Ordering rule: content blocks are sorted by (type priority, index) where
// type priority is text < thinking < tool_use < other. Blocks without an
// "index" field keep their relative order within the same type (stable sort).
//
// stop_reason rule: if missing/null and a tool_use block exists, set to
// "tool_use"; otherwise set to "stop". Existing non-null values are preserved.
//
// The function is a no-op for:
//   - invalid JSON (returned as-is)
//   - non-Claude responses (root "type" != "message")
//   - responses without a "content" array (returned as-is, but stop_reason is
//     still filled when missing/null)
//
// usage is always preserved untouched.
func NormalizeNonStreamContentOrder(rawJSON []byte) []byte {
	// Reject obviously invalid JSON early to avoid gjson mis-parsing.
	if !gjson.ValidBytes(rawJSON) {
		return rawJSON
	}

	// Only normalize Claude "message" responses; leave OpenAI-format payloads
	// untouched so we never accidentally mutate foreign schemas.
	if t := gjson.GetBytes(rawJSON, "type"); !t.Exists() || t.String() != "message" {
		return rawJSON
	}

	content := gjson.GetBytes(rawJSON, "content")
	if !content.Exists() || !content.IsArray() {
		// No content array to reorder, but still ensure stop_reason is set.
		return ensureStopReason(rawJSON, false)
	}

	blocks := content.Array()
	if len(blocks) == 0 {
		// Empty content array: keep as-is but still fill stop_reason.
		return ensureStopReason(rawJSON, false)
	}

	// Build a sortable view of the blocks. We keep the raw JSON of each block
	// so we can re-append it verbatim with sjson.SetRawBytes.
	type item struct {
		raw       string
		typePrio  int
		index     int64
		hasIndex  bool
		origOrder int
	}
	items := make([]item, 0, len(blocks))
	hasToolUse := false
	for i, b := range blocks {
		tp := b.Get("type").String()
		if tp == "tool_use" {
			hasToolUse = true
		}
		idxRes := b.Get("index")
		it := item{
			raw:       b.Raw,
			typePrio:  contentTypePriority(tp),
			origOrder: i,
		}
		if idxRes.Exists() {
			it.index = idxRes.Int()
			it.hasIndex = true
		}
		items = append(items, it)
	}

	// Stable sort so blocks with equal (typePrio, index) — or no index at
	// all — keep their original relative order.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].typePrio != items[j].typePrio {
			return items[i].typePrio < items[j].typePrio
		}
		// Both lack index: keep original order (stable sort guarantees this,
		// so just return false).
		if !items[i].hasIndex && !items[j].hasIndex {
			return false
		}
		// Blocks without an index are considered equal to any indexed block
		// for ordering purposes; stable sort keeps original order.
		if !items[i].hasIndex || !items[j].hasIndex {
			return false
		}
		if items[i].index != items[j].index {
			return items[i].index < items[j].index
		}
		return false
	})

	// Rebuild the content array. We start from the original JSON (which keeps
	// every other field intact) and replace "content" with the reordered
	// array by appending each block's raw JSON to content.-1.
	out := make([]byte, len(rawJSON))
	copy(out, rawJSON)
	// Clear the existing content array so we can re-append without leftovers.
	// sjson.SetRawBytes with "content" replaces the whole array.
	out, _ = sjson.SetRawBytes(out, "content", []byte("[]"))
	for _, it := range items {
		out, _ = sjson.SetRawBytes(out, "content.-1", []byte(it.raw))
	}

	return ensureStopReason(out, hasToolUse)
}

// ensureStopReason fills stop_reason when it is missing or null. If a
// tool_use block is present the value becomes "tool_use"; otherwise "stop".
//
// Additionally, when a tool_use block is present but the existing
// stop_reason is a non-tool value (e.g. "end_turn" mapped from OpenAI's
// "stop" finish_reason), it is corrected to "tool_use" to match the
// Anthropic protocol semantics. Existing values of "tool_use",
// "max_tokens" or "stop_sequence" are preserved. usage is never modified.
func ensureStopReason(rawJSON []byte, hasToolUse bool) []byte {
	sr := gjson.GetBytes(rawJSON, "stop_reason")
	if sr.Exists() && sr.Type != gjson.Null {
		// Correct stop_reason when tool_use blocks are present but the
		// reason does not reflect a tool-use termination.
		if hasToolUse {
			switch sr.String() {
			case "tool_use", "max_tokens", "stop_sequence":
				// Keep as-is; these are legitimate reasons even with tool_use.
			default:
				out, _ := sjson.SetBytes(rawJSON, "stop_reason", "tool_use")
				return out
			}
		}
		return rawJSON
	}
	value := "stop"
	if hasToolUse {
		value = "tool_use"
	}
	out, _ := sjson.SetBytes(rawJSON, "stop_reason", value)
	return out
}
