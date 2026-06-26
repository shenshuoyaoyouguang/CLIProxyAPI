package executor

import (
	"io"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// isRetryableStreamDisconnect reports whether a stream reading error represents
// a recoverable upstream disconnect. An unexpected EOF before any [DONE] marker
// means the upstream closed the connection prematurely (e.g. context overflow,
// thinking token budget exhausted, transient network issue). This is retryable.
//
// A normal io.EOF or an error after [DONE] is not retryable — the stream ended
// as expected (possibly without the marker, which we handle with a synthetic DONE).
func isRetryableStreamDisconnect(err error, gotDoneMarker bool) bool {
	if err == nil || gotDoneMarker {
		return false
	}
	return err == io.ErrUnexpectedEOF
}

// degradeReasoningForRetry lowers the reasoning_effort (or reasoning.effort for
// Codex/Responses format) by one notch to reduce thinking token consumption on
// retry. If no reasoning field is present, returns payload unchanged.
//
// Degradation chain: max/xhigh → high → medium → low → minimal → (remove field)
// For Codex format: reasoning.effort follows the same chain.
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

// detectReasoningEffort finds reasoning_effort (flat) or reasoning.effort (nested)
// and returns the value and format.
func detectReasoningEffort(body []byte) (effort, format string) {
	if v := gjson.GetBytes(body, "reasoning_effort"); v.Exists() && v.Type == gjson.String {
		return v.String(), "flat"
	}
	if v := gjson.GetBytes(body, "reasoning.effort"); v.Exists() && v.Type == gjson.String {
		return v.String(), "nested"
	}
	return "", ""
}

// degradeEffort returns the next-lower effort level, or "" to indicate removal.
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
