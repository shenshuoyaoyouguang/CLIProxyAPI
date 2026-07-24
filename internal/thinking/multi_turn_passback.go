package thinking

import (
	"fmt"

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

	result := body
	modified := false

	// Phase 1: lift real CoT into missing/empty reasoning_content.
	for i, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if !ShouldReplaceDeepSeekReasoningContent(msg.Get("reasoning_content")) {
			continue
		}
		lifted, ok := LiftDeepSeekReasoningText(msg)
		if !ok {
			continue
		}
		path := fmt.Sprintf("messages.%d.reasoning_content", i)
		updated, err := sjson.SetBytes(result, path, lifted)
		if err != nil {
			log.Errorf("deepseek reasoning: failed to lift reasoning_content at %s: %v", path, err)
			continue
		}
		result = updated
		modified = true
	}

	// Phase 2: placeholder for assistants still missing the field.
	// JSON null reasoning_content is treated as missing and standardized to ""
	// so the upstream never receives a bare null (which would yield HTTP 400).
	messages = gjson.GetBytes(result, "messages")
	for i, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		rc := msg.Get("reasoning_content")
		if rc.Exists() && rc.Type != gjson.Null {
			continue
		}
		path := fmt.Sprintf("messages.%d.reasoning_content", i)
		updated, err := sjson.SetBytes(result, path, "")
		if err != nil {
			log.Errorf("deepseek reasoning: failed to back-fill reasoning_content at %s: %v", path, err)
			continue
		}
		result = updated
		modified = true
	}

	if !modified {
		return body
	}
	return result
}
