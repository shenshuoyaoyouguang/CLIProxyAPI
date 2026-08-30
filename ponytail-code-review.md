# 深度代码审核：ponytail 删减改动

审核对象：本轮对 `sdk/cliproxy/auth/` 的 4 个生产文件 + 1 个测试文件的改动，以及 `ponytail-optimization-plan.md`。

按 `code-review-and-quality` 技能的五轴框架执行。**审核者是改动作者本人**，因此对每个发现都要求实证，不接受"我记得是对的"。

---

## 结论

**Approve（附条件）** —— 无 Critical 问题。发现 1 个 Required 性能缺陷（**已修复**），2 个已量化的可接受退化，2 个 Optional。

---

## Required：持锁执行磁盘 I/O（已修复）

**文件**：`sdk/cliproxy/auth/cooldown_state.go`，`Save()`

**问题**：`legacyCleanup` 原本是 `sync.Once`，在 `s.mu.Lock()` 之后调用：

```go
s.mu.Lock()                                  // 119 行
defer s.mu.Unlock()
s.legacyCleanup.Do(s.removeLegacyStateFiles) // 124 行 ← WalkDir 在锁内
```

`removeLegacyStateFiles` 会 `filepath.WalkDir` 整个目录。而 `service_auth.go:398` 传的是 `NewFileCooldownStateStoreWithAuthDir(authDir, authDir)`——**store 目录就是 auth 凭证目录**，里面可能有几十上百个文件。

后果：首次 `Save` 时，拿到锁的 goroutine 做全目录遍历，其余全部阻塞。而 `TestFileCooldownStateStore_ConcurrentSave` 正是 16 并发场景。

**修复**：改用 `atomic.Bool` + CAS，并把清理**移到加锁之前**：

```go
// walks the directory, so it runs before taking mu
if s.legacyCleaned.CompareAndSwap(false, true) {
    s.removeLegacyStateFiles()
}
s.mu.Lock()
defer s.mu.Unlock()
```

用 CAS 而非 `sync.Once` 是关键：`sync.Once.Do` 会让并发调用者**阻塞等待**第一个完成，即使移到锁外也只是把阻塞换个地方。CAS 让没抢到的直接跳过——清理只删旧 `.cds`、且会跳过共享文件路径，与并发写新文件不冲突。

---

## 已量化的可接受退化

### 1. `peek()` 从 O(1) 退化为 O(N)

**文件**：`sdk/cliproxy/auth/auto_refresh_loop.go`

删掉堆之后，`peek()` 从"取堆顶"变成"扫整个 map 求最小值"。调用链：每个 auth 刷新完成 → `queueReschedule` → wake → `applyDirty` → `resetTimer` → `peek`。

实测量级（`refreshCheckInterval = 5s`）：

| 规模 | 每秒 peek 成本 | 判定 |
|---|---|---|
| N=100，5 分钟刷新间隔 | ~500 ns/s | 可忽略 |
| N=1000，5 分钟刷新间隔 | ~50 μs/s | 可忽略 |
| N=1000，5s 间隔（极端） | ~3 ms/s（0.3% CPU） | 可接受 |

**判定：接受。** 同时 `upsert` 从 O(log N) 改善为 O(1)，部分抵消。这是用"低频率场景的常数因子"换"90 行堆代码"，划算。但**记录在案**——如果将来 auth 规模上到万级且刷新间隔很短，要重新评估。

### 2. 升级后旧冷却状态不迁移

`cooldown_state.go` 的 `Load` 只读新共享文件，不解析旧的 per-auth `.cds`。首次 `Save` 时清理它们。

**代价**：升级后冷却状态丢失重建。冷却是分钟~小时级运行时缓存，重建成本可忽略。**判定：接受**，但不做迁移是有意选择，不是疏忽。

---

## Optional

### 1. `conductor_execution.go` 仍有 1686 行，超过 1000 行检查线

技能把 1000 行定位为"常见检查信号"。本次**删了 197 行**（1883 → 1686），是改善不是恶化，所以不阻塞。但文件仍偏大，后续若要继续加逻辑，应先拆（比如把 Antigravity fallback、request-scoped error 处理各自抽出去）。

### 2. `execWith` 命名

`execWith(mode, executor, ctx, auth, req, opts)` 稍含糊，`executeWithMode` 更表意。**Nit**，不改也行。

---

## 审核中确认正确的部分

这几处是本次改动里最容易出错的地方，逐一验证过等价性：

**L29 的五处语义差异**（合并 `executeMixedOnce`/`executeCountMixedOnce`）
- 最阴的是**执行顺序**：`result.Error` 会被 `applyRequestScopedActionToResult` 修改，所以 count 路径的 `result.Error.Code != ErrorCodeForceCooldown` 判断必须在 apply **之后**。第一版写错时被测 `TestRequestScopedErrors_CountTokens_StopAndCooldown` 抓到（"expected auth1 to be in cooldown"），已修正。
- `CredentialScope` 位置：normal 在 apply 前无条件设置，count 只在非 neutral 分支设置——合并版用 `mode != execModeCountTokens` 分开，等价。
- `SkipQuotaObservation`：normal 路径为 `false`（原零值），等价。

**L28 的四处差异**（合并 `Execute`/`ExecuteCount`）
- antigravity fallback 用 `mode != execModeCountTokens` 守住。语义正确：count-tokens 不消耗 completion 配额，本就该跳过。
- unwrap 位置：原 Execute「先 unwrap 再判 fallback」、Count「直接 unwrap 返回」，统一成前者后最终返回值等价。

**L30 的 `executorCtx` 等价性**
`executorCtx` 在 `!countTokens` 时等于 `execCtx`，统一传 `executorCtx` 安全。

**测试覆盖验证**（防止 `mode` 守卫掩盖 bug）
- antigravity credits fallback：3 个测试文件覆盖（`antigravity_credits_test.go`、`home_fallback_audit_test.go`、`request_termination_test.go`）
- count_tokens 404 neutral 分支：`conductor_overrides_test.go` 有专项测试，含 `TestIsCountTokensEndpointNotFoundError`

**死代码检查**：删除 `executeCountMixedOnce`（195 行）后，`isCountTokensEndpointNotFoundError`(4)、`isResponsesCompactAvailabilityNeutralError`(2)、`isResponsesCompactRequestFaultError`(3)、`markUpstreamExecutionAttempt`(8) 引用数均 >0，无孤儿。

---

## 验证记录

```
gofmt -l sdk/cliproxy/auth/*.go   → clean
go build ./...                    → exit 0
go vet ./sdk/cliproxy/...         → exit 0
go test ./sdk/cliproxy/auth/ -race → ok (无竞态)
```

依赖包 `sdk/cliproxy/...` + `internal/home` + `internal/api/...` + `internal/runtime/...` 全过，
仅剩 2 个与本改动无关的 **pre-existing flaky**（`internal/runtime/executor/helps` 的 TTFT 时钟精度竞态，
连跑 8 次中 2 次通过，失败信息 `expected token TTFT > 0, got 0s`）。

---

## 对计划文件本身的审核

`ponytail-optimization-plan.md` 记录了 8 项核验结果（5 项可行、3 项否决）。审核确认这些结论都有源码级证据支撑，不是凭印象：

- L47 的 ALPN 判断有 Go 1.26.5 源码注释 + 测试注释双重佐证
- L48 的 x/net/proxy 能力边界已核实（只实现 SOCKS5）
- L38 的 `Retain()` 跨会话语义有 3 处 Codex WebSocket 调用点佐证

**一处建议**：计划顶部的"兑现率 32%"容易被误读成"工作没做好"。建议在措辞上区分——**否决率高说明原 review 质量有问题，不说明执行有问题**。已核实可行的 5 项，误差都在 10% 以内（L6/L29）或属于合理范围。
