package executor

import (
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ensureDeepSeekReasoningContent implements L4 passback for DeepSeek thinking
// mode (lift real CoT + thinking-active gate + empty-string placeholder).
//
// Pipeline (see internal/thinking/deepseek_passback.go for shared semantics):
//  1. Model allowlist (RequiresDeepSeekReasoningPassback)
//  2. Hard-off when thinking.type == "disabled"
//  3. Soft-active: effort / enabled / history non-empty RC / liftable thinking
//  4. Phase 1 lift: promote thinking_blocks / content thinking into empty RC
//  5. Phase 2 placeholder: missing RC → ""
//
// Non-empty reasoning_content is never overwritten. DeepSeek multi-turn tool
// calls require the field; missing it yields HTTP 400.
//
// Chat completions only: this mutates messages[]. responses/compact uses input
// and is skipped by the OpenAICompat executor (explicit no-op).
func ensureDeepSeekReasoningContent(body []byte, model string) []byte {
	if !thinking.RequiresDeepSeekReasoningPassback(model) {
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
	if !thinking.DeepSeekThinkingActive(body, messages) {
		return body
	}

	result := body
	modified := false

	// Phase 1: lift real CoT into missing/empty reasoning_content.
	for i, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if !thinking.ShouldReplaceDeepSeekReasoningContent(msg.Get("reasoning_content")) {
			continue
		}
		lifted, ok := thinking.LiftDeepSeekReasoningText(msg)
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
