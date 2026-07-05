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

---

### [MiMo 集成] UserDefined 字段语义与配置路径的注释/代码不一致

**问题描述**

`thinking.IsUserDefinedModel` 函数的注释（`internal/thinking/apply.go` L111-116）声称
"openai-compatibility.*.models[] 和 *-api-key.models[] 都标记为 UserDefined=true"，
但实际代码行为相反：

- `buildOpenAICompatibilityConfigModels`（`sdk/cliproxy/service.go` L2478）设置
  `UserDefined: false`
- `buildConfigModels`（`sdk/cliproxy/service.go` L2518，用于 gemini/codex/claude/vertex
  等 *-api-key 路径）设置 `UserDefined: true`

MiMo 仅能通过 `openai-compatibility.*.models[]` 配置（代码库无 `mimo-api-key` 路径，
`config_openai_compat_test.go` 的 "Mimo CN" 测试配置证实了这一点），因此 MiMo 模型的
实际 `UserDefined` 值为 **false**。

**影响范围**

- `internal/thinking/apply.go` 的 `IsUserDefinedModel` 注释具有误导性
- `internal/thinking/provider/mimo/apply.go`（ed20fd04 版本）的 `applyCompatibleMimo`
  分支仅当 `UserDefined=true` 时调用，但 MiMo 经 openai-compatibility 配置时
  `UserDefined=false`，因此 `applyCompatibleMimo` 实际不会被触发
- 后续 AI agent 在实现 MiMo thinking provider 时，若依赖注释而非代码，会错误地
  假设 MiMo 走 `applyCompatibleMimo` 路径

**解决方案**

实现 MiMo thinking provider 时，应以代码为准：MiMo 走标准路径（非 user-defined），
依赖 `modelInfo.Thinking != nil`（openai-compatibility 默认注入
`ThinkingSupport{Levels: ["low","medium","high"]}`）。`applyCompatibleMimo` 分支
可作为防御性代码保留，但在当前配置路径下不会触发。

**记录位置**

- 调研依据：`sdk/cliproxy/service.go` L2448-2528、`internal/thinking/apply.go` L111-125、
  `internal/api/handlers/management/config_openai_compat_test.go` L17-29

---

### [MiMo 集成] 多轮工具调用必须回传 reasoning_content（与 Kimi 同款陷阱）

**问题描述**

MiMo 官方文档
（https://platform.xiaomimimo.com/docs/en-US/usage-guide/passing-back-reasoning_content）
明确要求：当 MiMo 思考模式开启且多轮对话历史中存在 tool_calls 时，**必须**在后续
assistant 消息中完整回传 `reasoning_content` 字段，否则 API 返回 400 错误。

此要求与 Kimi 完全一致。Kimi 通过 `kimi_executor.go` 的
`normalizeKimiToolMessageLinks` 函数处理：
- 检测含 tool_calls 但缺失 reasoning_content 的 assistant 消息
- 用最近的 reasoning 或 content 内容回填
- 最终回退到 `"[reasoning unavailable]"`

**影响范围**

- 若 MiMo 通过 `openai_compat_executor.go` 路由而不做请求体规范化，多轮工具调用
  会触发 400 错误
- 影响所有 Agent 类产品（TRAE、Cursor、Codex 等）的多轮工具调用场景
- ed20fd04 commit message 第 4 点提到"新增推理内容保留逻辑，适配
  MiMo/DeepSeek 等原生使用 reasoning_content 的模型"，对应
  `reasoning_preserve_errcontract_test.go`，但当前分支无此逻辑

**解决方案**

阶段 3 Task 19（条件性响应翻译）必须实现，且不仅是响应翻译——还需要**请求体**
规范化（类似 Kimi 的 `normalizeKimiToolMessageLinks`）。建议二选一：
1. 新建 `mimo_executor.go` 嵌入 `OpenAICompatExecutor`，在 Execute/ExecuteStream
   前(mi)调用类似的 `normalizeMimoToolMessageReasoning` 函数
2. 在 `openai_compat_executor.go` 中添加条件性规范化（当
   `modelkind.IsMIMOModel(baseModel)` 时调用）

**记录位置**

- 调研依据：MiMo 官方文档、`internal/runtime/executor/kimi_executor.go` L332-455、
  ed20fd04 commit message

---

### [MiMo 集成] 深度思考模式下 temperature/top_p 被强制锁定

**问题描述**

MiMo 官方文档（mimo.mi.com/docs）明确：在思考模式下，`mimo-v2.5-pro`、
`mimo-v2.5` 等模型**不支持**自定义 `temperature` 和 `top_p` 参数，即使传入也会被
强制采用推荐默认值 `1.0` 和 `0.95`。若客户端传入非默认值，可能导致请求被拒绝或
行为异常。

ed20fd04 的 `mimo_executor.go` 实现了 `mimoLockThinkingParams` 函数，在
`thinking.type="enabled"` 时强制设置 `temperature=1.0`、`top_p=0.95`，覆盖用户的
自定义值。

**影响范围**

- 当前分支的 spec/manual doc 决定**不添加** `mimo_executor.go`，因此缺少此锁定
  逻辑
- 风险：当用户通过 config 或 client 传入自定义 temperature/top_p 时，MiMo 深度
  思考模式可能不稳定

**解决方案**

若阶段 3 验证发现此问题导致 400 错误，需要：
1. 新建 `mimo_executor.go`（嵌入 OpenAICompatExecutor）实现 `mimoLockThinkingParams`
2. 或在 `openai_compat_executor.go` 的 `ApplyPayloadConfigWithRequest` 之后添加
   条件性锁定（当 `modelkind.IsMIMOModel(baseModel)` 且 `thinking.type="enabled"`）

**记录位置**

- 调研依据：MiMo 官方文档、ed20fd04 的 `internal/runtime/executor/mimo_executor.go`
