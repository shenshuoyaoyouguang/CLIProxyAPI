package common

import "strings"

// ToolCallAccumulator accumulates streaming tool_call state.
// The type was previously duplicated in openai/claude, openai/gemini, and
// claude/openai/chat-completions; it now lives in the shared common package.
// StartEmitted is only used by translators that need to track whether
// content_block_start was already emitted (e.g. OpenAI→Claude); other
// translators can ignore it.
type ToolCallAccumulator struct {
	ID        string
	Name      string
	Arguments strings.Builder
	// StartEmitted tracks whether content_block_start has already been sent
	// for this tool index.
	StartEmitted bool
}
