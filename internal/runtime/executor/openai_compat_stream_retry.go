package executor

import (
	"io"

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
// thinking token consumption on retry.
// Degradation chain: max/xhigh → high → medium → low → minimal → (remove)
func degradeReasoningForRetry(body []byte) []byte {
	effort, format := detectReasoningEffort(body)
	if effort == "" {
		return body
	}
	next := degradeEffort(effort)
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
	}
	return body
}

func detectReasoningEffort(body []byte) (effort, format string) {
	if v := gjson.GetBytes(body, "reasoning_effort"); v.Exists() && v.Type == gjson.String {
		return v.String(), "flat"
	}
	if v := gjson.GetBytes(body, "reasoning.effort"); v.Exists() && v.Type == gjson.String {
		return v.String(), "nested"
	}
	return "", ""
}

func degradeEffort(effort string) string {
	switch effort {
	case "max", "xhigh":
		return "high"
	case "high":
		return "medium"
	case "medium":
		return "low"
	case "low":
		return "minimal"
	default:
		return ""
	}
}
