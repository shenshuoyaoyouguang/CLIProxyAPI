# Checklist

## 阶段 1：extra_body 字段剥离

- [x] `Execute` 函数在 `ApplyPayloadConfigWithRequest` 之后调用 `sjson.DeleteBytes(translated, "extra_body")`
- [x] `ExecuteStream` 函数在 `ApplyPayloadConfigWithRequest` 之后调用 `sjson.DeleteBytes(translated, "extra_body")`
- [x] `sjson.DeleteBytes` 的错误处理使用 `if errDel == nil { translated = updated }` 模式（键不存在时安全跳过）
- [x] `go build ./internal/runtime/executor/...` 编译通过
- [ ] 阶段 1 已提交独立 commit

## 阶段 2：SSE 流重试机制

### Blocker 修复验证

- [ ] **B1 修复**：`var param any` 位于 goroutine 作用域（与 `normalizer` 同级），非 `readStream` 闭包内局部变量
- [ ] **B1 修复**：合成 `data: [DONE]` 时使用 goroutine 作用域的 `param`（非新建 `var param any`）
- [ ] **B1 修复**：`readStream` 闭包通过引用使用 goroutine 作用域的 `param`
- [ ] **B2 修复**：`readStream` 内遇到 JSON 错误体（`{`/`[` 开头）时仅返回 `streamErr`，不调用 `reporter.PublishFailure`、不发 error chunk、不调用 `helps.RecordAPIResponseError`
- [ ] **B2 修复**：外层 `if errScan != nil` 块统一调用 `reporter.PublishFailure` + error chunk + `RecordAPIResponseError`（各仅 1 次）

### Warning 修复验证

- [ ] **W1 修复**：`readStream` 闭包签名仅为 `func(body io.Reader) error`，无 `done` 返回值
- [ ] **W2 修复**：外层错误处理块包含 `!errors.Is(errScan, context.Canceled) && !errors.Is(errScan, context.DeadlineExceeded)` 判断
- [ ] **W3 修复**：重试块内调用 `helps.RecordAPIRequest` 记录重试请求
- [ ] **W3 修复**：重试响应非 2xx 时调用 `log.Debugf` 记录状态码和错误体

### SSE 修复保留验证

- [ ] `SSENormalizer` 初始化逻辑保留（`responseFormat == FormatClaude` 时创建）
- [ ] `flushNormalizer` 闭包保留（调用 `normalizer.Flush()` 输出 chunks）
- [ ] `sendChunks` 闭包保留（通过 `normalizer.Process` 处理 chunks）
- [ ] 重试时重置 `normalizer = helps.NewSSENormalizer()`（若 FormatClaude）
- [ ] 流结束时调用 `flushNormalizer()`
- [ ] 流结束时调用 `reporter.EnsurePublished(ctx)`

### 流重试逻辑验证

- [ ] `isRetryableStreamDisconnect` 仅在 `err == io.ErrUnexpectedEOF && !gotSSEData` 时返回 true
- [ ] `degradeEffort` 降级链正确：max/xhigh→high→medium→low→minimal→""
- [ ] `degradeReasoningForRetry` 支持 flat（`reasoning_effort`）和 nested（`reasoning.effort`）两种格式
- [ ] `degradeReasoningForRetry` 在 next=="" 时删除字段，否则设置字段
- [ ] 重试仅触发一次（无递归重试）
- [ ] `retryBody` 初始化为 `translated`，降级后更新
- [ ] `TranslateStream` 调用使用 `retryBody`（非 `translated`）
- [ ] `httpResp2.Body.Close()` 在重试块内被调用

### 编译与回归测试

- [ ] `go build ./internal/runtime/executor/...` 编译通过
- [ ] `go test ./internal/runtime/executor/... -run "SSE" -v` 全部通过
- [ ] `go test ./internal/runtime/executor/... -run "SSEIntegration" -v` 全部通过
- [ ] 阶段 2 已提交独立 commit

## 阶段 3：MiMo/DeepSeek thinking provider 支持

### 前置调研验证

- [x] Task 10 调研已完成，MiMo 响应格式（是否含 reasoning_content）已确认
- [x] MiMo 配置路径（openai-compatibility vs *-api-key）及 UserDefined 值已确认
- [x] 调研结论已记录到 Agent.md

### modelkind 包验证

- [ ] `internal/modelkind/modelkind.go` 存在
- [ ] `IsDeepSeekModel("deepseek-chat")` 返回 true
- [ ] `IsDeepSeekModel("DeepSeek-Chat")` 返回 true（大小写不敏感）
- [ ] `IsMIMOModel("mimo-v2")` 返回 true
- [ ] `IsMIMOModel("gpt-4")` 返回 false

### DeepSeek provider 验证

- [ ] `internal/thinking/provider/deepseek/apply.go` 存在
- [ ] `init()` 调用 `thinking.RegisterProvider("deepseek", NewApplier())`
- [ ] `var _ thinking.ProviderApplier = (*Applier)(nil)` 编译时接口检查存在
- [ ] ModeLevel + Level 非空：删除 thinking，设置 reasoning_effort
- [ ] `normalizeDeepSeekEffort("xhigh")` 返回 "max"
- [ ] ModeNone + Level 空/None：删除 thinking 和 reasoning_effort，设置 thinking.type=disabled
- [ ] ModeBudget：通过 ConvertBudgetToLevel 转换后归一化

### MiMo provider 验证

- [ ] `internal/thinking/provider/mimo/apply.go` 存在
- [ ] `init()` 调用 `thinking.RegisterProvider("mimo", NewApplier())`
- [ ] ModeLevel + Level 非空非 None：设置 thinking.type=enabled
- [ ] ModeNone + Level 空/None：设置 thinking.type=disabled
- [ ] ModeBudget + Budget > 0：设置 thinking.type=enabled + mimoBoostMaxCompletion
- [ ] `mimoBoostMaxCompletion` 在 thinking.type=enabled 且 max_completion_tokens < budget 时提升

### 注册逻辑验证

- [ ] `thinking_providers.go` 含 `_ "...deepseek"` blank import
- [ ] `thinking_providers.go` 含 `_ "...mimo"` blank import
- [ ] `apply.go` 的 `nativeProviderAppliers` 含 `"deepseek": nil` 和 `"mimo": nil`
- [ ] `apply.go` 的 `extractThinkingConfig` switch 含 `case "mimo": return extractMIMOConfig(body)`
- [ ] `extractMIMOConfig` 优先解析 thinking.type（enabled→LevelHigh，disabled→ModeNone）
- [ ] `extractMIMOConfig` fallback 解析 reasoning_effort（none/low/medium/high/max/auto 对应映射）
- [ ] `validate.go` 的 `isBudgetCapableProvider` 含 "mimo"
- [ ] `validate.go` 的 `isOpenAIFamily` 含 "xai"、"deepseek"、"mimo"（三项均添加）
- [ ] `strip.go` 的 `StripThinkingConfig` 含 `case "mimo"` 和 `case "deepseek"`（paths: thinking, reasoning_effort）

### executor 路由验证

- [ ] `openai_compat_executor.go` 含 `modelkind` import
- [ ] `thinkingTargetForModel("deepseek-chat", "openai")` 返回 "deepseek"
- [ ] `thinkingTargetForModel("mimo-v2", "openai")` 返回 "mimo"
- [ ] `thinkingTargetForModel("gpt-4", "openai")` 返回 "openai"（defaultTarget）
- [ ] `Execute` 的 ApplyThinking 调用使用 `thinkingTargetForModel(baseModel, to.String())`
- [ ] `ExecuteStream` 的 ApplyThinking 调用使用 `thinkingTargetForModel(baseModel, to.String())`
- [ ] `CountTokens` 的 ApplyThinking 调用使用 `thinkingTargetForModel(baseModel, to.String())`

### 未添加项验证（避免无效改动）

- [ ] **未添加** `DefaultDisabled` 字段到 `ThinkingSupport` 结构体（W4：当前无 default 注入逻辑，空块无效）
- [ ] **未添加** `internal/thinking/norm.go`（H1：NormalizeEffort 无调用方，死代码）
- [ ] **未添加** `convertReasoningToThinkingContent` 函数（当前分支无此函数，不需要）

### 条件性任务验证（若调研发现 reasoning_content）

- [ ] 若 MiMo 响应含 reasoning_content，已补充响应翻译逻辑或新建 mimo_executor.go
- [ ] 响应翻译逻辑确保 reasoning_content 被正确处理（不丢失思考内容）

### 测试验证

- [ ] `internal/modelkind/modelkind_test.go` 存在且覆盖各种前缀和大小写
- [ ] `internal/thinking/provider/deepseek/apply_test.go` 存在且覆盖各 Mode
- [ ] `internal/thinking/provider/mimo/apply_test.go` 存在且覆盖各 Mode + mimoBoostMaxCompletion
- [ ] `extractMIMOConfig` 测试覆盖 thinking.type 优先 + reasoning_effort fallback
- [ ] `internal/runtime/executor/openai_compat_stream_retry_test.go` 存在且覆盖 isRetryableStreamDisconnect、degradeEffort、degradeReasoningForRetry

### 全量编译与测试

- [ ] `go build ./...` 全量编译通过
- [ ] `go vet ./...` 无 lint 错误
- [ ] `go test ./internal/modelkind/... -v` 全部通过
- [ ] `go test ./internal/thinking/... -v` 全部通过
- [ ] `go test ./internal/runtime/executor/... -v` 全部通过
- [ ] `go test ./...` 全量回归通过
- [ ] 阶段 3 已提交独立 commit

## 跨阶段验证

- [ ] 阶段 1、2、3 各自独立 commit，commit message 清晰
- [ ] 三个 commit 均可单独 cherry-pick（无强依赖）
- [ ] Agent.md 已更新（记录 user-defined 假设偏差、param 作用域、MiMo 响应格式等协作陷阱）
