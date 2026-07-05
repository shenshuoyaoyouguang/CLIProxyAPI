# extra_body 剥离 + 流重试 + MiMo/DeepSeek 支持 Spec

## Why

当前分支 `merge/upstream-v7.2.50` 存在三个问题：(1) 严格 schema 上游（如 z.ai GLM）因请求体含 OpenAI SDK 私有字段 `extra_body` 而返回 400；(2) SSE 流式响应因上游 context overflow 或 thinking token 预算耗尽而意外断开时无降级重试机制；(3) 缺少 MiMo/DeepSeek 模型的 thinking provider 支持。本方案通过分阶段渐进式实现解决上述问题，每阶段独立可交付，风险隔离。

## What Changes

### 阶段 1：extra_body 字段剥离（低风险，独立可交付）
- 在 `Execute` 和 `ExecuteStream` 函数中，于 `ApplyPayloadConfigWithRequest` 之后剥离请求体顶层 `extra_body` 字段

### 阶段 2：SSE 流重试机制（中风险，需修复 Blocker）
- 新建 `openai_compat_stream_retry.go`，提供 `isRetryableStreamDisconnect`、`degradeReasoningForRetry`、`detectReasoningEffort`、`degradeEffort` 工具函数
- 重构 `ExecuteStream` 的 goroutine：将流读取逻辑抽取为 `readStream` 闭包，添加 EOF 重试（仅重试一次），重试时降级 `reasoning_effort`
- **修复 Blocker B1**：将 `var param any` 提升到 goroutine 作用域，确保 `readStream` 闭包和合成 `[DONE]` 共享同一翻译器状态
- **修复 Blocker B2**：`readStream` 内遇到 JSON 错误体时仅返回错误不上报，由外层统一上报，避免双重 `PublishFailure`
- **修复 Warning W1**：删除 `done` 死代码返回值，`readStream` 仅返回 `error`
- **修复 Warning W2**：外层错误处理排除 `context.Canceled`/`context.DeadlineExceeded`，避免正常取消被误报为失败
- **修复 Warning W3**：重试块补充 `helps.RecordAPIRequest` 和错误体日志，提升可观测性
- 保留 SSE 修复的 `SSENormalizer`/`flushNormalizer`/`sendChunks`，重试时重置 normalizer

### 阶段 3：MiMo/DeepSeek thinking provider 支持（高复杂度，需前置调研）
- **前置调研（3.0）**：验证 MiMo 上游响应格式是否含 `reasoning_content` 字段；澄清 MiMo 配置路径（`openai-compatibility` vs `*-api-key`）及实际 `UserDefined` 值
- 新建 `internal/modelkind/` 包（`IsDeepSeekModel`、`IsMIMOModel`）
- 新建 `internal/thinking/provider/deepseek/apply.go`（处理 `reasoning_effort` + `thinking.type=disabled`）
- 新建 `internal/thinking/provider/mimo/apply.go`（处理 `thinking.type` + `reasoning_effort` fallback + `mimoBoostMaxCompletion`）
- 修改 `thinking_providers.go` 添加 deepseek/mimo 的 blank import
- 修改 `apply.go`：`nativeProviderAppliers` 添加占位、`extractThinkingConfig` 添加 mimo 分支、新增 `extractMIMOConfig` 函数
- 修改 `validate.go`：`isBudgetCapableProvider` 添加 mimo、`isOpenAIFamily` 添加 xai/deepseek/mimo
- 修改 `strip.go`：`StripThinkingConfig` 添加 mimo/deepseek 分支
- 修改 `openai_compat_executor.go`：新增 `thinkingTargetForModel` 函数，3 处 `ApplyThinking` 调用替换为目标路由
- **不添加** `DefaultDisabled` 字段（当前分支无 default 注入逻辑，空块无效 - W4）
- **不添加** `norm.go`（`NormalizeEffort` 无调用方，死代码 - H1）
- **不添加** `convertReasoningToThinkingContent`（当前分支无此函数，不需要）
- **条件性添加**：若前置调研发现 MiMo 响应含 `reasoning_content`，则补充响应翻译逻辑或新建 `mimo_executor.go`（W7）
- 补充 thinking provider 独立测试（当前无）

## Impact

- **Affected specs**: `standardize-sse-event-ordering`（SSE normalizer 与流重试需协调，重试时重置 normalizer）
- **Affected code**:
  - `internal/runtime/executor/openai_compat_executor.go`（3 阶段均涉及）
  - `internal/runtime/executor/openai_compat_stream_retry.go`（新建）
  - `internal/modelkind/modelkind.go`（新建）
  - `internal/thinking/provider/deepseek/apply.go`（新建）
  - `internal/thinking/provider/mimo/apply.go`（新建）
  - `internal/runtime/executor/helps/thinking_providers.go`（修改）
  - `internal/thinking/apply.go`（修改）
  - `internal/thinking/validate.go`（修改）
  - `internal/thinking/strip.go`（修改）

## ADDED Requirements

### Requirement: extra_body 字段剥离

系统 SHALL 在向 strict schema 上游发送请求前剥离 OpenAI SDK 私有字段 `extra_body`，避免上游返回 400。

#### Scenario: 请求体含 extra_body 字段
- **WHEN** 客户端请求体含 `extra_body` 字段且目标上游为 strict schema（如 z.ai GLM）
- **THEN** 系统在 `ApplyPayloadConfigWithRequest` 之后、发送请求前删除 `extra_body` 字段
- **AND** 上游不再因 `extra_body` 返回 400

#### Scenario: 请求体不含 extra_body 字段
- **WHEN** 客户端请求体不含 `extra_body` 字段
- **THEN** `sjson.DeleteBytes` 返回原 body + nil error，无副作用

#### Scenario: Execute 和 ExecuteStream 均生效
- **WHEN** 调用 `Execute`（非流式）或 `ExecuteStream`（流式）
- **THEN** 两者均在 `ApplyPayloadConfigWithRequest` 之后剥离 `extra_body`

### Requirement: SSE 流重试机制

系统 SHALL 在 SSE 流式响应因上游意外断开（`io.ErrUnexpectedEOF`）且未收到任何 SSE 数据时，降级 `reasoning_effort` 并重试一次。

#### Scenario: 流意外断开且无 SSE 数据
- **WHEN** SSE 流读取返回 `io.ErrUnexpectedEOF` 且 `gotSSEData == false`
- **THEN** 系统降级 `reasoning_effort`（max/xhigh → high → medium → low → minimal → 移除）
- **AND** 系统重置 `SSENormalizer`（新流需新状态）
- **AND** 系统发起新请求重试一次
- **AND** 重试流通过同一 `out` channel 输出 chunks

#### Scenario: 流已收到 SSE 数据后断开
- **WHEN** SSE 流读取返回 `io.ErrUnexpectedEOF` 但 `gotSSEData == true`
- **THEN** 系统不重试，按原错误处理流程上报

#### Scenario: 翻译器状态跨重试保持
- **WHEN** 触发流重试
- **THEN** `var param any` 位于 goroutine 作用域，`readStream` 闭包和合成 `[DONE]` 共享同一 `param`
- **AND** 翻译器跨 chunk 状态（message_start/content_block 跟踪）不被破坏

#### Scenario: JSON 错误体单次上报
- **WHEN** SSE 流中收到 JSON 错误体（`{` 或 `[` 开头）
- **THEN** `readStream` 仅返回错误不上报
- **AND** 外层错误处理块统一调用 `PublishFailure` + 发送 error chunk + `RecordAPIResponseError`（各仅 1 次）

#### Scenario: context 取消不被误报为失败
- **WHEN** context 被取消（`context.Canceled`）或超时（`context.DeadlineExceeded`）
- **THEN** 系统不调用 `PublishFailure` 上报失败

#### Scenario: 重试请求可观测
- **WHEN** 触发流重试
- **THEN** 系统调用 `helps.RecordAPIRequest` 记录重试请求
- **AND** 重试响应非 2xx 时记录状态码和错误体到日志

### Requirement: ModelKind 模型识别包

系统 SHALL 提供 `internal/modelkind` 包，通过模型名前缀识别模型家族。

#### Scenario: DeepSeek 模型识别
- **WHEN** 模型名以 `deepseek-` 开头（大小写不敏感）
- **THEN** `IsDeepSeekModel(model)` 返回 `true`

#### Scenario: MiMo 模型识别
- **WHEN** 模型名以 `mimo-` 开头（大小写不敏感）
- **THEN** `IsMIMOModel(model)` 返回 `true`

#### Scenario: 其他模型
- **WHEN** 模型名不以 `deepseek-` 或 `mimo-` 开头
- **THEN** 两个函数均返回 `false`

### Requirement: DeepSeek Thinking Provider

系统 SHALL 注册 `deepseek` thinking provider，处理 DeepSeek 模型的 thinking 配置。

#### Scenario: 启用 thinking（Level 模式）
- **WHEN** config 为 `ModeLevel` 且 Level 非空
- **THEN** 系统删除 `thinking` 对象，设置 `reasoning_effort` 为归一化后的值
- **AND** `xhigh` 被映射为 `max`（DeepSeek 不接受 xhigh）

#### Scenario: 禁用 thinking（None 模式）
- **WHEN** config 为 `ModeNone` 且 Level 为空或 None
- **THEN** 系统删除 `thinking` 对象和 `reasoning_effort`
- **AND** 设置 `thinking.type=disabled`

#### Scenario: Budget 模式
- **WHEN** config 为 `ModeBudget`
- **THEN** 系统通过 `ConvertBudgetToLevel` 转换，再归一化为 DeepSeek 接受的 effort 值

### Requirement: MiMo Thinking Provider

系统 SHALL 注册 `mimo` thinking provider，处理 MiMo 模型的 thinking 配置。

#### Scenario: 启用 thinking（Level 模式）
- **WHEN** config 为 `ModeLevel` 且 Level 非空非 None
- **THEN** 系统设置 `thinking.type=enabled`

#### Scenario: 禁用 thinking（None 模式）
- **WHEN** config 为 `ModeNone` 且 Level 为空或 None
- **THEN** 系统设置 `thinking.type=disabled`

#### Scenario: Budget 模式提升 max_completion_tokens
- **WHEN** config 为 `ModeBudget` 且 Budget > 0
- **THEN** 系统设置 `thinking.type=enabled`
- **AND** 若当前 `max_completion_tokens` < Budget，则提升为 Budget

#### Scenario: 提取 MiMo 配置（thinking.type 优先）
- **WHEN** 请求体含 `thinking.type`
- **THEN** `extractMIMOConfig` 优先解析 `thinking.type`（enabled→LevelHigh，disabled→ModeNone）

#### Scenario: 提取 MiMo 配置（reasoning_effort fallback）
- **WHEN** 请求体不含 `thinking.type` 但含 `reasoning_effort`
- **THEN** `extractMIMOConfig` fallback 解析 `reasoning_effort`（none→ModeNone，low→ModeBudget/8192，medium→ModeBudget/24576，high/max→ModeBudget/64512，auto→ModeAuto）

### Requirement: Thinking Target 路由

系统 SHALL 通过 `thinkingTargetForModel` 函数将 MiMo/DeepSeek 模型路由到对应 thinking provider。

#### Scenario: DeepSeek 模型路由
- **WHEN** `baseModel` 以 `deepseek-` 开头
- **THEN** `thinkingTargetForModel` 返回 `"deepseek"`

#### Scenario: MiMo 模型路由
- **WHEN** `baseModel` 以 `mimo-` 开头
- **THEN** `thinkingTargetForModel` 返回 `"mimo"`

#### Scenario: 其他模型路由
- **WHEN** `baseModel` 不匹配 MiMo/DeepSeek 前缀
- **THEN** `thinkingTargetForModel` 返回 `defaultTarget`

#### Scenario: 三处 ApplyThinking 调用均使用路由
- **WHEN** 调用 `Execute`、`ExecuteStream`、`CountTokens` 的 `ApplyThinking`
- **THEN** 三处均使用 `thinkingTargetForModel(baseModel, to.String())` 替换 `to.String()`

## MODIFIED Requirements

### Requirement: SSE 流式响应处理（继承自 standardize-sse-event-ordering）

SSE 流式响应处理 SHALL 保留 `SSENormalizer` 集成，并在流重试时重置 normalizer 状态。

#### Scenario: 首次流使用 SSENormalizer
- **WHEN** `responseFormat == FormatClaude`
- **THEN** goroutine 初始化 `SSENormalizer`，`sendChunks` 通过 `normalizer.Process` 处理 chunks

#### Scenario: 重试时重置 SSENormalizer
- **WHEN** 触发流重试且 `responseFormat == FormatClaude`
- **THEN** 系统重新赋值 `normalizer = helps.NewSSENormalizer()`，闭包看到新实例

#### Scenario: 流结束 flushNormalizer
- **WHEN** 流读取完成（正常或错误）
- **THEN** 调用 `flushNormalizer()` 补全缺失的终结事件

### Requirement: Thinking Provider 注册

`nativeProviderAppliers` map SHALL 包含 deepseek 和 mimo 的 nil 占位，由 init() 注册覆盖。

#### Scenario: 占位存在
- **WHEN** 编译时
- **THEN** `nativeProviderAppliers` 含 `"deepseek": nil` 和 `"mimo": nil`

#### Scenario: init 注册覆盖
- **WHEN** `thinking_providers.go` 的 blank import 触发 deepseek/mimo 包的 init()
- **THEN** `RegisterProvider` 覆盖 nil 占位为实际 Applier

### Requirement: StripThinkingConfig 支持 MiMo/DeepSeek

`StripThinkingConfig` SHALL 为 mimo 和 deepseek 提供剥离路径。

#### Scenario: mimo 剥离
- **WHEN** provider 为 `mimo`
- **THEN** 剥离 `thinking` 和 `reasoning_effort` 路径

#### Scenario: deepseek 剥离
- **WHEN** provider 为 `deepseek`
- **THEN** 剥离 `thinking` 和 `reasoning_effort` 路径

### Requirement: Validate 配置支持 MiMo/DeepSeek

`isBudgetCapableProvider` SHALL 包含 mimo；`isOpenAIFamily` SHALL 包含 xai、deepseek、mimo。

#### Scenario: mimo 是 budget-capable
- **WHEN** 调用 `isBudgetCapableProvider("mimo")`
- **THEN** 返回 `true`

#### Scenario: deepseek 是 OpenAI family
- **WHEN** 调用 `isOpenAIFamily("deepseek")`
- **THEN** 返回 `true`

## REMOVED Requirements

（本方案不移除任何现有需求）
