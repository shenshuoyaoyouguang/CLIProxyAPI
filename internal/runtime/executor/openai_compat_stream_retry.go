package executor

import (
	"io"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// isRetryableStreamDisconnect reports whether a stream reading error represents
// a recoverable upstream disconnect. An unexpected EOF before any SSE data
// means the upstream closed the connection prematurely (e.g. context overflow,
// thinking token budget exhausted, transient network issue).
func isRetryableStreamDisconnect(err error, gotSSEData bool) bool {
	if err == nil || gotSSEData {
		return false
	}
	return err == io.ErrUnexpectedEOF
}

// degradeReasoningForRetry lowers the reasoning_effort by one notch to reduce
// thinking token consumption on retry. Supports multiple effort field formats:
//   - flat:     reasoning_effort (OpenAI, Kimi, DeepSeek)
//   - nested:   reasoning.effort (Codex, xAI)
//   - claude:   output_config.effort (Claude adaptive thinking)
//
// The degradation uses the canonical chain from internal/thinking:
// max/xhigh → high → medium → low → minimal → (remove)
//
// For Claude's output_config.effort, "minimal" is not a valid upstream value,
// so it is treated as field removal (same as empty).
func degradeReasoningForRetry(body []byte) []byte {
	effort, format := detectReasoningEffort(body)
	if effort == "" {
		return body
	}
	next := string(thinking.DegradeThinkingLevel(thinking.ThinkingLevel(effort)))
	switch format {
	case "flat":
		if next == "" {
			body, _ = sjson.DeleteBytes(body, "reasoning_effort")
		} else {
			body, _ = sjson.SetBytes(body, "reasoning_effort", next)
		}
	case "nested":
		if next == "" {
			body, _ = sjson.DeleteBytes(body, "reasoning.effort")
		} else {
			body, _ = sjson.SetBytes(body, "reasoning.effort", next)
		}
	case "claude_effort":
		// Claude adaptive thinking does not support "minimal" effort.
		// Treat minimal as field removal.
		if next == "" || next == "minimal" {
			body, _ = sjson.DeleteBytes(body, "output_config.effort")
			if oc := gjson.GetBytes(body, "output_config"); oc.Exists() && oc.IsObject() && len(oc.Map()) == 0 {
				body, _ = sjson.DeleteBytes(body, "output_config")
			}
		} else {
			body, _ = sjson.SetBytes(body, "output_config.effort", next)
		}
	}
	return body
}

// detectReasoningEffort detects the reasoning effort field in a translated
// upstream request body. It supports three formats:
//   - "flat":   reasoning_effort ("...")
//   - "nested": reasoning.effort ("...")
//   - "claude_effort": output_config.effort ("...")
//
// Flat format takes priority over nested when both exist. Claude format is
// checked separately and only when the other two are absent.
func detectReasoningEffort(body []byte) (effort, format string) {
	if v := gjson.GetBytes(body, "reasoning_effort"); v.Exists() && v.Type == gjson.String {
		return v.String(), "flat"
	}
	if v := gjson.GetBytes(body, "reasoning.effort"); v.Exists() && v.Type == gjson.String {
		return v.String(), "nested"
	}
	if v := gjson.GetBytes(body, "output_config.effort"); v.Exists() && v.Type == gjson.String {
		return v.String(), "claude_effort"
	}
	return "", ""
}
