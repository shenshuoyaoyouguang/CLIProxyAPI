# 第三批变更摘要 — 边界与清理

> 范围：审核报告 **P2 #2–7、#11–15、#18–26**（20 项）+ **全部 P3**（25 项），合计 **45 项**。
> 结果：**34 项已落地**，**11 项因仓库权限阻断**（translator，已按 AGENTS.md 提 issue）。
> 另有 **3 项范围外发现**（见文末），其中 2 项已修复。

---

## 一、总览

| 子批次 | 包 | P2 | P3 | 状态 |
|---|---|---|---|---|
| A | `internal/thinking` | #2 #3 #4 #5 | 5 项 | ✅ 全部落地 |
| B | `internal/runtime/executor` | #6 #7 #11 | 8 项 | ✅ 全部落地 |
| C | `internal/api` | #12 #13 #14 #15 | 4 项 | ✅ 全部落地 |
| D | `internal/translator` | #18 #19 #20 #21 #26 | 6 项 | ⛔ 权限阻断 → issue #4720 |
| E | `internal/wsrelay` | #22 #23 | 1 项 | ✅ 全部落地 |
| F | `internal/registry` + `internal/signature` | #24 #25 | 1 项 | ✅ 全部落地 |

---

## 二、A · thinking 包（P2 #2–5 + P3 五项）

| 编号 | 问题 | 修改 | 文件 |
|---|---|---|---|
| P2 #2 | `normalizeClaudeBudget` 在约束不可满足时提前返回，把非法 `budget_tokens >= max_tokens` 原样发上游，必然 400 | 约束不可满足时改为**关闭思考**（`thinking.type=disabled`，删除 `budget_tokens`），保留调用方 `max_tokens` | `thinking/provider/claude/apply.go` |
| P2 #3 | `StripThinkingConfig` 缺 `case "zai"`，不支持思考的 GLM 模型会把 `reasoning_effort` 透传上游 | switch 补 zai 分支（zai applier 内嵌 openai applier，语义一致） | `thinking/strip.go` |
| P2 #4 | `RegisterProviderExtractor` 重复注册静默覆盖，冲突无感知 | 覆盖时告警，暴露重复注册 | `thinking/apply.go` |
| P2 #5 | nvidia `applyEnabledLevel` 的 default 分支不清 `reasoning_budget`，与 `medium_effort` 上游互斥，残留脏字段 | default 分支与 low 分支对齐，统一清除 | `thinking/provider/nvidia/apply.go` |
| P3 | `apply.go:534-538` 死代码 | 删除 | `thinking/apply.go` |
| P3 | `validate.go:147-150` 恒假条件 | 删除 | `thinking/validate.go` |
| P3 | `apply.go:500-502` 零值语义歧义 | 显式区分"未设置"与"设为 0" | `thinking/apply.go` |
| P3 | `multi_turn_passback.go:141,154` 日志级别过高 | 降级为 debug | `thinking/multi_turn_passback.go` |
| P3 | `convert.go:78-98` 往返转换不守恒且无说明 | 补注释标注不守恒边界 | `thinking/convert.go` |

**测试**：`provider/claude/apply_budget_conflict_test.go`（不可满足→disabled / 可满足→clamp 到 `max_tokens-1`）、`provider/nvidia/apply_level_test.go`（非 low 清 budget + low 分支回归）、`strip_zai_test.go`（zai 剥离 + openai 回归）。

**影响面**：Claude budget 模式请求由"必然 400"变为"降级无思考成功"；GLM/nemotron 请求不再携带脏字段。无 API 契约变更。

---

## 三、B · executor 包（P2 #6/#7/#11 + P3 八项）

| 编号 | 问题 | 修改 | 文件 |
|---|---|---|---|
| P2 #6 | Codex WS 终止失败路径**先解锁后 invalidate**，窗口内其他请求可拿到已失效连接 | 收敛为持锁内失效，消除竞态窗口 | `codex_websockets_execute.go`、`codex_executor_terminal.go` |
| P2 #7 | 非流式 `io.ReadAll` 出错被吞成 408（超时语义），掩盖真实网络错误 | 引入 request-scoped `codexStreamReadError`：读失败 → **502**；流读完但无终止事件 → **408** | `codex_executor_execute.go`、`codex_executor_stream.go` |
| P2 #11 | `utls_client` 建连无 ctx、无超时，可无限期挂起 | 仅对 **dial 阶段**设 `utlsConnectTimeout`（符合 AGENTS.md"仅凭据获取/建连允许超时"） | `helps/utls_client.go` |
| P3 | WS stream auth 缺 nil 守卫 | 补判空 | `codex_websockets_stream.go` |
| P3 | `ReplaceAll` 误用（应只替换首个） | 改精确替换 | `codex_executor_request.go` |
| P3 | reasoning 锚点回滚逻辑冗余 | 简化 | `codex_executor_reasoning.go` |
| P3 | antigravity 429 `Unknown` 分支死代码 | 删除 | `antigravity_executor_credits.go` |
| P3 | kimi `base_url` 被无条件覆盖 | 仅在未设置时写入 | `kimi_executor.go` |
| P3 | antigravity replay 占位分支无实现 | 补实现/删除占位 | `antigravity_reasoning_replay.go` |
| P3 | credits 5s 超时未在 AGENTS.md 记录为例外 | 补记入 AGENTS.md 超时例外清单 | `antigravity_executor_credits.go`、`AGENTS.md` |
| P3 | xai replay delete 前未读校验 | delete 前先读确认，避免误删他人条目 | `xai_reasoning_replay.go` |

**影响面**：Codex 非流式错误码由 408 改为 502（更准确，客户端重试策略可能随之改变，属预期修正）；utls 建连不再无限挂起。

---

## 四、C · api 包（P2 #12–15 + P3 四项）

| 编号 | 问题 | 修改 | 文件 |
|---|---|---|---|
| P2 #12 | `toggleConfigAPIKeyExcludedAll` 漏 ZAIKey，Z.ai key 无法被禁用 | 补 `"zai:apikey"` kind 分支 | `management/config_apikey_disable.go` |
| P2 #13 | 删除语义 trim 不对称：写入不 trim、删除 trim，带空格的 key 永远删不掉 | `deleteFromStringList` 改精确匹配；同时删除一处死 map | `management/config_lists.go` |
| P2 #14 | 管理端 `APICall` 可被用作 SSRF 跳板 | 三道防线：scheme 白名单 + `CheckRedirect` 拒绝内网跳转 + 响应体大小上限 | `management/api_tools.go` |
| P2 #15 | `deleteAuthFileByName` 路径逃逸 | 对**未受信任的 `name` 入参**推导路径做 `authDirContains` 校验；仍尊重 managed record 的 `path` 覆盖（设计上允许指向 AuthDir 外） | `management/auth_files_crud.go` |
| P3 | `withAuthIndex` TOCTOU | 索引获取与使用收敛进同一临界区 | `management/config_auth_index.go` |
| P3 | `normalizeOpenAICompatibilityEntry` 构造了未使用的 map | 删除 | `management/config_lists.go` |
| P3 | `server_reload` 统计漏 ZAIKey | `total` 与 printf 均补 `zaiAPIKeyCount` | `api/server_reload.go` |
| P3 | 请求日志明文记录 `Authorization` / `X-Management-Key` | 扩展 `util.MaskSensitiveHeaderValue` 识别 `management-key`，请求日志逐值掩码 | `api/middleware/request_logging.go`、`util/provider.go` |

**P2 #15 的一次返工**：首版把"路径逃逸防护"做成拒绝一切 AuthDir 外的文件，直接踩坏 `TestDeleteAuthFile_UsesAuthPathFromManager`（该测试把真实文件放在 externalDir 并要求删除）。正确边界是：**只校验不可信的 `name` 入参**，stored record 的 `path` 属性是可信来源，允许越界。修正后 4 个 `TestDeleteAuthFile_*` 全绿。

**影响面**：`util.MaskSensitiveHeaderValue` 的掩码扩展同时作用于 `request_logger_format.go` 与 `logging_helpers.go` 两处既有调用方（收益，非回归）。

---

## 五、D · translator 包（P2 #18–21/#26 + P3 六项）— ⛔ 权限阻断

**阻断原因**：AGENTS.md 规定改动 `internal/translator/` 需对 `router-for-me/CLIProxyAPI` 具备 WRITE/MAINTAIN/ADMIN 权限。实测 `gh repo view --json viewerPermission` 返回 **READ**，且本子批次是**纯 translator 改动**（不属于"作为其他改动的一部分"豁免），因此按规**停止代码改动**。

> 对照：第二批的 #16/#17 同样落在 translator，但那批是横跨 `thinking/` + `executor/` + `translator/` 的整体改动，命中 AGENTS.md 的"may modify it only as part of broader changes elsewhere"豁免，故已落地。本批次不满足该豁免。

**已交付替代物**：
- GitHub issue：**https://github.com/router-for-me/CLIProxyAPI/issues/4720**
- 本地副本：`TRANSLATOR-AUDIT-ISSUE.md`（含 11 项的 file/行号/bug 描述/intended code 草图）

| 编号 | 问题 | 目标位置 |
|---|---|---|
| P2 #18 | `custom_tool_call` 的 `input` 用 `SetBytes` 二次编码，应为 `SetRawBytes` | `openai/openai-responses/*_response.go`、`*_tools.go:32` |
| P2 #19 | 图片 `tool_result` 数组型 content 被降级为文本，图片丢失 | `interactions/claude/interactions_claude_request.go:216` |
| P2 #20 | 非流式只取 `content.0`，多 part 响应被截断 | `claude/openai-responses/*_response.go:195`、gemini 同 ~296 |
| P2 #21 | 流式块索引冲突，未先关闭未完成块 | `codex/claude/codex_claude_response.go:630`、`openai/claude/openai_claude_response.go:512` |
| P2 #26 | `fixCLIToolResponse` FIFO 匹配错配导致 tool result 被丢弃，应按 name 匹配 | `antigravity/gemini/antigravity_gemini_request.go:554` |
| P3 ×6 | `reasoning_summary_text.done` 硬编码换行 / `data:` 前缀严格匹配抽公共 / stream 参数注释 / `ToolCallAccumulator` 三处重复定义抽公共 / interactions 魔数常量化 / gemini 空 body 透传规范化 | 详见 issue |

**影响面**：无（未改动任何代码）。**依赖项**：需具 WRITE 权限的维护者合入。

---

## 六、E · wsrelay 包（P2 #22/#23 + P3 一项）

| 编号 | 问题 | 修改 | 文件 |
|---|---|---|---|
| P2 #22 | `cleanup()` 用 `s.pending = sync.Map{}` 整体替换，与并发读者竞态 | 改为 `Range` 内逐键 `Delete`，先 `forceDeliver` 错误消息再关闭 | `wsrelay/session.go` |
| P2 #23 | 每个请求起的 ctx 监控 goroutine 只在 ctx/session 结束时退出，请求正常完成后泄漏 | `pendingRequest` 新增 `done chan struct{}`，`close()` 时关闭，监控 goroutine `select` 增加 `case <-req.done` | `wsrelay/session.go` |
| P3 | `run(ctx)` 的 ctx 参数未使用 | 改为 `run()`，同步调用方 | `wsrelay/session.go`、`wsrelay/manager.go` |

**影响面**：长连接场景下 goroutine 数量不再随请求数单调增长。该包无单测，靠 `go build` + `go vet` + `-race` 全仓测试兜底。

---

## 七、F · registry + signature（P2 #24/#25 + P3 一项）

| 编号 | 问题 | 修改 | 文件 |
|---|---|---|---|
| P2 #24 | 远程 models.json 缺某 section 时，`tryRefreshModels` 无条件 `store.data = parsed`，静默清空该 provider 全部模型（`validateModelSection` 对空 section 只 warn 不报错） | 新增 `preserveMissingSections(oldData, newData)`：刷新目录中为空、而现役目录非空的 section，沿用现役定义并告警。在写入 store 前调用 | `registry/model_updater.go` |
| P2 #25 | 嵌入式 models.json 解析失败时 `init()` 只告警，`getModels()` 返回 nil，14 个 `Get*Models()` 与 `LookupStaticModelInfo` 直接解引用 → panic | `getModels()` 改为永不返回 nil：store 为空时返回 `emptyModelsCatalog` 并 `sync.Once` 告警。一处收口，覆盖全部调用方 | `registry/model_updater.go` |
| P3 | GPT 签名校验把 O(1) 的 `gAAAA` 前缀检查排在最长 32MB 的全串字符扫描之后 | 前缀检查前移至空值检查之后，扫描/解码之前 | `signature/gpt_validation.go` |

**关于 grok 的说明**：`grok_validation.go` 的顺序**未动**。它的 `gAAAA` 前缀检查已在字符扫描之前，且 Claude/Gemini 交叉探测必须早于本地 decode——`TestInspectGrokEncryptedContent_Rejects*` 系列显式断言错误信息为 "Claude"/"Gemini" fast-reject，重排会破坏该契约。现有顺序即为设计意图。

**影响面**：远程目录部分发布/截断不再导致 provider 模型整体消失；嵌入目录损坏时服务降级为空模型列表而非 panic。`preserveMissingSections` 在 `detectChangedProviders` 之前调用，因此被保留的 section 不会被误报为"变更"。

---

## 八、范围外发现

### 1. reasoning replay 缓存淘汰不确定（已修复）

`internal/cache` 的 `TestAntigravityReasoningReplayUnrelatedEvictionDoesNotBlockAbsentSnapshot` 在同一份代码上交替通过/失败（6 次运行中 2 次失败）。根因在**生产代码**：

- `evictOldestAntigravityReasoningReplayEntries` 与 `reasoningReplayStore.evictOldestLocked` 都用 `sort.Slice`（**不稳定排序**）且**只按 timestamp 比较**；
- Windows 时钟粒度粗，微秒内写入的两条记录拿到**完全相同**的 timestamp；
- 此时淘汰谁由 Go 的**随机 map 迭代顺序**决定 → 淘汰目标随机。

**修复**：两处均加确定性 tie-break（timestamp 相等时按 key 字典序）。修复后连续 6 轮整包测试全绿。
文件：`cache/antigravity_reasoning_replay_cache.go`、`cache/reasoning_replay_cache.go`。

### 2. codex 缓存过期测试契约过期（已修复）

第二批 P1"codex reasoning replay 缓存错误降级"把 `expireFailureFatal` 设为 `false`（读取已成功，滑动 TTL 刷新失败不应让请求失败），但 `codex_reasoning_replay_cache_test.go` 仍断言旧的 fatal 契约，导致 `TestCodexReasoningReplayRequiredHomeFailures/expire` 稳定失败。xai 侧已迁移（`TestXAIReasoningReplayRequiredHomeExpireFailureReturnsItems`），codex 侧遗漏。

**修复**：从 fatal 用例表中移除 `expire`，新增 `TestCodexReasoningReplayRequiredHomeExpireFailureReturnsItems`，与 xai 对齐。

### 3. `go vet ./...` 既有告警（未修复，超出本批范围）

- `internal/logging/request_logger_body_source.go:170` — `WriteTo` 签名不符 `io.WriterTo` 约定
- `internal/pluginhost/host_callbacks.go:164,178`、`host_model_stream_callbacks.go:29,48` — **cancel 未在所有路径调用，存在 context 泄漏**（与本次审核 P2 #23 同类问题，建议下一批处理）
- `internal/pluginhost/loader_windows.go` ×8 — `possible misuse of unsafe.Pointer`

以上均在本批次**未触及**的包中，为既有问题。

---

## 九、验证

| 步骤 | 命令 | 结果 |
|---|---|---|
| 全量构建 | `go build -buildvcs=false ./...` | ✅ |
| 服务端二进制 | `go build -buildvcs=false -o cli-proxy-api ./cmd/server` | ✅ |
| 格式 | `gofmt -l internal sdk cmd` | 干净 ✅（顺带修掉 `cache/codex_reasoning_replay_cache.go` 的对齐问题） |
| 静态检查 | `go vet` 于受影响包 | 干净 ✅（全仓剩余告警见"范围外发现 3"） |
| 单元测试 | `go test ./internal/... ./sdk/... ./test/... -count=1` | 全部通过 ✅ |
| 竞态检测 | `go test -race` 于 wsrelay/cache/registry/signature/thinking | 全部通过 ✅ |
| 稳定性 | `internal/cache` 连续 6 轮 | 全绿 ✅ |

> **注意**：本仓库 `.git` 的 tree object 已损坏（`fatal: unable to read tree`），`git status`/`git diff`/`git stash` 全部不可用，`go build` 需加 `-buildvcs=false` 才能给 main 包打戳。所有改动仅存在于磁盘工作区，**无法做基线 diff，也尚未提交**。建议尽快修复仓库（重新 clone 后应用工作区改动）。

## 十、状态

- **第三批完成**：45 项中 34 项落地、11 项（translator）按 AGENTS.md 提 issue #4720 后停止。
- **遗留依赖**：translator 11 项需 WRITE 权限维护者合入。
- **建议下一批**：`internal/pluginhost` 的 context 泄漏（vet 已报，与 P2 #23 同类）。
