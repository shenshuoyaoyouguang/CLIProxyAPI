# 第二批变更摘要 — 功能正确性（P2）

> 范围：审核报告 P2 严重/一般项中的功能正确性类。本批处理条目 **#1, #8, #9, #10, #16, #17**。
> 构建/测试/格式/ vet 全部通过。xai #8 经核实为**误报**，未改动（见下）。

---

## P2 #1 — Thinking JSON `null` 字段误触发思考模式

- **文件**：`internal/thinking/apply.go`
- **问题**：`gjson.Exists()` 对 JSON `null` 返回 `true`，导致 `null` 的 `reasoning_effort` / `budget_tokens` / `thinking` 字段被读成 `0` / `"none"` / `""`，静默套用了一个本不该有的 thinking 模式。
- **修复**：新增类型守卫 `fieldNonNil(r gjson.Result) bool`（`Exists() && Type != gjson.Null`），应用到 `extractClaudeConfig` / `extractGeminiConfig` / `extractOpenAIConfig` / `extractCodexConfig` 四个提取函数（DeepSeek 此前已单独处理）。
- **测试**：`internal/thinking/apply_null_guard_test.go`（含 `TestFieldNonNil` 及 openai/codex/claude/gemini 的 `*_NullReasoningEffort` / `*_NullBudgetTokens` / `*_NullThinkingLevel` / `*_NullThinkingBudget` 对称回归）。

## P2 #9 — Claude cloaking 在无 user message 时丢失系统指令

- **文件**：`internal/runtime/executor/claude_executor_cloaking.go`
- **问题**：请求没有 user message 时，用户 system 指令（作为 system 块搬运）会丢失——原逻辑只在首个 user message 前 prepend。
- **修复**：先收集 `userSystemParts` 并判断 `hasUserMessage`；当 `!strictMode && combined != "" && !hasUserMessage` 时，把指令作为 `text` 块**折叠进初始 system 数组的一次 `sjson.SetRawBytes`**（避免二次 raw 写入被后续 `injectFakeUserID` 的 `sjson.SetBytes` 重序列化丢弃）。`prependToFirstUserMessage` 现返回 `([]byte, bool)`。
- **测试**：`TestApplyCloaking_PreservesSystemInstructionsWhenNoUserMessage`（断言无 user message 时指令保留在 system 数组第 4 块）。

## P2 #10 — Antigravity 429 对非配额错误丢弃 `retryAfter`

- **文件**：`internal/runtime/executor/antigravity_executor_credits.go`
- **问题**：`error.status != RESOURCE_EXHAUSTED` 的 429（瞬时限流等）丢弃了服务端 `Retry-After`，被迫走软重试而非同鉴权即时重试。
- **修复**：非 `RESOURCE_EXHAUSTED` 的 429 若解析出 `retryAfter`，决策改为 `antigravity429DecisionInstantRetrySameAuth`；无 `retryAfter` 时仍回退软重试。
- **测试**：`TestDecideAntigravity429NonResourceExhaustedHonorsRetryAfter`（`retryAfter=12s` → `kind=instantRetrySameAuth`）、`TestDecideAntigravity429NonResourceExhaustedNoRetryAfterFallsBackToSoftRetry`。

## P2 #16 — Translator 包级可变全局变量（并发 + 每请求隔离）

- **文件**：`internal/translator/claude/openai/chat-completions/claude_openai_request.go`
- **问题**：包级 `user` / `account` / `session` 只懒初始化一次并被所有并发请求共享——既是数据竞争（无同步的懒初始化），也是隔离缺陷（每个请求都被打上同一个 `metadata.user_id`，合并了不同客户身份）。
- **修复**：删除这三个包级变量；每次调用用 `uuid.NewRandom()` 生成 `account` / `session` 并派生 `user`。
- **测试**：`TestConvertOpenAIRequestToClaude_DistinctUserIDPerCall`（200 并发调用，断言 `user_id` 全唯一；`-race` 可捕获原懒初始化的竞争）。

## P2 #17 — OpenAI→Claude SSE：`usage` 被 `FinishReason` 门控 + `[DONE]` 未兜底终止

- **文件**：`internal/translator/openai/claude/openai_claude_response.go`
- **问题**：(a) `usage` 的发射被 `FinishReason != ""` 门控，usage chunk 早于 `finish_reason` chunk 到达时 usage 被丢弃；(b) `[DONE]` 仅在 `FinishReason != ""` 时补发 `message_delta`，导致以 `[DONE]` 结束且从未出现 `finish_reason` 的流无法正常终止。
- **修复**：`param` 中累积 usage（跨 chunk）；当 `stop_reason` 与 usage 都已就绪时发射**唯一** `message_delta`，或在 `[DONE]` 时经 `emitMessageDeltaWithUsage` **无条件**补发（携带当前 `stop_reason` 与已累积 usage）。保持"恰好一个 `message_delta`"契约。
- **测试**：`TestStreaming_UsageBeforeFinishReason_EmitsBoth`（usage 先于 finish → 仍携带正确 `stop_reason=end_turn` 与 usage）、`TestStreaming_DoneWithoutFinishReason_Terminates`（无 finish_reason 仍以 `message_delta`+`message_stop` 终止）、既有 `TestStreaming_LateUsageOnlyDoesNotEmitAfterMessageStop`（仍恰好一个 `message_delta`）。

## P2 #8 — XAI reasoning replay "幽灵回合"（**误报，未改动**）

- **调查结论**：报告称需在 input 无 assistant 回合时拒绝缓存的 assistant 消息。但这是**合法多轮回放**——XAI 客户端每轮只发最新 user message，代理必须从缓存重建历史。加上该守卫会破坏 `TestXAIExecutorResponsesSSEReplaysEncryptedReasoningAndAssistantMessage`（第二轮只发 `{"role":"user","text":"second"}`，代理须注入 reasoning+assistant 回合）。
- **处置**：已回退临时守卫与相关测试，**不修改**。
- **给审阅者**：该报告条目是对既有回放语义的误读。

---

## 验证

| 步骤 | 命令 | 结果 |
|---|---|---|
| 全量构建 | `go build ./...` | ✅ |
| 服务端二进制编译（AGENTS.md 要求） | `go build -o cli-proxy-api ./cmd/server` | ✅ |
| 格式 | `gofmt -l` 于改动文件 | 干净 ✅ |
| 静态检查 | `go vet` 于受影响包 | 干净 ✅ |
| 单元测试 | `go test ./internal/thinking/... ./internal/runtime/executor/... ./internal/translator/openai/claude/... ./internal/translator/claude/openai/chat-completions/...` | 全部通过 ✅（新测试额外以 `-race` 运行） |

## 状态

- **第二批完成**：P2 #1, #8（误报，已记录）, #9, #10, #16, #17。
- **第三批待处理**：P2 #2–7, #11–15, #18–26 及全部 P3。
