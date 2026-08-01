## 目标
消除 `internal/cache/codex_reasoning_replay_cache.go`（505 行）与 `xai_reasoning_replay_cache.go`（423 行）的重复：把共享骨架（缓存键构建、KV 双后端读写、内存 map+TTL、批量驱逐、过期清理、clone）提取为单一共享实现，两个文件降为薄封装（仅保留 provider 专属逻辑）。

## 关键约束（已核实，全部由测试钉死，行为必须逐字节保持）
- 导出 API 名称/签名全部保留：executor 生产代码依赖 `CodexReasoningReplayTurnType`、`AppendCodexReasoningReplayItemsBestEffort`、`GetCodexReasoningReplayItemsRequired`、`DeleteCodexReasoningReplayItemRequired`；XAI 侧 `StoreXAIReasoningReplayItems`/`XAIReasoningReplayStoreStatus` 被 executor var hook 绑定且测试覆盖，类型必须不变（用类型别名实现）
- 行为差异（见上方结论）：append/CAS、trim、turn 标记、message item、hasReplayAnchor、四态枚举、expire 失败处理差异、KV 接口含 CAS —— 全部保留
- 同包测试引用未导出符号：`currentCodexReasoningReplayKVClient`、`codexReasoningReplayKVClient`、`codexReasoningReplayKVKey`、`trimCodexReasoningReplayItems`、`codexReasoningReplayMu`、`codexReasoningReplayEntries`；XAI 侧 `currentXAIReasoningReplayKVClient`、`xaiReasoningReplayKVClient`、`xaiReasoningReplayKVKey` —— 以别名/薄函数保留，`signature_cache.go:138-139` 调用的 `purgeExpiredCodex/XAIReasoningReplayCache` 保留为 1 行薄函数

## 设计：共享 store 结构体（不用泛型——差异是行为回调而非类型）
新建 `internal/cache/reasoning_replay_cache.go`（约 250 行）：
- `reasoningReplayEntry{Items, Timestamp}`、`reasoningReplayKVClient` 接口（5 方法，含 CAS）
- `reasoningReplayStoreStatus` 枚举（InvalidArgs/Stored/NoReplayableState/BackendError）
- `reasoningReplayStoreConfig`：memoryKeyPrefix、kvKeyPrefix、ttl、maxEntries、evictBatchSize、logLabel、expireFailureFatal、normalize 回调、appendTurn 回调、kvClient 回调（闭包读包装文件包级 var，保证测试注入生效）
- `reasoningReplayStore` 方法：`set`（返回四态）、`get`、`delete`、`clear`、`append`（CAS 循环，仅 Codex 用）、`purgeExpired`、`evictOldestLocked`，以及共享 `reasoningReplayCacheKey`/`reasoningReplayKVKey`/`cloneReasoningReplayItems`/`trimReasoningReplayItems`；日志模板用 label 替换（如 `home kv best-effort %s reasoning replay set failed prefix=cpa:%s:*:`），输出与现状逐字节一致

## 文件改动
1. **新建 `internal/cache/reasoning_replay_cache.go`**：上述共享实现
2. **重写 `codex_reasoning_replay_cache.go`**（约 505 → 200 行）：保留常量（原名原值）、`type codexReasoningReplayKVClient = reasoningReplayKVClient` + current var、`var codexStore = newReasoningReplayStore(...)`、全部导出函数委托 store（set 结果映射为 bool）、Codex 专属 normalize 系列（turn/reasoning/function_call/custom_tool_call）、`appendCodexReasoningReplayTurn`、`trimCodexReasoningReplayItems`（1 行调共享版）、`codexReasoningReplayKVKey`、`purgeExpiredCodexReasoningReplayCache`（1 行）
3. **重写 `xai_reasoning_replay_cache.go`**（约 423 → 190 行）：保留常量、`type xaiReasoningReplayKVClient = reasoningReplayKVClient` + current var、`type XAIReasoningReplayStoreStatus = reasoningReplayStoreStatus` + 4 个枚举值别名、`var xaiStore = newReasoningReplayStore(...)`（expireFailureFatal=false）、导出函数委托（`StoreXAIReasoningReplayItems` 直接返回 store.set 的四态）、XAI 专属 normalize（reasoning/message/function_call/custom_tool_call）、`xaiReasoningReplayKVKey`、`purgeExpiredXAIReasoningReplayCache`
4. **测试适配（仅 2 处）**：
   - `codex_reasoning_replay_cache_test.go:360-361`：`codexReasoningReplayMu/codexReasoningReplayEntries` → `codexStore.mu/codexStore.entries`（存储移入 store 后包级 var 不复存在）
   - `xai_reasoning_replay_cache_test.go`：给 `fakeXAIReasoningReplayKVClient` 补 `KVCompareAndSwap` 方法（共享接口含 CAS，fake 需实现；从 codex fake 复制，约 14 行）
5. **不动的文件**：`signature_cache.go`（保持 138-139 行接线）、`antigravity_reasoning_replay_cache.go`（第三个同构文件，语义更复杂，合并留作后续单独任务）、executor/translator 全部不动

## 明确不做
- 不删除已确认的死代码（`DeleteCodexReasoningReplayItem`、`DeleteXAIReasoningReplayItem` 零调用；`CacheXAIReasoningReplayItemsBestEffort` 仅内部使用）——仅提及
- 不统一 modelName 的长度前缀（当前未提交的 key 修复只对 sessionKey 加长度前缀，保持现状，避免扩大 diff）

## 验证
1. `gofmt -w .`
2. `go build -o test-output ./cmd/server && rm test-output`（AGENTS.md 要求）
3. `go test ./internal/cache/...`（18+ 个缓存测试，行为钉死处全部回归）
4. `go test ./internal/runtime/executor/`（codex/xai/websockets 集成测试，含 var hook 覆盖）
5. `go test ./...` 全量（若耗时过长则至少保证受影响包全绿，并如实报告）
6. 用 code-reviewer 子代理复核 diff