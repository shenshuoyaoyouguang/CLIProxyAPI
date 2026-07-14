package common

import "testing"

func TestClaudeStopReasonMappers(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "openai stop", got: MapOpenAIStopReasonToClaude("stop", false), want: "end_turn"},
		{name: "openai length", got: MapOpenAIStopReasonToClaude("length", false), want: "max_tokens"},
		{name: "openai tool calls", got: MapOpenAIStopReasonToClaude("tool_calls", true), want: "tool_use"},
		{name: "openai content filter", got: MapOpenAIStopReasonToClaude("content_filter", false), want: "end_turn"},
		{name: "codex stop with tool", got: MapCodexStopReasonToClaude("stop", true), want: "tool_use"},
		{name: "codex content filter", got: MapCodexStopReasonToClaude("content_filter", false), want: "refusal"},
		{name: "codex context window", got: MapCodexStopReasonToClaude("model_context_window_exceeded", false), want: "model_context_window_exceeded"},
		{name: "gemini max tokens", got: MapGeminiFinishReasonToClaude("MAX_TOKENS", false), want: "max_tokens"},
		{name: "gemini tool", got: MapGeminiFinishReasonToClaude("STOP", true), want: "tool_use"},
		{name: "antigravity unknown", got: MapAntigravityFinishReasonToClaude("UNKNOWN", false), want: "end_turn"},
		{name: "interactions tool", got: MapInteractionsStopReasonToClaude(true), want: "tool_use"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}
