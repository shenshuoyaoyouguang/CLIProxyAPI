package thinking

import (
	"strings"

	"github.com/tidwall/gjson"
)

// NormalizeProviderModelID trims, lowercases, strips thinking suffixes like
// "(high)", and strips optional provider prefixes (e.g. "deepseek/deepseek-v4-pro").
// It is shared by the DeepSeek and NVIDIA model routing paths.
func NormalizeProviderModelID(model string) string {
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
// reasoning_content multi-turn passback (DeepSeek V4 thinking mode).
//
// deepseek-chat is intentionally excluded: it maps to v4-flash non-thinking mode
// and must not receive passback injection. models.json also omits the thinking
// block for this id so ApplyThinking does not advertise levels for chat mode.
func RequiresDeepSeekReasoningPassback(model string) bool {
	m := NormalizeProviderModelID(model)
	if m == "" {
		return false
	}
	switch m {
	case "deepseek-v4-pro", "deepseek-v4-flash":
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
	// The same CoT can be present in both thinking_blocks and the content array
	// after translation (e.g. LiteLLM/Anthropic adapters emit both shapes);
	// concatenating it twice would duplicate the reasoning text in the
	// passback payload.
	seen := make(map[string]struct{})
	addPart := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, dup := seen[text]; dup {
			return
		}
		seen[text] = struct{}{}
		parts = append(parts, text)
	}

	if blocks := msg.Get("thinking_blocks"); blocks.Exists() && blocks.IsArray() {
		for _, block := range blocks.Array() {
			if t := block.Get("type").String(); t != "" && t != "thinking" {
				continue
			}
			addPart(GetThinkingText(block))
		}
	}

	if content := msg.Get("content"); content.Exists() && content.IsArray() {
		for _, part := range content.Array() {
			if part.Get("type").String() != "thinking" {
				continue
			}
			addPart(GetThinkingText(part))
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
//
// The gate decision is produced by scanDeepSeekPassback, which also collects the
// per-message lift results that Ensure's update pass reuses, so thinking_blocks /
// content arrays are parsed once per request instead of once per message twice
// (review P4).
func DeepSeekThinkingActive(body []byte, messages gjson.Result) bool {
	return scanDeepSeekPassback(body, messages).active
}

// deepSeekPassbackScan carries the single history scan shared by the L4 gate
// (DeepSeekThinkingActive) and the update pass (Ensure). Both used to call
// LiftDeepSeekReasoningText per assistant message — the gate once and the update
// pass again — re-parsing thinking_blocks/content arrays twice per request on
// tool-heavy histories (review P4). Collecting the lifts once lets both
// consumers share a single scan.
type deepSeekPassbackScan struct {
	// active mirrors the DeepSeekThinkingActive gate decision.
	active bool
	// lifted maps an assistant-message index in the messages array to its lifted
	// reasoning text. Absence means the message is not liftable.
	lifted map[int]string
}

// scanDeepSeekPassback evaluates the L4 gate and collects the lifted reasoning
// text for every assistant message in a single pass over the history. Ensure
// reuses the lifted map in its update loop instead of lifting a second time.
//
// The cheap gate decisions (thinking.type hard-off/hard-on, explicit
// reasoning_effort) run before any lift parsing, so a request that disables
// thinking pays zero history parsing; the lift map is built only when a
// consumer needs it (the default history decision, or the update pass once the
// gate opened).
func scanDeepSeekPassback(body []byte, messages gjson.Result) deepSeekPassbackScan {
	scan := deepSeekPassbackScan{}

	tt := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "thinking.type").String()))
	if tt == "disabled" {
		return scan
	}
	if tt == "enabled" {
		scan.active = true
		scan.collectLifts(messages)
		return scan
	}
	if effort := gjson.GetBytes(body, "reasoning_effort"); effort.Exists() {
		if IsInactiveReasoningEffort(effort.String()) {
			// Explicit inactive effort: only re-open for multi-tool reasoning
			// continuity (real reasoning_content field + tool calls). Liftable
			// thinking_blocks alone must not re-pollute chat mode.
			scan.active = hasDeepSeekMultiToolReasoningContinuity(messages)
		} else {
			scan.active = true
		}
		if !scan.active {
			// Soft off without tool continuity: the update pass will not run,
			// so no lift parsing is needed.
			return scan
		}
		scan.collectLifts(messages)
		return scan
	}

	// No explicit flags: the gate decision and the lift collection share one
	// history scan (review P4). The default history path decides on the
	// prebuilt lifts instead of re-parsing thinking_blocks/content arrays.
	scan.collectLifts(messages)
	scan.active = deepSeekThinkingActive(messages, scan.lifted)
	return scan
}

// collectLifts fills the scan's lifted map with the reasoning text of every
// liftable assistant message. It is called only when a consumer needs the map:
// the default history gate, or the update pass once the gate opened.
func (scan *deepSeekPassbackScan) collectLifts(messages gjson.Result) {
	if !messages.IsArray() {
		return
	}
	if scan.lifted == nil {
		scan.lifted = make(map[int]string)
	}
	for i, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if lifted, ok := LiftDeepSeekReasoningText(msg); ok {
			scan.lifted[i] = lifted
		}
	}
}

// deepSeekThinkingActive implements the default history gate used when the
// request carries no thinking flags: history with non-empty reasoning_content
// or a liftable thinking block opens the gate, so multi-turn tool passback
// keeps working when the client omitted effort flags.
func deepSeekThinkingActive(messages gjson.Result, lifted map[int]string) bool {
	if !messages.IsArray() {
		return false
	}
	for i, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if rc := msg.Get("reasoning_content"); rc.Exists() {
			if strings.TrimSpace(rc.String()) != "" {
				return true
			}
		}
		if _, ok := lifted[i]; ok {
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
