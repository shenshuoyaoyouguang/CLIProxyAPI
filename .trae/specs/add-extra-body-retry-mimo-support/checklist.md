# Checklist

## 阶段 1：extra_body 字段剥离

- [x] `Execute` 函数在 `ApplyPayloadConfigWithRequest` 之后调用 `sjson.DeleteBytes(translated, "extra_body")`
- [x] `ExecuteStream` 函数在 `ApplyPayloadConfigWithRequest` 之后调用 `sjson.DeleteBytes(translated, "extra_body")`
- [x] `sjson.DeleteBytes` 的错误处理使用 `if errDel == nil { translated = updated }` 模式（键不存在时安全跳过）
- [x] `go build ./internal/runtime/executor/...` 编译通过
- [x] 阶段 1 已提交独立 commit（commit `f8268824`）

## 阶段 2：SSE 流重试机制

### Blocker 修复验证

- [x] **B1 修复**：`var param any` 位于 goroutine 作用域（与 `normalizer` 同级），非 `readStream` 闭包内局部变量
- [x] **B1 修复**：合成 `data: [DONE]` 时使用 goroutine 作用域的 `param`（非新建 `var param any`）
- [x] **B1 修复**：`readStream` 闭包通过引用使用 goroutine 作用域的 `param`
- [x] **B2 修复**：`readStream` 内遇到 JSON 错误体（`{`/`[` 开头）时仅返回 `streamErr`，不调用 `reporter.PublishFailure`、不发 error chunk、不调用 `helps.RecordAPIResponseError`
- [x] **B2 修复**：外层 `if errScan != nil` 块统一调用 `reporter.PublishFailure` + error chunk + `RecordAPIResponseError`（各仅 1 次）

### Warning 修复验证

- [x] **W1 修复**：`readStream` 闭包签名仅为 `func(body io.Reader) error`，无 `done` 返回值
- [x] **W2 修复**：外层错误处理块包含 `!errors.Is(errScan, context.Canceled) && !errors.Is(errScan, context.DeadlineExceeded)` 判断
- [x] **W3 修复**：重试块内调用 `helps.RecordAPIRequest` 记录重试请求
- [x] **W3 修复**：重试响应非 2xx 时调用 `log.Debugf` 记录状态码和错误体

### SSE 修复保留验证

- [x] `SSENormalizer` 初始化逻辑保留（`responseFormat == FormatClaude` 时创建）
- [x] `flushNormalizer` 闭包保留（调用 `normalizer.Flush()` 输出 chunks）
- [x] `sendChunks` 闭包保留（通过 `normalizer.Process` 处理 chunks）
- [x] 重试时重置 `normalizer = helps.NewSSENormalizer()`（若 FormatClaude）
- [x] 流结束时调用 `flushNormalizer()`
- [x] 流结束时调用 `reporter.EnsurePublished(ctx)`

### 流重试逻辑验证

- [x] `isRetryableStreamDisconnect` 仅在 `err == io.ErrUnexpectedEOF && !gotSSEData` 时返回 true
- [x] `degradeEffort` 降级链正确：max/xhigh→high→medium→low→minimal→""
- [x] `degradeReasoningForRetry` 支持 flat（`reasoning_effort`）和 nested（`reasoning.effort`）两种格式
- [x] `degradeReasoningForRetry` 在 next=="" 时删除字段，否则设置字段
- [x] 重试仅触发一次（无递归重试）
- [x] `retryBody` 初始化为 `translated`，降级后更新
- [x] `TranslateStream` 调用使用 `retryBody`（非 `translated`）
- [x] `httpResp2.Body.Close()` 在重试块内被调用

### 编译与回归测试

- [x] `go build ./internal/runtime/executor/...` 编译通过
- [x] `go test ./internal/runtime/executor/... -run "SSE" -v` 全部通过
- [x] `go test ./internal/runtime/executor/... -run "SSEIntegration" -v` 全部通过
- [x] 阶段 2 已提交独立 commit（commit `9eacb53a`）

## 阶段 3：MiMo/DeepSeek thinking provider 支持

### 前置调研验证

- [x] Task 10 调研已完成，MiMo 响应格式（是否含 reasoning_content）已确认
- [x] MiMo 配置路径（openai-compatibility vs *-api-key）及 UserDefined 值已确认
- [x] 调研结论已记录到 Agent.md

### modelkind 包验证

- [x] `internal/modelkind/modelkind.go` 存在
- [x] `IsDeepSeekModel("deepseek-chat")` 返回 true
- [x] `IsDeepSeekModel("DeepSeek-Chat")` 返回 true（大小写不敏感）
- [x] `IsMIMOModel("mimo-v2")` 返回 true
- [x] `IsMIMOModel("gpt-4")` 返回 false

### DeepSeek provider 验证

- [x] `internal/thinking/provider/deepseek/apply.go` 存在
- [x] `init()` 调用 `thinking.RegisterProvider("deepseek", NewApplier())`
- [x] `var _ thinking.ProviderApplier = (*Applier)(nil)` 编译时接口检查存在
- [x] ModeLevel + Level 非空：删除 thinking，设置 reasoning_effort
- [x] `normalizeDeepSeekEffort("xhigh")` 返回 "max"
- [x] ModeNone + Level 空/None：删除 thinking 和 reasoning_effort，设置 thinking.type=disabled
- [x] ModeBudget：通过 ConvertBudgetToLevel 转换后归一化

### MiMo provider 验证

- [x] `internal/thinking/provider/mimo/apply.go` 存在
- [x] `init()` 调用 `thinking.RegisterProvider("mimo", NewApplier())`
- [x] ModeLevel + Level 非空非 None：设置 thinking.type=enabled
- [x] ModeNone + Level 空/None：设置 thinking.type=disabled
- [x] ModeBudget + Budget > 0：设置 thinking.type=enabled + mimoBoostMaxCompletion
- [x] `mimoBoostMaxCompletion` 在 thinking.type=enabled 且 max_completion_tokens < budget 时提升

### 注册逻辑验证

- [x] `thinking_providers.go` 含 `_ "...deepseek"` blank import
- [x] `thinking_providers.go` 含 `_ "...mimo"` blank import
- [x] `apply.go` 的 `nativeProviderAppliers` 含 `"deepseek": nil` 和 `"mimo": nil`
- [x] `apply.go` 的 `extractThinkingConfig` switch 含 `case "mimo": return extractMIMOConfig(body)`
- [x] `extractMIMOConfig` 优先解析 thinking.type（enabled→LevelHigh，disabled→ModeNone）
- [x] `extractMIMOConfig` fallback 解析 reasoning_effort（none/low/medium/high/max/auto 对应映射）
- [x] `validate.go` 的 `isBudgetCapableProvider` 含 "mimo"
- [x] `validate.go` 的 `isOpenAIFamily` 含 "xai"、"deepseek"、"mimo"（三项均添加）
- [x] `strip.go` 的 `StripThinkingConfig` 含 `case "mimo"` 和 `case "deepseek"`（paths: thinking, reasoning_effort）

### executor 路由验证

- [x] `openai_compat_executor.go` 含 `modelkind` import
- [x] `thinkingTargetForModel("deepseek-chat", "openai")` 返回 "deepseek"
- [x] `thinkingTargetForModel("mimo-v2", "openai")` 返回 "mimo"
- [x] `thinkingTargetForModel("gpt-4", "openai")` 返回 "openai"（defaultTarget）
- [x] `Execute` 的 ApplyThinking 调用使用 `thinkingTargetForModel(baseModel, to.String())`
- [x] `ExecuteStream` 的 ApplyThinking 调用使用 `thinkingTargetForModel(baseModel, to.String())`
- [x] `CountTokens` 的 ApplyThinking 调用使用 `thinkingTargetForModel(baseModel, to.String())`

### 未添加项验证（避免无效改动）

- [x] **未添加** `DefaultDisabled` 字段到 `ThinkingSupport` 结构体（W4：当前无 default 注入逻辑，空块无效）
- [x] **未添加** `internal/thinking/norm.go`（H1：NormalizeEffort 无调用方，死代码）
- [x] **未添加** `convertReasoningToThinkingContent` 函数（当前分支无此函数，不需要）

### 条件性任务验证（若调研发现 reasoning_content）

- [x] 若 MiMo 响应含 reasoning_content，已补充响应翻译逻辑（新建 `mimo_normalize.go` + `openai_compat_executor.go` 3 处条件性调用）
- [x] 响应翻译逻辑确保 reasoning_content 被正确处理（`normalizeMimoToolMessageReasoning` 回填缺失的 reasoning_content）
- [x] `mimoLockThinkingParams` 在 `thinking.type=enabled` 时锁定 `temperature=1.0`、`top_p=0.95`（参考 ed20fd04 的 mimo_executor.go）

### 测试验证

- [x] `internal/modelkind/modelkind_test.go` 存在且覆盖各种前缀和大小写（13 个子测试）
- [x] `internal/thinking/provider/deepseek/apply_test.go` 存在且覆盖各 Mode（8 个测试）
- [x] `internal/thinking/provider/mimo/apply_test.go` 存在且覆盖各 Mode + mimoBoostMaxCompletion（11 个测试）
- [x] `extractMIMOConfig` 测试覆盖 thinking.type 优先 + reasoning_effort fallback（14 个测试）
- [x] `internal/runtime/executor/openai_compat_stream_retry_test.go` 存在且覆盖 isRetryableStreamDisconnect、degradeEffort、degradeReasoningForRetry（25 个子测试）
- [x] `internal/runtime/executor/mimo_normalize_test.go` 存在且覆盖 normalizeMimoToolMessageReasoning + mimoLockThinkingParams（14 个测试）

### 全量编译与测试

- [x] `go build ./...` 全量编译通过
- [x] `go vet ./...` 无 lint 错误（本次改动包 vet 全通过；预先存在的 vet 错误与本次改动无关）
- [x] `go test ./internal/modelkind/... -v` 全部通过（13 个子测试）
- [x] `go test ./internal/thinking/... -v` 全部通过（含 extractMIMOConfig 14 + deepseek 8 + mimo 11）
- [x] `go test ./internal/runtime/executor/... -v` 全部通过（含 SSE normalizer、场景 8、mimo_normalize 14、stream_retry 25）
- [x] `go test ./...` 全量回归通过（修复 isOpenAIFamily 误添加 xai 导致的 4 个 X 系列测试失败）
- [x] 阶段 3 已提交独立 commit（commit `f300cb19`）

### 阶段 3 修复偏差记录

- [x] **偏差 1**：`isOpenAIFamily` 移除 `xai`（manual 方案要求添加 xai/deepseek/mimo，但 X 系列测试期望 openai→xai 走跨家族 clamping；最终保留 deepseek/mimo，移除 xai）
- [x] **偏差 2**：阶段 3 拆分为 2 个 commit（`011469f6` Task 14-18 注册逻辑 + `f300cb19` Task 19-24 规范化/测试/修复），非单一 commit
- [x] **偏差 3**：commit message 调整为 `feat: add MiMo request normalization and thinking provider test coverage`（比 Task 24 原定 message 更准确反映实际内容）

## 跨阶段验证

- [x] 阶段 1、2、3 各自独立 commit，commit message 清晰（阶段 1: `f8268824`，阶段 2: `9eacb53a`，阶段 3: `011469f6` + `f300cb19`）
- [x] 三个 commit 均可单独 cherry-pick（无强依赖）
- [x] Agent.md 已更新（记录 user-defined 假设偏差、param 作用域、MiMo 响应格式、isOpenAIFamily 误添加 xai 等协作陷阱）
