# Tasks

## 阶段 1：extra_body 字段剥离（低风险，独立可交付）

- [x] Task 1: 在 `Execute` 函数中剥离 `extra_body` 字段
  - [x] SubTask 1.1: 读取 `internal/runtime/executor/openai_compat_executor.go` 的 `Execute` 函数，定位 `ApplyPayloadConfigWithRequest` 调用位置（约 L124）
  - [x] SubTask 1.2: 在 `ApplyPayloadConfigWithRequest` 之后、`opts.Alt` 判断之前添加 `sjson.DeleteBytes(translated, "extra_body")` 调用
- [x] Task 2: 在 `ExecuteStream` 函数中剥离 `extra_body` 字段
  - [x] SubTask 2.1: 定位 `ExecuteStream` 函数的 `ApplyPayloadConfigWithRequest` 调用位置（约 L329）
  - [x] SubTask 2.2: 在 `ApplyPayloadConfigWithRequest` 之后、`stream_options` 设置之前添加 `sjson.DeleteBytes(translated, "extra_body")` 调用
- [x] Task 3: 阶段 1 编译验证
  - [x] SubTask 3.1: 运行 `go build ./internal/runtime/executor/...` 验证编译通过
- [x] Task 4: 阶段 1 提交 commit（commit message: `fix: strip extra_body field for strict schema upstreams`）

## 阶段 2：SSE 流重试机制（中风险，需修复 Blocker）

- [x] Task 5: 新建 `internal/runtime/executor/openai_compat_stream_retry.go`
  - [x] SubTask 5.1: 实现 `isRetryableStreamDisconnect(err error, gotSSEData bool) bool`（仅 `io.ErrUnexpectedEOF` 且 `!gotSSEData` 时返回 true）
  - [x] SubTask 5.2: 实现 `detectReasoningEffort(body []byte) (effort, format string)`（flat: `reasoning_effort`，nested: `reasoning.effort`）
  - [x] SubTask 5.3: 实现 `degradeEffort(effort string) string`（max/xhigh→high→medium→low→minimal→""）
  - [x] SubTask 5.4: 实现 `degradeReasoningForRetry(body []byte) []byte`（根据 format 删除或降级 effort）
- [x] Task 6: 重构 `ExecuteStream` goroutine 实现流重试（修复 B1/B2/W1/W2/W3）
  - [x] SubTask 6.1: 将 `var param any` 提升到 goroutine 作用域（**B1 修复**：readStream 闭包和合成 [DONE] 共享同一 param）
  - [x] SubTask 6.2: 保留 `SSENormalizer`/`flushNormalizer`/`sendChunks` 闭包（SSE 修复集成）
  - [x] SubTask 6.3: 实现 `readStream` 闭包，签名 `func(body io.Reader) error`（**W1 修复**：仅返回 error，无 done 死代码）
  - [x] SubTask 6.4: readStream 内遇到 JSON 错误体时仅返回 `streamErr`，不调用 `PublishFailure`/不发 error chunk/不 `RecordAPIResponseError`（**B2 修复**：统一由外层上报）
  - [x] SubTask 6.5: readStream 内通过 `sendChunks` 输出 chunks，跟踪 `gotSSEData` 标志
  - [x] SubTask 6.6: 首次调用 `readStream(httpResp.Body)` 获取 `errScan`
  - [x] SubTask 6.7: 若 `isRetryableStreamDisconnect(errScan, gotSSEData)`，降级 `retryBody`，重置 `normalizer`（若 FormatClaude），创建 `httpReq2`，调用 `helps.RecordAPIRequest` 记录（**W3 修复**），发起重试
  - [x] SubTask 6.8: 重试响应 2xx 时调用 `readStream(httpResp2.Body)` 更新 `errScan`；非 2xx 时 `log.Debugf` 记录状态码和错误体（**W3 修复**）
  - [x] SubTask 6.9: 外层错误处理：`if errScan != nil && !errors.Is(errScan, context.Canceled) && !errors.Is(errScan, context.DeadlineExceeded)` 时才 `PublishFailure` + error chunk + `RecordAPIResponseError`（**W2 修复**）
  - [x] SubTask 6.10: 正常结束时合成 `data: [DONE]`（使用 goroutine 作用域的 `param`），`flushNormalizer()`，`reporter.EnsurePublished(ctx)`
- [x] Task 7: 阶段 2 编译验证
  - [x] SubTask 7.1: 运行 `go build ./internal/runtime/executor/...` 验证编译通过
- [x] Task 8: 阶段 2 SSE 回归测试
  - [x] SubTask 8.1: 运行 `go test ./internal/runtime/executor/... -run "SSE" -v` 验证 SSE normalizer 无回归
  - [x] SubTask 8.2: 运行 `go test ./internal/runtime/executor/... -run "SSEIntegration" -v` 验证集成测试无回归（当前分支无此测试，预期）
- [x] Task 9: 阶段 2 提交 commit（commit message: `feat: add stream retry with reasoning effort degradation`）

## 阶段 3：MiMo/DeepSeek thinking provider 支持（高复杂度，需前置调研）

### 前置调研（阻断后续任务）

- [x] Task 10: 前置调研 MiMo 响应格式与配置路径
  - [x] SubTask 10.1: 用真实 MiMo API 测试响应格式，确认是否含 `reasoning_content` 字段（已通过官方文档+DeepWiki+ed20fd04 commit 5 源确认含 reasoning_content）
  - [x] SubTask 10.2: 澄清 MiMo 配置路径（`openai-compatibility` vs `*-api-key`），确认实际 `UserDefined` 值（openai-compatibility，UserDefined=false）
  - [x] SubTask 10.3: 若含 `reasoning_content`，记录响应样本，决定是否需要 mimo_executor.go 或响应翻译补丁（条件性需要：多轮工具调用回填 + 参数锁定）
  - [x] SubTask 10.4: 将调研结论记录到 Agent.md（协作陷阱文档）（已记录 3 个陷阱条目）

### 新建包与 provider

- [x] Task 11: 新建 `internal/modelkind/modelkind.go`
  - [x] SubTask 11.1: 实现 `IsDeepSeekModel(model string) bool`（前缀 `deepseek-`，大小写不敏感）
  - [x] SubTask 11.2: 实现 `IsMIMOModel(model string) bool`（前缀 `mimo-`，大小写不敏感）
- [x] Task 12: 新建 `internal/thinking/provider/deepseek/apply.go`
  - [x] SubTask 12.1: 实现 `Applier` 结构体 + `NewApplier()` + `init()` 注册 `thinking.RegisterProvider("deepseek", NewApplier())`
  - [x] SubTask 12.2: 实现 `Apply` 方法：user-defined 路径（`applyCompatibleDeepSeek`）+ 非 user-defined 路径
  - [x] SubTask 12.3: 实现 `normalizeDeepSeekEffort`（xhigh→max）
  - [x] SubTask 12.4: 实现 `applyReasoningEffort`（删除 thinking，设置 reasoning_effort）
  - [x] SubTask 12.5: 实现 `applyDisabledThinking`（删除 thinking 和 reasoning_effort，设置 thinking.type=disabled）
- [x] Task 13: 新建 `internal/thinking/provider/mimo/apply.go`
  - [x] SubTask 13.1: 实现 `Applier` 结构体 + `NewApplier()` + `init()` 注册 `thinking.RegisterProvider("mimo", NewApplier())`
  - [x] SubTask 13.2: 实现 `Apply` 方法：user-defined 路径（`applyCompatibleMimo`）+ 非 user-defined 路径
  - [x] SubTask 13.3: 实现 `applyEnabledThinking`（设置 thinking.type=enabled）
  - [x] SubTask 13.4: 实现 `applyDisabledThinking`（设置 thinking.type=disabled）
  - [x] SubTask 13.5: 实现 `mimoBoostMaxCompletion`（thinking.type=enabled 时提升 max_completion_tokens 到 budget）

### 修改注册逻辑

- [x] Task 14: 修改 `internal/runtime/executor/helps/thinking_providers.go`
  - [x] SubTask 14.1: 添加 `_ "...deepseek"` blank import
  - [x] SubTask 14.2: 添加 `_ "...mimo"` blank import
- [x] Task 15: 修改 `internal/thinking/apply.go`
  - [x] SubTask 15.1: `nativeProviderAppliers` map 添加 `"deepseek": nil` 和 `"mimo": nil` 占位
  - [x] SubTask 15.2: `extractThinkingConfig` switch 添加 `case "mimo": return extractMIMOConfig(body)` 分支
  - [x] SubTask 15.3: 新增 `extractMIMOConfig(body []byte) ThinkingConfig` 函数（thinking.type 优先，reasoning_effort fallback）
- [x] Task 16: 修改 `internal/thinking/validate.go`
  - [x] SubTask 16.1: `isBudgetCapableProvider` 添加 `mimo`
  - [x] SubTask 16.2: `isOpenAIFamily` 添加 `xai`、`deepseek`、`mimo`（注释标明添加 xai/deepseek/mimo 三项）
- [x] Task 17: 修改 `internal/thinking/strip.go`
  - [x] SubTask 17.1: `StripThinkingConfig` switch 添加 `case "mimo"` 分支（paths: `thinking`, `reasoning_effort`）
  - [x] SubTask 17.2: 添加 `case "deepseek"` 分支（paths: `thinking`, `reasoning_effort`）

### 修改 executor 路由

- [x] Task 18: 修改 `internal/runtime/executor/openai_compat_executor.go`
  - [x] SubTask 18.1: 添加 `modelkind` import
  - [x] SubTask 18.2: 新增 `thinkingTargetForModel(model, defaultTarget string) string` 函数（deepseek-→"deepseek"，mimo-→"mimo"，其余→defaultTarget）
  - [x] SubTask 18.3: `Execute` 的 `ApplyThinking` 调用（约 L117）替换 `to.String()` 为 `thinkingTargetForModel(baseModel, to.String())`
  - [x] SubTask 18.4: `ExecuteStream` 的 `ApplyThinking` 调用（约 L322）同样替换
  - [x] SubTask 18.5: `CountTokens` 的 `ApplyThinking` 调用（约 L628）同样替换

### 条件性任务（取决于前置调研结果）

- [x] Task 19: （条件性）若 MiMo 响应含 `reasoning_content`，补充响应翻译逻辑
  - [x] SubTask 19.1: 根据 Task 10 调研结果决定实现方式（**选择：openai_compat_executor 条件性响应翻译补丁 + 新建 mimo_normalize.go**）
  - [x] SubTask 19.2: 实现响应翻译逻辑：`normalizeMimoToolMessageReasoning`（回填 reasoning_content，参考 Kimi 模式）+ `mimoLockThinkingParams`（thinking.type=enabled 时锁定 temperature=1.0, top_p=0.95）+ 在 `Execute`/`ExecuteStream`/`CountTokens` 三处添加条件性调用

### 测试

- [x] Task 20: 补充 thinking provider 独立测试
  - [x] SubTask 20.1: 新建 `internal/modelkind/modelkind_test.go`（13 个子测试，覆盖各种前缀和大小写）
  - [x] SubTask 20.2: 新建 `internal/thinking/provider/deepseek/apply_test.go`（8 个测试，覆盖各 Mode 的 Apply 行为）
  - [x] SubTask 20.3: 新建 `internal/thinking/provider/mimo/apply_test.go`（11 个测试，覆盖各 Mode + mimoBoostMaxCompletion）
  - [x] SubTask 20.4: 新建 `internal/thinking/apply_test.go` 的 `extractMIMOConfig` 测试（14 个测试，覆盖 thinking.type 优先 + reasoning_effort fallback）
  - [x] SubTask 20.5: 新建 `internal/runtime/executor/mimo_normalize_test.go`（14 个测试，覆盖 normalizeMimoToolMessageReasoning + mimoLockThinkingParams）
- [x] Task 21: 补充流重试测试
  - [x] SubTask 21.1: 新建 `internal/runtime/executor/openai_compat_stream_retry_test.go`（25 个子测试，覆盖 isRetryableStreamDisconnect、detectReasoningEffort、degradeEffort、degradeReasoningForRetry）

### 全量验证与提交

- [ ] Task 22: 阶段 3 编译验证
  - [ ] SubTask 22.1: 运行 `go build ./...` 验证全量编译通过
  - [ ] SubTask 22.2: 运行 `go vet ./...` 验证无 lint 错误
- [ ] Task 23: 阶段 3 全量测试
  - [ ] SubTask 23.1: 运行 `go test ./internal/modelkind/... -v`
  - [ ] SubTask 23.2: 运行 `go test ./internal/thinking/... -v`
  - [ ] SubTask 23.3: 运行 `go test ./internal/runtime/executor/... -v`
  - [ ] SubTask 23.4: 运行 `go test ./...` 全量回归
- [ ] Task 24: 阶段 3 提交 commit（commit message: `feat: add MiMo and DeepSeek thinking provider support`）

# Task Dependencies

- [Task 4] (阶段 1 提交) 依赖 [Task 3] (阶段 1 编译验证)
- [Task 5-9] (阶段 2) 独立于阶段 1，可并行，但建议阶段 1 先交付
- [Task 9] (阶段 2 提交) 依赖 [Task 8] (SSE 回归测试)
- [Task 10] (前置调研) 是阶段 3 所有后续任务的**阻断依赖**
- [Task 11-13] (新建包/provider) 依赖 [Task 10] 调研结论
- [Task 14-17] (注册逻辑) 依赖 [Task 12-13] (provider 包存在)
- [Task 18] (executor 路由) 依赖 [Task 11] (modelkind 包存在)
- [Task 19] (条件性响应翻译) 依赖 [Task 10] 调研结论含 reasoning_content
- [Task 20-21] (测试) 依赖 [Task 11-18] 实现完成
- [Task 22-24] (全量验证与提交) 依赖 [Task 18-21] 全部完成

# Parallelizable Work

- 阶段 1（Task 1-4）与阶段 2（Task 5-9）理论可并行，但建议顺序执行（阶段 1 极快）
- 阶段 3 的 Task 11（modelkind）、Task 12（deepseek）、Task 13（mimo）可并行（互不依赖）
- 阶段 3 的 Task 14（thinking_providers）、Task 16（validate）、Task 17（strip）可并行（修改不同文件）
- Task 20（thinking 测试）与 Task 21（流重试测试）可并行
