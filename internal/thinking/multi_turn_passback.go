package thinking

import (
	"bytes"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// MultiTurnReasoningPassback is the provider-family seam for multi-turn
// chain-of-thought field hygiene (history CoT continuity). It is orthogonal to
// ApplyThinking / ProviderApplier, which write request-level thinking flags.
//
// Invariants for Ensure:
//   - never overwrites non-empty reasoning_content
//   - no-op when Applies(model) is false
//   - DeepSeek: lift thinking_blocks / content thinking before "" placeholder
type MultiTurnReasoningPassback interface {
	Applies(model string) bool
	Ensure(body []byte, model string) []byte
}

var multiTurnPassbacks []MultiTurnReasoningPassback

func registerMultiTurnReasoningPassback(p MultiTurnReasoningPassback) {
	if p == nil {
		return
	}
	multiTurnPassbacks = append(multiTurnPassbacks, p)
}

func init() {
	registerMultiTurnReasoningPassback(deepSeekMultiTurnPassback{})
}

// EnsureMultiTurnReasoningPassback runs the first registered adapter whose
// Applies(model) is true (first-match-wins). If none apply, body is returned
// unchanged. Callers that skip passback for responses/compact must not call
// this (or call only when SkipPassback is false).
func EnsureMultiTurnReasoningPassback(body []byte, model string) []byte {
	for _, p := range multiTurnPassbacks {
		if p.Applies(model) {
			return p.Ensure(body, model)
		}
	}
	return body
}

// deepSeekMultiTurnPassback implements L4 passback for DeepSeek thinking mode
// (lift real CoT + thinking-active gate + empty-string placeholder).
//
// Pipeline (see deepseek_passback.go for shared predicates):
//  1. Model allowlist (RequiresDeepSeekReasoningPassback / Applies)
//  2. Hard-off when thinking.type == "disabled"
//  3. Soft-active: effort / enabled / history non-empty RC / liftable thinking
//  4. Phase 1 lift: promote thinking_blocks / content thinking into empty RC
//  5. Phase 2 placeholder: missing/null RC → ""
//
// Non-empty reasoning_content is never overwritten. DeepSeek multi-turn tool
// calls require the field; missing it yields HTTP 400.
//
// Chat completions only: this mutates messages[]. Callers skip for
// responses/compact (input-shaped payloads).
type deepSeekMultiTurnPassback struct{}

func (deepSeekMultiTurnPassback) Applies(model string) bool {
	return RequiresDeepSeekReasoningPassback(model)
}

func (deepSeekMultiTurnPassback) Ensure(body []byte, model string) []byte {
	if !RequiresDeepSeekReasoningPassback(model) {
		return body
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body
	}

	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}

	// Hard-off / soft-active gate before mutating history.
	if !DeepSeekThinkingActive(body, messages) {
		return body
	}

	// Single pass: compute the target reasoning_content for every assistant
	// message. Collecting updates first keeps the rebuild O(body) instead of
	// O(numMessages * body) (review M2).
	updates := make(map[int]string)
	for i, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		rc := msg.Get("reasoning_content")
		if rc.Exists() && rc.Type != gjson.Null && strings.TrimSpace(rc.String()) != "" {
			// Non-empty chain-of-thought is never overwritten.
			continue
		}
		// Lift real CoT from translated shapes (thinking_blocks / content
		// thinking) into missing or empty reasoning_content.
		if lifted, ok := LiftDeepSeekReasoningText(msg); ok {
			updates[i] = lifted
			continue
		}
		if rc.Exists() && rc.Type == gjson.Null {
			// JSON null reasoning_content is illegal upstream (HTTP 400);
			// standardize it to "" (issue #4).
			updates[i] = ""
			continue
		}
		if !rc.Exists() && assistantHasToolCalls(msg) {
			// DeepSeek multi-turn tool calls require the field; missing it
			// yields HTTP 400, so back-fill an empty placeholder. Plain-text
			// assistant turns stay untouched to keep the message shape stable
			// for upstream prompt caching (review M1).
			updates[i] = ""
		}
	}
	if len(updates) == 0 {
		return body
	}

	// Rebuild the messages array once instead of re-parsing the whole body per
	// updated message.
	var buf bytes.Buffer
	buf.Grow(len(messages.Raw) + 32)
	buf.WriteByte('[')
	first := true
	for i, msg := range messages.Array() {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		if value, ok := updates[i]; ok {
			updated, err := sjson.SetBytes([]byte(msg.Raw), "reasoning_content", value)
			if err != nil {
				// Expected failure path: the original message is kept verbatim,
				// so the request stays valid and only CoT continuity is lost.
				log.Warnf("deepseek reasoning: failed to set reasoning_content at messages.%d, keeping original message: %v", i, err)
				buf.WriteString(msg.Raw)
				continue
			}
			buf.Write(updated)
		} else {
			buf.WriteString(msg.Raw)
		}
	}
	buf.WriteByte(']')

	result, err := sjson.SetRawBytes(body, "messages", buf.Bytes())
	if err != nil {
		// Expected failure path: the untouched body is returned, so the request
		// still succeeds without reasoning_content passback.
		log.Warnf("deepseek reasoning: failed to replace messages array, sending body unchanged: %v", err)
		return body
	}
	return result
}

// assistantHasToolCalls reports whether an assistant message carries a non-empty
// tool_calls array. Only such turns need reasoning_content passback for DeepSeek
// multi-turn tool continuity.
func assistantHasToolCalls(msg gjson.Result) bool {
	tc := msg.Get("tool_calls")
	return tc.Exists() && tc.IsArray() && len(tc.Array()) > 0
}
