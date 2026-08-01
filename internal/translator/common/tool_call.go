package common

import "strings"

// ToolCallAccumulator 累积流式 tool_call 的状态。
// 该类型原先在 openai/claude、openai/gemini、claude/openai/chat-completions 三个包中
// 重复定义，现统一抽到 common 包。
// StartEmitted 仅在需要追踪 content_block_start 是否已发出的翻译器中使用
// （如 OpenAI→Claude），其余翻译器可忽略该字段。
type ToolCallAccumulator struct {
	ID        string
	Name      string
	Arguments strings.Builder
	// StartEmitted tracks whether content_block_start has already been sent
	// for this tool index.
	StartEmitted bool
}
