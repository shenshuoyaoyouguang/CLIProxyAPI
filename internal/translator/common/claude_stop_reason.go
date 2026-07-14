package common

// MapOpenAIStopReasonToClaude maps OpenAI finish reasons to Claude stop_reason values.
func MapOpenAIStopReasonToClaude(reason string, _ bool) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "stop", "", "content_filter":
		return "end_turn"
	default:
		return "end_turn"
	}
}

// MapCodexStopReasonToClaude maps Codex response stop reasons to Claude stop_reason values.
func MapCodexStopReasonToClaude(reason string, hasToolUse bool) string {
	if hasToolUse {
		return "tool_use"
	}

	switch reason {
	case "", "stop", "completed":
		return "end_turn"
	case "max_tokens", "max_output_tokens":
		return "max_tokens"
	case "tool_use", "tool_calls", "function_call":
		return "end_turn"
	case "end_turn", "stop_sequence", "pause_turn", "refusal", "model_context_window_exceeded":
		return reason
	case "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}

// MapGeminiFinishReasonToClaude maps Gemini finish reasons to Claude stop_reason values.
func MapGeminiFinishReasonToClaude(reason string, hasToolUse bool) string {
	if hasToolUse {
		return "tool_use"
	}
	switch reason {
	case "MAX_TOKENS":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

// MapAntigravityFinishReasonToClaude maps Antigravity finish reasons to Claude stop_reason values.
func MapAntigravityFinishReasonToClaude(reason string, hasToolUse bool) string {
	return MapGeminiFinishReasonToClaude(reason, hasToolUse)
}

// MapInteractionsStopReasonToClaude maps Interactions completion state to Claude stop_reason values.
func MapInteractionsStopReasonToClaude(hasToolUse bool) string {
	if hasToolUse {
		return "tool_use"
	}
	return "end_turn"
}
