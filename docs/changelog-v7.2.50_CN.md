# 本地提交总结（merge/upstream-v7.2.50）

本文档汇总当前分支 `merge/upstream-v7.2.50` 相对 `origin/main` 领先的 **121 个提交**，覆盖时间范围 **2026-07-04 ~ 2026-07-13**。内容按功能领域分组，逐项说明「做了什么」与「解决了什么问题」，便于代码审查、发布说明编写与后续排障。

> 提交类型分布：feat 52、fix 30、refactor 6、docs 5、chore 5、test 3、perf 3，另含若干 merge 提交。

---

## 合并准备补充（`refactor/clean-fork-v7.2.50`）

本小节记录 clean-fork / merge-prep 阶段的**有意行为变更**，便于发布说明与兼容性评估。路径硬化**不回滚**。

### Breaking

- `sdk/auth.FileTokenStore` 的 Save/Delete 与路径解析入口现经 `store.ResolveManagedPath` 解析。
- 越出 `auth-dir` 的 `..` 穿越与**外部**绝对路径一律拒绝；`auth-dir` 内的绝对路径仍允许。
- Auth 文件必须位于配置的 auth 目录之下；此前允许/容忍的外部绝对路径写法不再可用。
- 同步收紧 `internal/store` 各 backend（git/object/postgres）的 path 解析，与 `FileTokenStore` 一致。

### Behavior

- Claude 流式重试受 `streaming.stream-retry-enabled` 门控（默认 `false`），与 OpenAI-compatible executor 对齐。
- 流重试分类**仅**将 SSE `data:` 行视为「已收到数据」。
- heartbeat / comment / `event:` / `id:` / `retry:` 行不再抑制 pre-data 重试，避免空心跳误判为已开始出流。

---

## 一、上游同步

本分支包含两次与 `upstream/main` 的同步合并，是 121 个提交的基础层。

| 提交 | 说明 |
|---|---|
| `4709b9db` | 合并 upstream/main，同步 75 个上游提交，解决 2 处冲突 |
| `3e78857d` | 合并 upstream/main 到 `merge/upstream-v7.2.50` |

上游带来的内容包括赞助商信息更新（`6fc4f0c4`、`f35539c2`、`bc279c61`、`53ebde03`、`cc2095f3`）、README 项目条目（`759b30ee`）、Antigravity hub user agent 更新（`4f2e1904`）等文档与配置类改动。

---

## 二、认证与凭证调度（auth / oauth）

这是本次改动数量最多、稳定性收益最高的领域，集中在 `sdk/cliproxy/auth/conductor.go` 的失败处理与重试策略。

### 重试风暴与退避

- **`96ac97c9` feat(auth): 瞬态上游错误退避机制**
  新增 `transientUpstreamBackoff`（150ms）与 `isTransientUpstreamError`，用 `errors.Is` 精确识别 `context.DeadlineExceeded`、`net.OpError`（connection refused）等瞬态错误。在三个执行入口（`executeMixedOnce` / `executeCountMixedOnce` / `executeStreamMixedOnce`）统一插入退避，防止单个 flailing provider 触发全凭证重试风暴。**减少了对错误消息文本匹配的依赖**。

- **`270869dd` fix(auth): 配额退避每冷却窗口只升一级 + 冷却抖动**
  修复两个问题：(1) 此前每个 429 都无条件抬升退避等级，N 个并发失败会一次把等级抬升 N 级（12+ 次失败直接跳到 30 分钟平台期）；现在同一冷却窗口内的失败复用该窗口，退避阶梯每窗口最多前进一级。(2) `waitForCooldown` 原本睡到精确恢复时刻，导致所有等待请求同步唤醒、齐刷刷冲击唯一恢复的凭证再次升级；现在加入随机抖动（最多 wait/4，上限 2s）打散唤醒时刻。

- **`0d23f791` fix(auth): 抖动后的冷却等待保持在 max-retry-interval 内**
  为上一项的抖动加上上界约束，避免抖动把等待推到超过配置的最大重试间隔。

### 会话亲和性（session affinity）

- **`6b29d706` fix(auth): 防止瞬态失败导致会话亲和性抖动**
  `MarkResult` 原本在任何失败时都无条件调用 `invalidateSessionAffinity`，形成 miss→bind→fail→invalidate 的循环（单凭证遇到 502/超时等瞬态错误时尤甚）。现在**仅当凭证真正进入冷却或被禁用时才失效绑定**，瞬态失败保留绑定以便重试。同时将 session-affinity 缓存 key 中的 model 规范化（剥离 thinking 后缀），使 `grok-4.5` 与 `grok-4.5:thinking` 共享同一会话绑定，与 `RoundRobinSelector` 行为一致。

### 凭证刷新与错误恢复

- **`ec3aba23` feat(auth): 401 时自动刷新凭证**（Closes #4087）
  新增 `tryRefreshAfterUnauthorized`，请求过程中遇到 401 时刷新 OAuth 凭证；用 `refreshLocks` 防止同一 auth ID 并发刷新；刷新成功后重置 unauthorized 模型状态并恢复运行。

- **`3aa42a6f` fix(auth): `invalid_grant` 错误挂起重试**（Closes #4120）
  为 `invalid_grant` 增加 fallback 与 suspension 处理及检测辅助方法，避免失效授权反复重试。

- **`505c59d8` fix(auth): 团队计划凭证覆盖问题**（Closes #4075）
  `CredentialFileName` 对 team-scoped 计划加入裁剪后的 `hashAccountID`，新增 `isTeamScopedPlan`，确保同一邮箱下多个团队的文件名唯一，防止互相覆盖。

- **`ee71dc52` feat(auth/models): Claude 模型 ID 前缀处理 + 插件鉴权禁用态**
  新增 `EnsureClaudeModelIDPrefix` / `ResolveClaudeModelIDPrefix`，标准化并解码 `claude-fable-5-dd-<reversed>` 形式的模型 ID 用于路由与响应格式化；对插件虚拟 auth 及其展开子项强制 disabled 状态，并持久化该元数据。

### OAuth 会话与安全

- **`4396b4d7` fix(auth): OAuth state 硬化与会话完成可观测性**（Closes review 1-6）
  - OAuth state 改用加密随机数（xAI/Kimi）；`CompleteOAuthSession` 返回 bool，重复完成时打 warning。
  - `Register` 校验 state；oauth_callback 将 IPv6 `::1` 加入本地重定向白名单并校验 `redirect_url`。
  - 请求日志脱敏敏感头（Authorization、Cookie、X-API-Key 等）。
  - `openai_compat` 的 `buildRequest` 重构为返回 error，去掉 4 处裸字符串返回。
  - `xai_reasoning_replay` 的 caller hash 从 8 字节扩到 16 字节（命名空间隔离；持久缓存 key 失效，内存 TTL 1h）。
  - 构建脚本：新增 `build-optimized.ps1` 替换 docker-build 脚本；`GOTOOLCHAIN=local` 改为 go.mod 版本告警而非硬失败。

- **`7115e7e0` fix(oauth): 会话完成幂等化**
- **`d1ef06cb` fix(oauth): 遗留 getter 隐藏已完成会话**
- **`1af23344` fix(oauth): 拒绝未知的已完成状态**
- **`6e819ab6` feat(oauth): 重构为设备流 + 可取消会话，扩展测试覆盖**
- **`f081b91e` fix(plugin-store): 保留已安装来源标识**

---

## 三、xAI / Grok

### 加密推理回放（reasoning replay）

围绕 `store:false` 多轮场景下的推理内容保留与回放，是本领域的核心特性，配套多个健壮性修复。

- **`041816c2` feat(xai): Responses/Claude 加密推理回放 + 缓存硬化**
  保留 `include=reasoning.encrypted_content`；为 `store:false` 多轮缓存 reasoning + assistant 消息；在 Claude→Codex 对 grok 目标接受 Grok thinking 签名。缓存硬化：blob 级 reasoning 过滤、历史已有末条消息时绝不重复注入 assistant、按下游 API key 隔离会话 key、不可回放的完成轮清理缓存、规范化 refusal 部分。

- **`3f875ecd` fix(xai): 避免歧义的推理回放注入**
- **`f1e9347f` fix(xai): 保留纯 tool-call 的回放批次**
- **`487f8afc` fix(xai): 无调用方鉴权时禁用未隔离的回放**（安全：无 caller 身份则不回放，防串号）
- **`18d239d5` fix(xai): 压缩（compaction）后清理回放缓存**
- **`dc551b7d` refactor(xai): 移除未使用的 assistant content equal 辅助函数**

### 工具 schema 与稳定性

- **`0df267ad` fix(xai): 防止复杂 tool schema 导致 Desktop 挂起 + free-usage Retry-After**
  Codex Desktop 注入的 `codex_app.automation_update` 带庞大 oneOf+$ref schema，在 xAI free/build Responses 上会「接受 HTTP 但从不发 SSE」，客户端卡死到取消。修复：上游前把该类大 schema 简化为宽松 object schema；订阅免费额度耗尽的 429 映射为 24h RetryAfter 以便账号轮换/冷却。

- **`ca67caf0` fix(xai): 命名空间级 tool 参数处理 + 收窄 schema 简化范围**
  将 `namespaceName` 上下文传入 `normalizeXAITool`；schema 简化严格限定到 `codex_app.automation_update`（按精确命名空间+名称匹配），避免误伤无关工具。

### 请求头与识别

- **`1a0bbe09` fix(executor): 识别 Grok CLI OAuth 请求**
- **`6c70996e` feat(executor): 记录解析出的 xAI base URL + 增强测试**
- **`dc162b93` feat(executor): 重构 XAI 请求头应用逻辑 + 扩展测试**
- **`3533484a` feat(executor): 新增 chat-proxy 专用请求头 + 改进 base URL 处理**
- **`3fd18926` feat(executor): XAI 接入 model registry 支持 reasoning effort**
- **`0ba5fab5` merge #4240**（xai CLI user agent，#4233）

---

## 四、Codex / GPT-5.6

### 独立搜索代理（Alpha search）

- **`46e2894a` fix(codex): 代理 GPT-5.6 独立搜索**
  在 `internal/api/server.go` 新增独立 search 代理路径。
- **`9c3f7207` feat(codex): 为 alpha search 实现会话亲和 + 请求日志**（Closes #4166）
  新增 `SelectAuth` 方法用于灵活凭证选择；引入 per-session 会话亲和以复用凭证；启用 alpha search API 的请求/响应详细日志。
- **`55f4d6ed` feat(codex): 向 auth 选择传入 Gin context**
  新增 `codexSearchGinContextSelector` 捕获并校验 Gin context，用于凭证选择时携带查询参数，兼容 Home 调度。

### 模型注册与能力

- **`445de6c0` / `f21beb05` feat(models): 注册 GPT-5.6 Sol/Terra/Luna** 系列模型及增强能力/上下文。
- **`5f8899b7` / `35dba9b4` / `f084eefa`** chore(models)：Sol 配置调整、移除 registry 中的 Sol、从 models.json 移除 "ultra" effort。
- **`15f30371` feat(models): 限制 Codex 输入模态为 text 与 image**
- **`078ed178` feat(openai): Codex client 模型支持 input/output 模态**
- **`ed293344` fix: 向 Codex 客户端暴露 ultra reasoning effort**
- **`ef0a4a56` feat(middleware): 支持 Codex response websocket 日志**
- **`dc4be167` / `ea20742e` feat/fix(usage): 上报请求与响应的 service tier**；无 usage 时保留 response tier。
- **`abb52248` feat(translator): Codex 响应 usage 细分支持 `cache_write_tokens`**
- **`cdccc72d` fix(translator): 终止响应时解决 pending 的 codex tool calls**

---

## 五、Translator / 协议兼容

> 注意：`internal/translator/` 按项目约定不做独立改动，以下均为更大范围改动的一部分。

### 工具调用（tool call）

- **`07455ecb` fix(translator): 上游流式 tool_calls 缺 id 时合成 call_id** 以保证事件链完整。
- **`bd7cc647` feat(translator): 支持 Codex `additional_tools` 工具下发与 custom 工具历史回放**
- **`bd2aafb8` feat(translator): custom 工具调用以 `custom_tool_call` item 回放给客户端**
- **`e9d3dfbc` fix(translator): 工具输出与 custom input 解包健壮性改进**
- **`f4a8aee6` feat(translator): OpenAI responses 支持 namespace 与 custom 工具处理**
- **`dc39f445` fix(translator): 稳定 Responses Lite 工具事件**
- **`dc77bf4d` feat(translator): Claude 工具响应结构化内容解析**

### 参数与缓存映射

- **`d899c962` feat(translator): OpenAI `max_tokens` → Gemini `maxOutputTokens`**（Closes #4108）
  同时处理 `max_completion_tokens`，`max_tokens` 优先。
- **`bea95670` feat(translator): responses 与 messages 的 cache control 处理**（Closes #4146）
  新增 `AttachCacheControl` / `AttachMessageCacheControl`，在 content parts、messages、tools 间一致注入并保留 cache control 元数据。

### 响应逻辑与 Interactions 兼容

- **`f5dbce4b` / `14b13966` refactor(translator): 简化响应逻辑 + 增强 thinking 兼容处理**
- **`b9ccaa0f` fix(translator): 恢复上游 SSEEventData 单行输出以兼容 Interactions**

---

## 六、SSE 与流式协议合规

### Anthropic 协议合规

- **`6edd4f7b` feat(executor): SSE normalizer + 非流式内容排序器（Anthropic 协议合规）**
  新增 `SSENormalizer` 缓冲、重排、标准化 Claude SSE 事件；`NormalizeNonStreamContentOrder` 排序 content block 并修正 `stop_reason`。修复要点：去重复换行注入、event 前缀多空格/tab 解析、默认 `stop_reason` `stop`→`end_turn`、buffer `message_delta` 直到 `content_block_stop`、有 tool_use 块时修正为 `tool_use`、`message_delta` 去重、强制 `message_delta` 先于 `message_stop`。
- **`3cb3b149` test(helps): SSE 重复换行与协议合规修复的单测**

### keep-alive 与分帧修复

- **`dc064889` fix(claude): 转发前聚合 SSE 事件，避免 keep-alive 分帧**
  Claude→Claude passthrough 中原本每行 SSE 独立成 chunk，下游 keep-alive 心跳可能插到 `event:` 与 `data:` 之间导致「Unexpected end of JSON input」。现在按完整事件（空行结尾）聚合成单 chunk 发送，心跳只能落在事件之间；scanner 出错时跳过尾部残缓避免发送半截帧。
- **`d3f3aef1` fix(executor): claude/xai passthrough 累积 SSE 为原子 chunk**
- **`ab6ed392` test(executor): 验证 Claude executor SSE 事件完整透传**

### 性能

- **`26c4b565` perf(sse): 将 per-event 流水线折叠为 per-chunk 单缓冲**
- **`c4da5854` perf(sse): `handleMessageDelta` 复用 `gjson.ParseBytes`**
- **`bc812e5f` perf(usage): 避免解析无关的流 chunk**

---

## 七、Thinking / 推理管线

### 流式重试基础设施

- **`72c84950` feat: 流式重试基础设施 + SSE 变换层**
  新增流重试退避、分类与幂等支持（`helps/idempotency.go`）；新增 SSE events/integrity/transform/validation 组件；更新 claude 与 openai_compat executor 的重试逻辑；`sdk_config` 与 conductor 增加重试配置。
- **`9eacb53a` feat: 带 reasoning effort 降级的流重试**
  在收到任何 SSE 数据前遇到 unexpected EOF 时，降级 `reasoning_effort` 并重试一次。修复两个阻塞点：(B1) 将 var 参数提升到 goroutine 作用域，使 translator 状态跨重试与合成 `[DONE]` 保持；(B2) `readStream` 只返回错误不上报，由外层统一 `PublishFailure`。新增 `modelkind` 包与 deepseek/mimo thinking provider（此时尚未接入 registry）。
- **`aaf8d737` feat: 增强流重试的 Claude effort 降级 + gofmt 修复**

### MiMo / DeepSeek provider

- **`011469f6` feat: 将 MiMo 与 DeepSeek thinking provider 接入 registry**
  加入 blank import、`nativeProviderAppliers` 占位、`extractMIMOConfig`、strip/validate 中的 mimo/deepseek 分支，以及 executor 的 `thinkingTargetForModel` 路由；三处 `ApplyThinking` 调用将 `deepseek-`/`mimo-` 前缀模型路由到专属 provider。
- **`f300cb19` feat: MiMo 请求规范化 + thinking provider 测试覆盖**
  `normalizeMimoToolMessageReasoning` 为多轮 tool call 回填 `reasoning_content`（仿 Kimi 模式）；`mimoLockThinkingParams` 在 `thinking.type=enabled` 时锁定 `temperature=1.0`/`top_p=0.95`（MiMo 深度思考模式要求）；修复 `isOpenAIFamily` 移除 xai 以保留跨家族 level clamping。
- **`b6ff6119` feat(executor): DeepSeek 多轮 tool call 的 `reasoning_content` 规范化**
- **`abf009f4` feat(executor): DeepSeek KV Cache usage 归一化为标准 `cached_tokens` 字段**
- **`0d33ed33` feat(auth): 会话亲和关闭时记录 DeepSeek KV cache 亲和提示**

### 推理等级策略

- **`0e07d941` feat: auto thinking 映射到最高等级/最大预算**
  当 `DynamicAllowed=false` 强制 auto 为固定值时，改用模型支持的最高等级（level 模型）或最大预算（budget 模型），而非此前保守的中间值。使 "auto" 契合「自动推理即用满能力」的用户预期。
- **`c6121086` feat(models): 调整 thinking 等级，移除 "none" 与 "xhigh"**
- **`186c87ba` feat(models): confidence level 增加 `xhigh` 目标选项**
- **`1204101f` feat(models): registry 中 `zero_allowed` thinking config 改为 `false`**
- **`db4f1cef` feat(validation): 增强跨家族 level clamping，处理模型类型不匹配**
- **`f8268824` fix: 为严格 schema 上游剥离 `extra_body` 字段**
  OpenAI SDK 注入的私有 `extra_body` 会被 z.ai GLM 等严格 schema 上游以 400 拒绝；在 Execute/ExecuteStream 中于 `ApplyPayloadConfigWithRequest` 后剥离。

---

## 八、模型注册与配置（models / config）

- **`7c47edb1` feat(models): 注册 Grok 4.5**（扩展能力与上下文）
- **`26d45fd4` feat(models): 支持从配置覆盖模型请求头**
- **`3586d3e7` feat(config): 全 API 支持可配置的模型显示名**
- **`b4c59405` chore(models): 更新默认 client 版本与 user agent，修订 GPT-5.5 配置**
- **`49094932` feat(config): `LoadConfigOptional` 与 `ParseConfigBytes` 默认启用 WebsocketAuth**
- **`df080389` fix: example API key 安全模式下放行 management 访问**（#4107 safemode）

---

## 九、图像与执行器（executor / handlers）

- **`045a9642` feat(handlers): 图像专用模型扩展 `grok-imagine-image` / `grok-imagine-image-quality`**
- **`f9162d39` feat(executor): 图像生成 function tool 校验 + 测试**
- **`631f7a65` fix(executor): 增强 responses-lite 请求的图像生成 tool 处理**（#4192）
- **`042f1fea` feat(handlers): 图像执行结合 auth manager 更新方法名**
- **`2075f77c` feat(executor): 增强 `cache_tokens` 处理 + 别名规范化解析**
- **`e99a2056` feat(executor): 支持 `using_api` 属性切换 API 路径 + 测试**
- **`aa05fb27` feat(executor, handlers): 增强 websocket 输出恢复与压缩逻辑**
- **`dea47879` refactor(executor): 用 `StreamUsageBuffer` 集中 OpenAI 流 usage 处理**
- **`4f157fbd` fix(executor): 将 `message_too_big` WebSocket 错误映射为结构化 API 响应**

---

## 十、Interactions（新特性）

- **`8b9c4da2` feat(interactions): 支持 Google Interactions**
  新增 API handler 与 executor 逻辑，OpenAI/Claude Interactions 的请求响应转换，集成 Gemini API；改动涉及 config、management handlers、server 路由等多处。
- **`46c8c770` refactor(interactions): 提取 `InteractionsSSEBuilder`**，消除 6 个 provider 的 SSE 事件构造重复代码。
- **`5648dc6c` refactor(interactions): 统一翻译器命名风格 + 消除重复代码**

---

## 十一、基础设施：Store 统一、安全硬化、CI

**核心提交 `b9beae03`**（feat: unify store interface, harden security, and improve CI/test infrastructure）跨 store / api / CI / 依赖多个模块。以下为逐项核实后的准确说明。

### Store 一致性（非生产接口统一）

- 生产接口 `sdk/cliproxy/auth/store.go` 的 `Store` 仍为 **List/Save/Delete 三方法**，未变。
- 6 方法的「统一接口」（含 Bootstrap/PersistConfig/Close/ConfigPath/AuthDir）实际定义在**测试文件** `internal/store/store_conformance_test.go`，通过共享一致性测试套件 `ConformanceTests` 反向约束 git/object/postgres 三个 backend 行为一致。
- 三个 backend 各自补齐 `Bootstrap()` / `Close()`（及 ObjectStore 的 `Client()`）以满足测试契约。
- 新增 `testutil/store` 辅助（`TestAuth`、`TestConfig`、`WriteTestAuthFiles`、`RunConcurrent`）。

> 说明：提交信息称「Define common Store interface」，但生产侧并未引入多态；"统一"发生在测试契约层。

### 安全硬化

- **AuthMiddleware fail-closed**（`server.go:1851`）：manager 为 nil 时由 `c.Next()`（放行一切）改为返回 500，堵住配置缺失时的静默越权。**明确改进**。
- **CORS 改为反射请求**（`server.go:1748`）：`Access-Control-Allow-Origin` 回显请求 `Origin`，`Allow-Headers` 回显 `Access-Control-Request-Headers`。由于未设置 `Allow-Credentials` 且本服务鉴权走 `Authorization`/API key 而非 cookie，此改动安全性与原 `*` 等价，非实质硬化。
- **路径穿越防护**：三个 store 统一用 `strings.Contains(p, "..")` 拦截 `resolveAuthPath`/`resolveDeletePath`。有效但手法粗糙——合法文件名如 `my..config.json` 会误报，且未走 `filepath.Clean` + 前缀校验的规范做法。

> 提交信息称「Remove dead code: duplicate path traversal checks in IsAbs branches」，但 postgres 的 diff 实际是**新增**了 IsAbs 分支与 fallthrough 分支各一次的重复 `..` 检查，描述与代码相反。

### 行为变更（有静默副作用）

三个 store 均删除了 Save 中的 disabled-auth 提前返回：

```go
-  if auth.Disabled {
-      if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
-          return "", nil   // 原：禁用且文件不存在则跳过写入
-      }
-  }
```

**影响**：现在 disabled 状态的 auth 也会落盘。原逻辑为「禁用且从未持久化的凭证不写文件」。属语义变化，下游若依赖「disabled auth 不产生文件」的假设需另行验证。

其他行为变更：GitTokenStore 的 `List` ID 剥离 `.json` 后缀保持一致；`resolveDeletePath` 在缺后缀时回退尝试 `.json`；`PersistAuthFiles` 将相对路径基于 authDir 解析；`Bootstrap` 支持从远端拉取或从 example 模板复制配置。

### CI 分层（质量最高的部分）

`.github/workflows/pr-test-build.yml` 拆为两轨：

- **fast-test**：每个 PR + push main 触发，无 Docker，`go test -race -short ./...`，10 分钟超时。
- **deep-test**：仅 `push && ref==main`（或 schedule）触发，拉起 `postgres:16-alpine` + `minio` 服务容器，`go test -race -count=1 ./...`，30 分钟超时。
- 新增 `SKIP_TESTCONTAINERS` 环境变量：默认 `"1"` 跳过 testcontainers，deep-test 中置空以启用集成测试。

> 小瑕疵：文件末尾缺少 EOF 换行（`No newline at end of file`），不影响功能但不符 gofmt/EOF 惯例。

### 依赖与清理

- 新增 `testcontainers-go v0.43.0`（minio + postgres 模块）；`redis/go-redis` 提升为直接依赖；升级 `klauspost/compress`、`minio-go`、`golang.org/x/{crypto,net,sync,sys}` 等。
- 移除已完成的 `.trae/specs/add-extra-body-retry-mimo-support/` 文档；移除废弃的 `rand.Seed`（Go 1.20+ 自动播种）。

### 相关测试提交

- **`0478f7a1` test: 跨 executor/store/tui 新增 248 个测试用例**
- **`3ef74dce` merge #4109（websocket）**、**`22bb89a4` merge #4107（safemode）**

---

## 十二、文档与规范

- **`1ae905bc` docs: 记录 DeepSeek `reasoning_content` 与 KV cache 集成陷阱**
- **`a3799b9c` docs(spec): 标记 add-extra-body-retry-mimo-support 全部任务完成**
- 赞助商与 README 相关：`6fc4f0c4`、`f35539c2`、`bc279c61`、`53ebde03`、`cc2095f3`、`759b30ee`。

---

## 附：需要跟进验证的三处风险

按优先级排列，供后续跟进：

1. **【高】disabled-auth 落盘回归**（`b9beae03`）：三个 store 删除 disabled 提前返回后，禁用凭证会写文件，需确认 List 计数、UI 展示等下游是否受影响。
2. **【中】路径穿越防护改进**（`b9beae03`）：~~`strings.Contains(p, "..")` 应改为 `filepath.Clean` + authDir 前缀校验~~ → **已在 merge-prep 落地**（`store.ResolveManagedPath` + `FileTokenStore`，见上文「合并准备补充」）。
3. **【低】提交信息措辞修正**（`b9beae03`）：Store 接口统一、CORS 硬化、去除重复检查三处描述与代码不符，若用于审计或发布说明应先校正。

---

*生成时间：2026-07-14。对应分支 `merge/upstream-v7.2.50`，领先 `origin/main` 121 个提交。*
