# Agent Collaboration Notes

This document records unexpected situations and pitfalls encountered during
development, to help subsequent AI agents avoid stepping into the same traps.
Each entry follows the format: `### [Category] Problem Description`, followed
by detailed description, impact scope, and suggested solutions.

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

**解决方案**

需要在请求体规范化中添加 MiMo 的 `reasoning_content` 回传逻辑（类似 Kimi 的
`normalizeKimiToolMessageLinks`）。建议二选一：
1. 新建 `mimo_executor.go` 嵌入 `OpenAICompatExecutor`，在 Execute/ExecuteStream
   前(mi)调用类似的 `normalizeMimoToolMessageReasoning` 函数
2. 在 `openai_compat_executor.go` 中添加条件性规范化（当
   `modelkind.IsMIMOModel(baseModel)` 时调用）

**记录位置**

- 调研依据：MiMo 官方文档、`internal/runtime/executor/kimi_executor.go` L332-455

---

### [MiMo 集成] 深度思考模式下 temperature/top_p 被强制锁定

**问题描述**

MiMo 官方文档（mimo.mi.com/docs）明确：在思考模式下，`mimo-v2.5-pro`、
`mimo-v2.5` 等模型**不支持**自定义 `temperature` 和 `top_p` 参数，即使传入也会被
强制采用推荐默认值 `1.0` 和 `0.95`。若客户端传入非默认值，可能导致请求被拒绝或
行为异常。

**影响范围**

- 当前分支无 `mimo_executor.go`，因此缺少此锁定逻辑
- 风险：当用户通过 config 或 client 传入自定义 temperature/top_p 时，MiMo 深度
  思考模式可能不稳定

**解决方案**

若验证发现此问题导致 400 错误，需要：
1. 新建 `mimo_executor.go`（嵌入 OpenAICompatExecutor）实现 `mimoLockThinkingParams`
2. 或在 `openai_compat_executor.go` 的 `ApplyPayloadConfigWithRequest` 之后添加
   条件性锁定（当 `modelkind.IsMIMOModel(baseModel)` 且 `thinking.type="enabled"`）

**记录位置**

- 调研依据：MiMo 官方文档

---

### [DeepSeek 集成] 工具调用后续轮次必须完整回传 reasoning_content

**问题描述**

DeepSeek 官方文档
（https://api-docs.deepseek.com/zh-cn/guides/thinking_mode/）明确要求：在思考模式下，
若 assistant 消息包含 `tool_calls`，则后续请求中**必须**完整回传该 assistant
消息的 `reasoning_content`。若未正确回传，API 会返回 400 错误。

与 MiMo/Kimi 不同的是，DeepSeek 文档未要求锁定 `temperature`/`top_p` 等采样参数；
文档说明这些参数在 `deepseek-reasoner` 下即使传入也只是“不生效”，不会因此成为
必须修正的兼容性问题。

**影响范围**

- 当前 `openai_compat_executor.go` 仅对 MiMo 做了 `reasoning_content` 规范化，未对
  DeepSeek 做同类处理
- 影响 DeepSeek 思考模式下的多轮工具调用场景，尤其是 Agent 类客户端未完整回传
  `reasoning_content` 时会触发 400

**解决方案**

在请求体规范化中添加 DeepSeek 的 `reasoning_content` 回传逻辑，建议：
1. 新建 `deepseek_normalize.go`，实现 `normalizeDeepSeekToolMessageReasoning`
2. 在 `openai_compat_executor.go` 的 `Execute`、`ExecuteStream`、`CountTokens`
   中，在 `ApplyPayloadConfigWithRequest` 之后、请求发出之前条件调用
3. 回填优先级建议与 MiMo 保持一致：当前消息 `reasoning` → 当前消息 `content` →
   最近一条真实存在的 `reasoning_content` → `"[reasoning unavailable]"`

**记录位置**

- 调研依据：DeepSeek 官方文档

---

### [DeepSeek KV Cache] usage 命中字段需要标准化为 cached_tokens

**问题描述**

DeepSeek KV Cache 官方文档
（https://api-docs.deepseek.com/zh-cn/guides/kv_cache）说明命中信息通过
`usage.prompt_cache_hit_tokens` 和 `usage.prompt_cache_miss_tokens` 返回。
当前仓库的 OpenAI 风格 usage 解析与大多数跨协议翻译链路主要识别
`prompt_tokens_details.cached_tokens` / `input_tokens_details.cached_tokens`，
因此仅修请求侧多轮能力还不够，KV Cache 命中数据仍可能在内部统计或协议转换时丢失。

**影响范围**

- `internal/runtime/executor/helps/usage_helpers.go` 若不识别 DeepSeek 字段，usage
  统计拿不到命中 token
- OpenAI 兼容执行器若不在响应侧补标准字段，OpenAI → Responses/Claude/Interactions
  等转换链路无法稳定继承 cached token 信息
- 客户端即使显式传了 `X-Session-ID`、`conversation_id` 或类似会话标识，若代理未开启
  `routing.session-affinity`，这些标识当前也不会参与 auth 粘性路由，只会继续按默认
  调度策略选 auth

**解决方案**

建议分两层处理：
1. 在 usage 解析中支持 `prompt_cache_hit_tokens` 回退到 `CachedTokens`
2. 在 DeepSeek OpenAI 兼容响应进入翻译链前，补写标准
   `prompt_tokens_details.cached_tokens` / `input_tokens_details.cached_tokens`
   字段，同时保留原始 DeepSeek 字段
3. 若客户端已传会话标识但命中率仍低，优先检查并开启 `routing.session-affinity`
3. 当 DeepSeek 命中多 auth 路由且未开启 `routing.session-affinity` 时，建议在 auth
   选择阶段输出提示日志；第一阶段只提示，不引入服务端会话存储

**记录位置**

- 代码：`internal/runtime/executor/helps/usage_helpers.go`
- 响应标准化：`internal/runtime/executor/helps/`

---

### [Thinking 验证] isOpenAIFamily 不应包含 xai

**问题描述**

`internal/thinking/validate.go` 的 `isOpenAIFamily` 函数用于 `isSameProviderFamily(from, to)`
判断，控制 `allowClampUnsupported` 和 `strictBudget` 两个验证行为：
- 同家族（`isSameProviderFamily=true`）：`allowClampUnsupported=false`，不支持的 level
  直接报错；`strictBudget=true`，budget 严格范围验证
- 跨家族（`isSameProviderFamily=false`）：`allowClampUnsupported=true`，不支持的 level
  会被 `clampLevel` 降级到最近的支持值；`strictBudget=false`，budget 会被 clamp

若 xai 在 `isOpenAIFamily` 中，`openai→xai` 变成同家族，xhigh/max/minimal 等不支持的
level 直接报错而非 clamp，破坏跨家族 level clamping 行为。

**解决方案**

`isOpenAIFamily` **不应**包含 `xai`。当前实现为：
```go
case "openai", "openai-response", "codex", "deepseek", "mimo":
    return true
```

**注意：后续 AI agent 不要将 xai 添加回 `isOpenAIFamily`，否则会破坏跨家族 clamping。**

**记录位置**

- 代码：`internal/thinking/validate.go` 的 `isOpenAIFamily` 函数
- 测试：`test/thinking_conversion_test.go` 的 `TestThinkingE2ENewProviderTargets` X 系列测试
