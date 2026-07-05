# Agent Collaboration Notes

This document records unexpected situations and pitfalls encountered during
development, to help subsequent AI agents avoid stepping into the same traps.
Each entry follows the format: `### [Category] Problem Description`, followed
by detailed description, impact scope, and suggested solutions.

---

### [SSE 标准化] 切分换行注入陷阱

**问题描述**

在处理 SSE(Server-Sent Events)字节流时,多处代码使用
`bytes.Split(data, []byte("\n"))` 按行切分。当上游使用 CRLF(`\r\n`)行尾时,
切分后每行会残留尾随的 `\r`。这些残留的 `\r` 会被带入后续的 JSON 解析、
prefix 检查等逻辑,导致:

1. `gjson.ValidBytes` 因 JSON 末尾有 `\r` 而判定为非法 JSON,使得模型名重写
   (`rewriteModelInResponse`)被静默跳过。
2. `bytes.HasPrefix(line, []byte("event:"))` 虽然不受影响,但 line 末尾的 `\r`
   会在后续 `append(pendingEvent, '\n')` 等操作中意外混入输出,造成重复换行
   注入(`data: {1}\n\n\n`)。

**影响范围**

- `sdk/cliproxy/auth/response_model_rewriter.go`:`rewriteSSEPayloadLines`、
  `RewriteChunk`、`extractLastDataPayload` 三处 `bytes.Split` 调用
- `internal/translator/claude/openai/chat-completions/claude_openai_response.go`:
  `ConvertClaudeResponseToOpenAINonStream` 中的 `bytes.Split` 调用
- 其他使用 `bufio.NewScanner` 的位置不受影响(scanner 自动去除行尾换行)

**解决方案**

在所有 `bytes.Split(..., []byte("\n"))` 切分 SSE 字节的位置,对切分后的每行
执行 `bytes.TrimRight(line, "\r\n")` 去除尾随换行符。新增工具函数
`splitSSELines`(`internal/runtime/executor/helps/sse_normalizer.go`)提供了
统一的切分+TrimRight 实现,供新代码复用。

**记录位置**

- 修复提交:`internal/runtime/executor/helps/sse_normalizer.go`(splitSSELines)、
  `sdk/cliproxy/auth/response_model_rewriter.go`、
  `internal/translator/claude/openai/chat-completions/claude_openai_response.go`
- 测试:`internal/runtime/executor/helps/sse_normalizer_test.go` 的
  `TestSplitSSELines` 覆盖 CRLF 场景

---

### [SSE 标准化] event 前缀多空格解析陷阱

**问题描述**

SSE 协议规范允许 `event:` 字段后跟任意数量的空格。但现有代码中解析 `event:`
前缀的逻辑(如 `bytes.CutPrefix(line, []byte("event: "))`)只处理单空格情况。
当上游 provider 发送 `event:  message_start`(两个或更多空格)时,事件类型
无法被正确识别,导致:

1. 事件类型被误判为空字符串或包含多余空格的字符串。
2. 后续的 switch/case 分支无法匹配,事件被丢弃或误处理。
3. Claude SSE 协议标准化器(SSENormalizer)无法正确分类事件,导致缓冲重排
   逻辑失效。

**影响范围**

- 所有从 SSE 行提取事件类型的位置,特别是:
  - `internal/runtime/executor/helps/sse_normalizer.go` 的 `sseEventType` 函数
  - 任何依赖 `event:` 前缀解析的 translator 或 handler

**解决方案**

使用健壮的 `sseEventType` 解析器(`internal/runtime/executor/helps/sse_normalizer.go`),
它使用 `bytes.CutPrefix(line, []byte("event:"))` 去除前缀后,循环跳过任意数量
的空格和 tab,正确提取事件类型。该函数容忍 `event:` 后任意空白字符(空格、tab)
的组合,符合 SSE 协议规范。

**记录位置**

- 实现:`internal/runtime/executor/helps/sse_normalizer.go` 的 `sseEventType` 函数
- 测试:`internal/runtime/executor/helps/sse_normalizer_test.go` 的
  `TestSSEEventType` 覆盖单空格/多空格/tab 混合/CRLF 等场景

---

### [协议合规] SSENormalizer Flush 默认 stop_reason 使用了非法值 "stop"

**问题描述**

`sse_normalizer.go` 的 `Flush` 方法在合成缺失的 `message_delta` 时,默认
`stop_reason` 为 `"stop"`。但 Anthropic 协议合法的 `stop_reason` 值仅为:
`end_turn`、`max_tokens`、`stop_sequence`、`tool_use`。`"stop"` 不是合法值,
严格遵循协议的客户端(如 Anthropic 官方 SDK)会拒绝或误解析该事件。

**影响范围**

- `internal/runtime/executor/helps/sse_normalizer.go` 的 `Flush` 方法
- `internal/runtime/executor/helps/sse_normalizer_test.go` 的
  `TestSSENormalizer_FlushCompletesMissing` 测试(曾用错误断言 `"stop"`
  固化了该 bug)

**解决方案**

将默认值改为 `"end_turn"`(与 translator 中 `mapOpenAIFinishReasonToAnthropic`
将 OpenAI `"stop"` 映射为 `"end_turn"` 的语义一致)。同步修正测试断言。

**记录位置**

- 修复:`internal/runtime/executor/helps/sse_normalizer.go` 的 `Flush` 方法
- 测试:`internal/runtime/executor/helps/sse_normalizer_test.go`
- 注意:后续 AI agent 修改该测试时,不要将断言回退为 `"stop"`

---

### [协议合规] message_delta 必须在所有 content_block_stop 之后发出

**问题描述**

`SSENormalizer.handleMessageDelta` 原先直接透传 `message_delta` 事件,不检查
`activeBlocks` 是否已全部关闭。若上游先发 `message_delta` 再发
`content_block_stop`,客户端会看到 `message_delta` 出现在
`content_block_stop` 之前,违反 Anthropic 协议要求:"message_delta 必须在
所有 content_block_stop 之后、message_stop 之前"。

**影响范围**

- `internal/runtime/executor/helps/sse_normalizer.go` 的
  `handleMessageDelta` 与 `releaseReady`

**解决方案**

`handleMessageDelta` 在 `len(activeBlocks) > 0` 时将事件缓冲到 `pending`;
`releaseReady` 增加 `message_delta` 专门分支,仅在 `activeBlocks` 为空时
释放并标记 `messageDeltaSent`。

**记录位置**

- 修复:`internal/runtime/executor/helps/sse_normalizer.go`

---

### [协议合规] 非流式响应中 tool_use 块存在时 stop_reason 应为 "tool_use"

**问题描述**

`ConvertOpenAIResponseToClaudeNonStream` 在 `finish_reason="stop"` 时映射为
`stop_reason="end_turn"`,但如果 content 数组中同时存在 `tool_use` 块(来自
OpenAI 的 `tool_calls`),`stop_reason` 仍为 `"end_turn"` 而非 `"tool_use"`。
流式路径有 `effectiveOpenAIFinishReason` 通过 `SawToolCall` 覆盖为
`"tool_calls"` 再映射为 `"tool_use"`,但非流式路径缺少等价修正。

`NormalizeNonStreamContentOrder` 的 `ensureStopReason` 原先仅在 stop_reason
缺失时填充,不修正已存在的非 null 值。

**影响范围**

- `internal/runtime/executor/helps/normalize_nonstream.go` 的 `ensureStopReason`
- `internal/translator/openai/claude/openai_claude_response.go` 的
  `ConvertOpenAIResponseToClaudeNonStream`

**解决方案**

`ensureStopReason` 在 `hasToolUse==true` 且现有 stop_reason 不属于
`{tool_use, max_tokens, stop_sequence}` 时,强制改为 `"tool_use"`。

**记录位置**

- 修复:`internal/runtime/executor/helps/normalize_nonstream.go`
