# CLIProxyAPI 删减优化方案（按影响优先级排序）

基于 `ponytail-review.md`，但对每一条做了源码核验。构建基线已确认干净：`go build ./...` 退出码 0。

> **先读这一节：报告里有 4 处结论是错的，直接影响排期。**

| 报告条目 | 报告说法 | 实测 | 影响 |
|---|---|---|---|
| L12 | 删 `scheduler.go`，复用 `selector.go`，-1000 行 | `authScheduler` **是活的**：`conductor.go:126` 字段、`:201` 构造、**41 处 `pickSingle` 调用**；且遗漏了 `scheduler_test.go` **2080 行** | 收益远大于估算（毛 -3303），但语义不等价，风险最高 |
| L15 | 8 个接口「各只有 1 个实现」，YAGNI，-120 行 | false：`ProviderExecutor` **20 个实现**、`Selector` **4 个实现**、`Stop()` 6 个；只有 `Hook`(1)、`PickAuth`(1) 真的薄 | **不能整组删**，只能摘 `Hook`/`NoopHook`，约 -60 |
| L29 | `ExecuteCount`(50 行) 是 `Execute` 的复制粘贴，-120 行 | 找错目标了。真正的孪生是 `executeMixedOnce`(190 行) 与 `executeCountMixedOnce`(194 行)，**`diff` 只有 26 行** | 收益比估算高约 60 行，且改动点更清晰 |
| L48 | 改用 `golang.org/x/net/proxy` | `go.mod` 已有 `golang.org/x/net v0.57.0` | 无需新增依赖，可直接开工 |

> **⚠️ 2026-08-31 实测校准（重要）**：两项已完成，误差天差地别——
>
> | 条目 | 报告估算 | 实测 | 偏差 |
> |---|---|---|---|
> | L7 去 heap | -240 | **-52** | 虚高 4.6 倍 |
> | L6 cds 塌缩 | -180 | **-165** | 误差 8%，准 |
> | L48+L47+L49 代理改造 | -190 | **0（全部否决）** | 技术前提错误 |
> | L26 home error 类型 | -100 | **0（否决）** | 删了丢重试语义 |
> | L29 合并 MixedOnce 孪生 | -180 | **-162** | 误差 10%，准 |
> | L28 合并重试循环 | -120 | **-35（部分）** | executeHome 不合，虚高 3.4 倍 |
> | L30 拆 executeHomeOnce | -100 | **-1（主体否决）** | 见下 |
>
> | L38 home_selection 生命周期 | -200 | **0（否决）** | 跨会话所有权转移 |
>
> **八项累计实测 -415 行，报告估算 -1,310，兑现率 32%。8 项里 4 项整体/主体否决。**
>
> **P0 与 P1 已全部核验完毕**，可行与不可行各占一半。剩下只有 P2 的 L12/L59 和未核验的 YAGNI/标准库/长尾组。
>
> 两个值得单独说的：
>
> 1. **L30 暴露了报告的概念混淆**：给它标「-100 行」，建议的方法却是 **extract method**。extract method 是重构不是删减——代码只是挪位置，净行数通常 **+10 ~ +30**，怎么都不可能 -100。「降低单函数复杂度」和「减少总行数」被当成一回事了。
> 2. **L38 暴露了报告的抽象层次误判**：它把「跨会话的所有权转移」（`Retain()` 把 selection 交给 WebSocket 长连接）看成了「函数内的局部资源清理」，所以才觉得 `defer cancel()` 能替代。`defer` 在函数返回时触发，而这里 selection 必须活到函数返回之后。
>
> **更值得警惕的是否决率而非误差率**：核验的 8 项里，1 项虚高 4.6 倍、1 项虚高 3.4 倍、3 项整体/主体不可做（照做会引入生产 bug 或丢失语义）。**报告的行数估算和技术可行性判断都必须逐条验证，不能直接照抄执行。**
>
> 按 43% 兑现率外推，整份计划的 3500/5500 实际可能只有 **-1,400 ~ -2,000**（不含 L12）或 **-4,200 ~ -5,100**（含 L12）。
>
> **建议放弃「总删减量」这个 KPI，改成逐项判断「这一项值不值得做」。** 已验证值得做的：L7、L6、L29。明确不该碰的：`sdk/proxyutil/` 全部、L26。

另外一个关键结构性事实：**删减量是双峰分布的**。

- 不动 `scheduler.go` → 天花板约 **-2,100 ~ -2,600 行**，到不了 3500。
- 动 `scheduler.go` → 直接跳到 **-4,900 ~ -5,750 行**。

报告给的「保守 3500 / 激进 5500」不是一个连续区间，而是「做不做 L12」这一个决策的两态结果。所以优先级排序的真正问题只有一个：**L12 要不要做、什么时候做**。

---

## P0 — 低风险、高确定性、隔离性好（先做，共约 -710 行）

### 1. L7 + L8 — `sdk/cliproxy/auth/auto_refresh_loop.go`（455 行）→ **实测 -52 行** ✅ 已完成（2026-08-31）

> **⚠️ 实测校准**：报告估 -240 行，**实际只有 -52 行**（455 → 403，36 增 88 删）。删掉堆省下约 90 行，但 `peek`/`popDue` 从「取堆顶 O(1)」改成「扫描整个 map 求最小」反而各涨 7~8 行。**报告在这条上虚高约 4.6 倍。**
>
> 这个校准很关键：**整份计划的 3500/5500 都建立在这些单项估算上**。若普遍存在同量级虚高，真实总量要打对折。建议每完成一项就回填实际数字，前三项做完重估总量。
>
> **L8（删 worker pool）已否决，不要做**：`refreshMaxConcurrency = 16`（`conductor_refresh.go:25`），且 `AuthAutoRefreshWorkers` 是**用户可配项**（`internal/config/config.go:75`，yaml key `auth-auto-refresh-workers`）。改成串行会把 16 个并发 OAuth token 刷新（每个都是网络调用）变成串行，刷新波峰延迟放大约 16 倍，同时删掉一个对外暴露的配置项。这是拿性能换行数，不是删减。
>
> 已完成内容：删 `refreshHeapItem` + `refreshMinHeap`（含 `Len/Less/Swap/Push/Pop` 五个方法）；`index` 从 `map[string]*refreshHeapItem` 改为 `map[string]time.Time`（**保留字段名 `index`**，`conductor_remove_test.go:94/106` 直接读写它，改名会编译失败）；`upsert`/`remove` 塌缩成 map 赋值/删除；`rebuild` 去掉中间 `entry` 结构体；`popDue` 改为遍历 map + `slices.SortFunc` 按到期时间排序，保持「最逾期的优先」语义。精确定时的 `time.Timer` 机制保留（换成 1s ticker 会让空转时每秒白唤醒一次）。
>
> 验证：`gofmt` ✓ / `go build ./...` ✓ / `go vet` ✓ / `go test ./sdk/cliproxy/auth/ -count=1` ✓ / `-race` ✓（8.008s，无竞态）。

- **问题**：文件内实现了完整的 `container/heap`（`Push/Pop/Len/Less/Swap`）+ 4 个辅助 map 组成的「到期时间堆 + 脏集合」，配 16 协程 worker pool，只为定时刷新 N 个 auth。
- **核验**：`container/heap` 在**全仓库仅此一处**引用（`grep -rn "container/heap"` 只有这一个文件）。删掉不会波及任何其它包。
- **替代方案**：
  - 堆 + 脏集合 → 一个 `[]*Auth` 按 `nextRefreshAt` 排序的切片，配 `time.Ticker(1s)` 扫一遍。N 是十到百量级，每秒一次线性扫描的开销可以忽略。
  - 或者更省事：每个 auth 一个 `time.AfterFunc(d, func(){ refresh(a) })`，改期时 `timer.Reset(d)`。连 ticker 都不用。
  - 16 协程 worker pool → `for { time.Sleep(interval); for _, a := range auths { refresh(a) } }`（L8，-40 行）。
- **风险**：低。行为差异只有「刷新时刻的抖动粒度从堆精度变成 1s 粒度」，而刷新间隔本身是分钟级。
- **验证**：`go test ./sdk/cliproxy/auth/ -run 'Refresh|AutoRefresh' -count=1`。

### 2. L6 — `sdk/cliproxy/auth/cooldown_state.go` → **实测 -165 行** ✅ 已完成（2026-08-31）

> **实测校准**：报告估 -180，实际 **-165**（生产 335→227 = -108；测试 617→560 = -57）。误差 8%，**这条报告估得准**（对比 L7 虚高 4.6 倍）。
>
> **⚠️ 两个必须知道的坑，报告完全没提：**
>
> 1. **单文件必须沿用 `.cds` 扩展名，不能用 `.json`。** `service_auth.go:398` 传的是 `NewFileCooldownStateStoreWithAuthDir(authDir, authDir)`——**store 目录和 auth 凭证目录是同一个**。而 `sdk/auth/filestore.go:184`（以及 gitstore / objectstore / watcher）扫凭证时只按 `strings.HasSuffix(lower(name), ".json")` 过滤，递归扫子目录，解析失败静默跳过。一个名为 `cooldown-state.json` 的文件会被当成合法凭证加载，`metadata["type"]` 为空兜底成 `"unknown"`，然后**构造出一条幽灵 auth**。用 `.cds` 就天然被忽略。
> 2. **原子写（temp + rename）不能省。** `cooldown_state_test.go:218` 明确断言并发 Save 后无 `*.tmp` 残留，且并发读不能看到半截文件。报告建议的「`os.WriteFile` 一行搞定」会破坏这个保证——**已按原子写实现，不要照报告改**。

已完成内容：删掉整套 per-auth 路径推导（`statePath`/`stateRelativePath`/`cdsPathForRel`/`sanitizeCooldownFileName`/`cooldownFileNameUnsafe` 正则）；`Load` 从 `WalkDir` 扫目录改成读单个 `cooldown-state.cds`；`Save` 从「按 auth 分组写 N 个文件 + 清理 stale」改成「整体写一个文件 + 空则删除」；`cooldownStateFile` 去掉 `AuthID`/`Provider` 字段（单文件存多条 auth 时这两个字段是错的）。

保留的部分（**都是刻意的，别当废代码删**）：
- `CooldownStateRecord.AuthFile` 导出字段 + `cooldownAuthFile()`：`conductor_cooldown.go:676/694` 在用，且 `AuthFile` 是 sdk 对外暴露的字段，删了是 breaking change。
- `NewFileCooldownStateStoreWithAuthDir(dir, authDir)` 签名：`authDir` 参数现在无用，但保留以兼容 `service_auth.go:398` 和 4 处测试调用，改签名是 sdk 的 breaking change。
- 旧的 per-auth `.cds` 文件用 `sync.Once` 在首次 Save 时清理一次，避免升级后磁盘垃圾堆积。**代价**：升级后旧的冷却状态不迁移，会重建（冷却状态是分钟~小时级的运行时缓存，重建成本可忽略）。

验证：gofmt ✓ / `go build` ✓ / vet ✓ / `go test ./sdk/cliproxy/auth/ -race` ✓ / `sdk/cliproxy/...` + `internal/home` + `internal/api/...` 全 ok。

- **问题**：手写的 per-auth `.cds` 状态文件。每个 auth 一个子目录 + 一个文件，`Save` 里 `os.MkdirAll(s.dir)` 之后又 `os.MkdirAll(dir)`（L5，重复建目录）。
- **核验**：冷却状态本身已在内存里，持久化只是「进程重启后恢复」的辅助路径，不是热路径。
- **替代方案**：一个共享目录 + 一个 JSON 文件，整个文件塌缩成 ~30 行：
  ```go
  func (s *FileCooldownStateStore) Save(ctx context.Context, records map[string]QuotaState) error {
      if len(records) == 0 { return os.Remove(s.path) }          // 空则删除
      b, err := json.Marshal(records)
      if err != nil { return err }
      return os.WriteFile(s.path, b, 0o600)                       // 目录在构造时建一次
  }
  func (s *FileCooldownStateStore) Load(ctx context.Context) (map[string]QuotaState, error) {
      b, err := os.ReadFile(s.path)
      if errors.Is(err, os.ErrNotExist) { return nil, nil }
      ...
  }
  ```
- **风险**：低，但有一个迁移点——旧的 per-auth 目录布局会留下孤儿文件。在 `Load` 里加一次性清理，或在 release note 里说明。
- **验证**：`go test ./sdk/cliproxy/auth/ -run 'Cooldown' -count=1`；注意 `claude_ratelimit_cooldown_test.go:305` 有个 `mockCooldownStateStore`，接口保持不变即可。

### 3. L48 + L47 + L49 — `sdk/proxyutil/proxy.go` → ❌ **全部否决，收益 0**（2026-08-31 核验）

报告估 -190 行。**核验后三项全部不可做**，逐条给证据：

| 条目 | 报告建议 | 核验结论 |
|---|---|---|
| L48 | `httpConnectDialer` 改用 `golang.org/x/net/proxy` + `http.ProxyURL` | ❌ **技术前提错误**。`x/net/proxy` 只实现 SOCKS5，`FromURL` 对 `http`/`https` 无内建类型（需 `RegisterDialerType` 自己注册，等于没省）。而 `BuildDialer` 的契约是返回 `proxy.Dialer` 接口（给 WebSocket / 裸连接用），**标准库和 x/net 都没有 HTTP CONNECT 的 Dialer 实现**。`http.ProxyURL` 是给 `http.Transport.Proxy` 用的，接口不匹配，替换不了。 |
| L47 | Go 1.20+ 原生支持 `https://` 代理，`buildHTTPSProxyDialTLSContext` 整段删除 | ❌ **会导致生产事故**。技术前提**成立**（Go 1.26.5 源码 `transport.go:2039-2040` 确认支持 `https://proxy.com\|https\|foo.com`），但那段手工代码里 `NextProtos = []string{"http/1.1"}` **是有意的 bugfix**。`proxy_test.go:518-520` 的注释写明：代理一旦协商 h2 ALPN，就会期望 HTTP/2 帧并拒绝 HTTP/1.1 的 CONNECT，表现为 `unexpected EOF / bogus greeting`。该测试专门断言 `NegotiatedProtocol != "h2"`。删掉 = 引入随机连接失败。 |
| L49 | `Mode` 四态 → `bool + *url.URL` | ❌ **删了改行为**。Mode 被 **8 处跨包生产代码**引用（`utls_transport.go:185`、`media.go:271`、`sideband.go:696/699`、`codex_websockets_connection.go:189/192`、`utls_client.go:63/300`）。且 `ModeInherit` 与 `ModeDirect` 语义有别——前者「未配置，跟随环境」，后者「显式绕过」，`utls_transport.go:185` 专门判断 `mode != ModeInherit`。合并会丢掉这个区分。 |

**结论：不要动 `sdk/proxyutil/`。** 这一项从 P0 里划掉。报告在这三项上的判断全部基于对 Go 标准库和 x/net 能力的误判。

- **问题**：
  - `httpConnectDialer.DialContext`（L222-313，91 行）：手写的 CONNECT 实现。
  - `buildHTTPSProxyDialTLSContext`（L135-186，51 行）：手写的 CONNECT-over-TLS。
  - `Mode` 四态枚举 + `Setting` 结构体（L20-39）：为「解释一个代理字符串」造了 4 个状态。
- **核验**：`golang.org/x/net v0.57.0` **已在 go.mod 中**，`golang.org/x/net/proxy` 可直接用，无新增依赖。
- **替代方案**：
  - SOCKS5 → `golang.org/x/net/proxy.SOCKS5("tcp", addr, auth, proxy.Direct)`（L48 主体，-100）。
  - HTTP CONNECT → `http.Transport{Proxy: http.ProxyURL(u)}` 原生支持（L48 剩余部分）。
  - HTTPS 代理 → Go 1.20+ 起 `http.ProxyURL` 已能处理 `https://` 代理 URL，`buildHTTPSProxyDialTLSContext` 整段删除（L47，-60）。
  - `Mode` 四态 → `type Setting struct { URL *url.URL; Direct bool }`，nil+false=继承、nil+true=直连、URL=走代理（L49，-30）。
  - `bufferedConn`（L354）只在 CONNECT 路径用，随 L48 一起删。
- **风险**：中低。**必须实测**：CONNECT-over-TLS 的 TLS 握手指纹、以及 `proxyAuthorization` 的 header 拼接（`Proxy-Authorization` 而非 `Authorization`）在替换后是否一致。
- **验证**：包内已有 1047 行测试，全跑 `go test ./sdk/proxyutil/ -count=1`；另需一个真实代理的手动冒烟（HTTP 代理 + SOCKS5 + HTTPS 代理三种）。

### 4. L26 — `sdk/cliproxy/auth/conductor_home.go` → ❌ **否决，收益 0**（2026-08-31 核验）

报告说这 7 个类型是「for one retry-after field」，建议用 `errors.New` 替换。**核验后不成立**——每个类型都承载实际语义，替换会丢掉重试策略的元数据：

| 类型 | 实际作用 | 能否删 |
|---|---|---|
| `homeErrorEnvelope` / `homeErrorDetail` | Home 协议的 JSON 错误契约（6 字段） | ❌ 是协议结构，不是样板 |
| `homeDispatchRetryAfterError` | 携带 `cause` + `retryAfter` + `requestRetry` | ❌ 生产 13 处引用 |
| `homeRetryRoundExhaustedError` | 实现 `RetryAfter() *time.Duration` 供调用方决策重试时机 | ❌ 生产 11 处；`errors.New` 无法承载方法 |
| `homeRetryRoundTiming` | 多错误 retry-after 聚合（immediate 优先、invalid 短路、取最小正值） | ❌ 真业务逻辑，不是样板 |

**结论**：这 7 个类型不是「为了一个字段造的」，报告误判了。生产引用 51 处、测试引用 23 处。动了就是高风险零收益。**不要碰。**

- **问题**：为了表达「一个 retry-after 字段」造了 7 个类型/函数：`homeErrorEnvelope`(L117)、`homeErrorDetail`(L121)、`homeDispatchRetryAfterError`(L130)、`homeRetryRoundExhaustedError`(L140)、`homeRetryRoundTiming`(L194)，加 `markHomeRetryRoundExhausted` / `isHomeRetryRoundExhausted`。
- **替代方案**：标准库 `errors` + 一个结构化字段：
  ```go
  var errHomeRetryRoundExhausted = errors.New("home retry round exhausted")
  // 需要携带 reason 时：fmt.Errorf("%w: %s", errHomeRetryRoundExhausted, reason)
  // 需要携带 retry-after 时：在自己已有的 Error 结构上加一个 RetryAfter time.Duration 字段
  ```
  调用方从类型断言改成 `errors.Is(err, errHomeRetryRoundExhausted)`。
- **风险**：低。但注意 `home_retry_contract_test.go`（**1648 行**）大概率直接断言了这些类型，删之前先 `grep -rn "homeRetryRoundExhaustedError\|homeDispatchRetryAfterError" --include=*_test.go`。
- **验证**：`go test ./sdk/cliproxy/auth/ -run 'HomeRetry|RetryContract' -count=1`。

---

## P1 — 中风险、收益高（P0 稳定后再做，共约 -600 行）

### 5. L29 — 合并 `executeMixedOnce` / `executeCountMixedOnce` → **实测 -162 行** ✅ 已完成（2026-08-31）

> **实测校准**：报告估 -180，实际 **-162**（1883 → 1721）。删掉 195 行孪生函数，新增 33 行 `execMode`/`execWith`/分支。误差 10%。
>
> **⚠️ 合并时踩到的坑（照报告做必踩）**：5 处语义差异里，最阴的是**执行顺序**。原 count 版的判断是 `isCountTokensEndpointNotFoundError(...) && result.Error.Code != ErrorCodeForceCooldown`，而 `result.Error` 会被 `applyRequestScopedActionToResult` 修改——所以 **neutral 必须在 apply 之后算**。我第一版把 neutral 提到 apply 之前，直接被 `TestRequestScopedErrors_CountTokens_StopAndCooldown` 抓到（「expected auth1 to be in cooldown」）。
> 正确顺序：normal 路径的 `CredentialScope` 在 apply **前**设置（无条件），两个模式的 neutral 判断都在 apply **后**。

已完成：新增 `execMode`（`execModeNormal`/`execModeCountTokens`）+ `execWith()` 分派 helper；`executeMixedOnce` 加 `mode` 参数；删除 `executeCountMixedOnce`（195 行）；两个调用点分别传 `execModeNormal` / `execModeCountTokens`。

5 处语义差异的合并方式：
1. 执行分派（2 处）→ `execWith(mode, executor, ...)`
2. `SkipQuotaObservation`（2 处）→ `mode == execModeCountTokens`
3. **`CredentialScope` 位置**（最阴）→ normal 无条件在 apply 前设；count 只在非 neutral 分支设
4. 可用性中性判断 → count 用 `isCountTokensEndpointNotFoundError`，normal 用 `isResponsesCompactAvailabilityNeutralError`
5. `isResponsesCompactRequestFaultError`（2 处）→ 用 `mode != execModeCountTokens &&` 守住

验证：gofmt ✓ / build ✓ / `go test ./sdk/cliproxy/auth/ -race` ✓ / `sdk/cliproxy/...` + `internal/home` + `internal/api/...` + `internal/runtime/...` 全过（除 2 个已知 pre-existing flaky）。

- **位置**：`conductor_execution.go:415-604`（190 行）与 `:606-799`（194 行）。
- **核验结果（实测 diff）**：两个函数 **只有 26 行差异**，语义差别集中在 5 个点：
  1. `executor.Execute` vs `executor.CountTokens`（2 处）
  2. count 路径多 `SkipQuotaObservation: true`（2 处）
  3. count 路径用 `isCountTokensEndpointNotFoundError` 替代 `isResponsesCompactAvailabilityNeutralError`
  4. count 路径没有 `isResponsesCompactRequestFaultError` 判断（2 处）
  5. `CredentialScope` 赋值位置不同
- **替代方案**：合成一个函数，用 `mode` 枚举参数化：
  ```go
  type execMode int
  const ( execModeNormal execMode = iota; execModeCountTokens )

  func (m *Manager) executeMixedOnce(ctx, providers, req, opts, maxRetryCredentials, retryRound, defaultRequestRetry int, mode execMode) (cliproxyexecutor.Response, error)
  ```
  内部：调用点用 `mode.call(executor, execCtx, auth, execReq, execOpts)` 分派 `Execute`/`CountTokens`；`SkipQuotaObservation: mode == execModeCountTokens`；两个 `isResponsesCompact*` 判断用 `if mode == execModeNormal && …` 守住。净删 194 行，新增约 14 行分支。
- **风险**：中。**这是热路径**。5 处差异必须逐条对照迁移，漏一处就会改变「count_tokens 404 不挂模型」这个特意加的行为（源码里有注释明确说明）。
- **验证**：`go test ./sdk/cliproxy/auth/ -run 'Execute|CountTokens|Compact' -count=1` + `home_execution_paths_test.go`（1524 行）全跑。

### 6. L28 — 合并重试循环 → **实测 -35 行** ✅ 部分完成（2026-08-31）

> **实测校准**：报告估 -120，实际 **-35**（1721 → 1686）。**报告说「三个近乎相同的重试外壳」不成立——只有两个真同构。**
>
> **⚠️ `executeHome` 不能合并，已保留。** 它的循环结构跟 `Execute`/`ExecuteCount` **根本不同**，不是「少量 flag 差异」：
>
> | | `Execute` / `ExecuteCount` | `executeHome` |
> |---|---|---|
> | retry-round 状态机 | 无 | 有：`retryRoundPending` + `retryRoundWaited` 双标志、`pendingHomeRetryRoundDelay()`、`homeRetryAllowed()`，命中时 `continue` **不递增 attempt** |
> | 传给 once 的 limit | 常量 `-1` | `&homeRetryLimit` **可变指针**，被 once 函数写回 |
> | `!shouldRetry` 时 | `break`，走统一尾部（ctx.Err 检查 → preferred 合并 → unwrap → antigravity fallback） | **直接 return**，且要额外做 `isHomeRetryRoundExhausted` 的 preferred 合并 |
> | 尾部 antigravity fallback | Execute 有 / ExecuteCount 无 | 完全没有 |
>
> 相同度大概 50%（对比 L29 的两个孪生是 94%）。强行合并只会塞满 `if homeMode` 分支，可读性下降。**否决这部分。**

已完成的部分：`Execute` 和 `ExecuteCount` 合并进 `executeWithRetry(ctx, providers, req, opts, mode)`，两个公开入口退化成 1 行转发（公开签名不变，外部零感知）。

4 处差异的处理：
1. `executeHome(..., false/true)` → `mode == execModeCountTokens`
2. `execModeNormal` / `execModeCountTokens` → 透传 `mode`
3. **antigravity credits fallback**：只有 Execute 有 → 用 `mode != execModeCountTokens &&` 守住（count-tokens 不消耗 completion 配额，本来就该跳过）
4. **unwrap 位置**：Execute 是「先 unwrap 再判断 fallback」，ExecuteCount 是「直接 unwrap 返回」→ 统一成前者，两者最终返回值等价

验证：gofmt ✓ / build ✓ / `-race` ✓ / `sdk/cliproxy/...` + `internal/home` + `internal/api/...` + `internal/runtime/...` 全过（除 2 个已知 pre-existing flaky）。
- **验证**：同上 + 全包 `go test ./sdk/cliproxy/auth/ -count=1`。

### 7. L30 — 拆分 `executeHomeOnce` → **实测 -1 行** ⚠️ 主体否决（2026-08-31）

> **这个条目本身有概念错误**：报告标了「-100 行」，但描述的方法是 **extract method**。**extract method 是重构不是删减**——代码只是换个地方放，净行数通常是 **+10 ~ +30**（多出函数签名和调用），不可能 -100。报告把「降低单函数复杂度」和「减少总行数」混为一谈了。

**主体否决理由**：这个函数 225 行的复杂度是**内在的**，不是没拆好。它要同时协调：selection 生命周期（`End`/`endHomeSelectionBeforeRedispatch`/`retainHomeWebsocketSelection`）、attempt 上下文（`homeExecutionAttemptContext`/`releaseAttempt`）、模型别名解析、请求拦截器、结果上报、以及 5 类错误分支（request-scoped action / request invalid / credential scope / terminated / stop）。

最大的块是内层 `for _, upstreamModel := range models`（约 110 行），但它用 `continue`/`break`/多处 `return` 控制流，并且要写回外层 `lastErr`/`upstreamErr`。抽成函数需要 15+ 个参数、4 个返回值（含一个 outcome 枚举来表达 continue/break）。**这不是简化，是把复杂度搬到签名里。**

**已做的小改进（-1 行）**：内层循环里有个手写闭包在分派 `CountTokens`/`Execute`：
```go
execute := func() (cliproxyexecutor.Response, error) {
    if countTokens { return selection.Executor.CountTokens(executorCtx, ...) }
    return selection.Executor.Execute(execCtx, ...)
}
```
改成复用 L29 引入的 `execWith(homeExecMode, selection.Executor, executorCtx, ...)`。行数几乎不变，但**分派逻辑收敛到唯一一处**——否则 L29 建的 `execWith` 就形同虚设。

> 如果真想降低这个函数的认知负担，正确做法是**先补测试再拆**，而不是直接 extract method。它现在 225 行没有针对性的单元测试（测的都是上层的 `executeHome` 行为）。在没测试网的情况下拆 225 行热路径，是在赌。
- **验证**：重构前后各跑一次 `go test ./sdk/cliproxy/auth/ -count=1`，结果必须完全一致。

### 8. L38 — `sdk/cliproxy/auth/home_selection.go` → ❌ **否决，收益 0**（2026-08-31 评估）

报告说 `sync.WaitGroup` + `defer cancel()` 就是同一件事。**彻底误判**——它把「跨会话的所有权转移」看成了「函数内的局部资源清理」。

**决定性证据：`Retain()` 是跨请求持有。**

```go
// Retain transfers selection ownership from a request to an execution session.
func (s *HomeDispatchSelection) Retain() { ... s.retained.Store(true) }
```

3 处调用**全部在 Codex WebSocket 长连接路径**：`internal/client/codex/live/live.go:428`、`websocket.go:80`、`internal/runtime/executor/codex_websockets_session.go:261`。

**selection 的所有权会从 HTTP 请求转移到 WebSocket 会话，生命周期远超创建它的那个函数帧。`defer cancel()` 在函数返回时触发，而这里 selection 必须在函数返回后继续存活——语义完全对不上。**

另外 5 条同样无法用 `defer` 表达：

| 现有机制 | 作用 | `defer cancel()` + WaitGroup 能否替代 |
|---|---|---|
| `attemptCancels`（map[uint64]*attemptCancel） | 管理 **N 个**并发 attempt 的 cancel；`AttemptContext()` 被 **5 处跨包调用**（`server_routes.go`、`live/`、`sideband.go`、`capabilities.go`、`websocket.go`） | ❌ 单个 cancel 管不了 N 个并发 attempt |
| `executionResources.Close()` | **逆序**执行 closers + `errors.Join` **聚合**所有错误 | ❌ defer 是 LIFO 但会丢错误 |
| `EndWithRelease` 的 `sync.Once` | 幂等，重复 End 只生效一次 | ❌ defer 不提供幂等 |
| `ReleaseTicket.Wait(ctx)` | 等待资源**真正释放完成**，带 group/sequence 语义 | ⚠️ WaitGroup 只能覆盖「等待」，丢了 group/sequence |
| `Add` 遇已关闭时返回 `ErrRegistryNotAccepting` 并立即执行 closeFn | 明确处理「注册进已关闭的 selection」 | ❌ 需重写全部错误处理 |

**还有个硬依赖**：这个文件深度耦合 `executionregistry`（`Scope`、`ErrInvalidExecutionResource`、`ErrRegistryNotAccepting`、`ReleaseTicket`）。**L59 不动，L38 就动不了。**

**结论：不要碰 `home_selection.go`。** 它是 WebSocket 长连接的生命周期核心，改坏的后果是连接泄漏或提前关闭。

- **问题**：`executionResources` + `attemptCancels` + `HomeDispatchSelection` + 8 个辅助方法，约 280 行在做资源生命周期跟踪。
- **替代方案**：`sync.WaitGroup` + `defer cancel()` 就是同一件事：
  ```go
  ctx, cancel := context.WithCancel(parent)
  defer cancel()
  var wg sync.WaitGroup
  wg.Add(1)
  go func() { defer wg.Done(); … }()
  wg.Wait()   // 等价于等所有资源释放
  ```
- **风险**：中。**要先确认**现在这套 tracking 是否承担了「超时强制回收」或「跨请求持有」的语义——如果有，单纯 `defer cancel()` 会改变生命周期。先读 `home_concurrency.go` 和 `internal/home/concurrency_release.go` 再动手。
- **验证**：`go test ./sdk/cliproxy/auth/ ./internal/home/ -count=1`；`home_concurrency_test.go` 是重点。

---

## P2 — 高收益、高风险（需要设计决策，最后做）

### 9. L12 — `sdk/cliproxy/auth/scheduler.go`（1223 行）+ `scheduler_test.go`（2080 行）→ **净 -2,800 行**

这是全表最大的一块，也是唯一能让删减量突破 5000 的项。但它**不是报告说的「删文件，复用 selector.go」**。

- **核验结果**：
  - `authScheduler` 是活的：`conductor.go:126` 有 `scheduler *authScheduler` 字段，`:201` 调用 `newAuthScheduler(selector)`。
  - 调用面：`pickSingle` 41 次、`upsertAuth` 17 次、`pickMixed`/`pickMixedWithStrategy` 各 2 次、`RecordRemovalTombstone` 3 次、`rebuild` 2 次、`setSelector` 1 次。
  - 测试：`scheduler_test.go` **2080 行**（报告完全没算）。

- **真正的障碍（报告没提）：两个结构的语义不等价。**

  | | `authScheduler` | `selector.go` |
  |---|---|---|
  | 形态 | 增量索引：`readyBucket → readyView → cooldownQueue` 四层树，按 provider/model 分桶 | 全量扫描：`Pick(ctx, provider, model, opts, auths []*Auth)` |
  | 选人 | `pickSingle(ctx, provider, model, opts, tried)` — 不传 auth 列表 | 必须传入 `auths []*Auth` 切片 |
  | 多 provider | `pickMixed(providers []string, …)` 一次跨 provider 选 | 单次只认一个 provider，需外层循环 |
  | 复杂度 | O(1) 桶查找 | O(N) 每次扫描 |

  直接替换意味着 **41 处 `pickSingle` 调用点全部要改成「取 auth 切片 → 调 `selector.Pick`」**，并且每次选择从 O(1) 变成 O(N)。

- **建议的三步走，不要一把梭**：
  1. **先补基准测试**：在动之前写一个 `BenchmarkPickSingle`，覆盖 10 / 100 / 1000 个 auth 的选人耗时。没有这个数，就无法判断 O(N) 扫描能不能接受。**这一步不做，后面全是猜。**
  2. **加适配层**：保留 `authScheduler` 的对外方法签名（`pickSingle`/`pickMixed`/`upsertAuth`/`removeAuth`…），把内部实现换成「从 `m.auths` 取切片 + 调 `selector.Pick`」。这样 41 个调用点零改动，先验证功能等价性 + 拿到性能数据。
  3. **确认等价后再删壳**：删掉四层树结构，并把 `scheduler_test.go` 里与 `selector_test.go`（2487 行）重复的部分删掉。**注意**：tombstone、mixed-provider、websocket 偏好这几块 coverage 大概率是 `selector_test.go` 没有的，需要往 `selector_test.go` 移植约 300-500 行，否则会留测试盲区。
- **收益核算**：1223（实现）+ 2080（测试）= 3303 毛；减去移植的 ~500 行测试 = **净 -2,800**。
- **风险**：**高**。认证选人是整个代理的核心路径， regressions 会表现为「偶发选到被冷却的 auth」，很难在 CI 里复现。建议独立 PR、独立灰度。

### 10. L59 — `sdk/cliproxy/executionregistry/registry.go`（470 行）→ **-350 行**

- **问题**：`BeginDispatch → Install → End → EndWithRelease → SetReleaseSink` 状态机，配 `ReleaseGroup`/`ReleaseTicket`/`Scope`/`ScopeSpec`/`PendingDispatch` 五个类型，只为「跟踪一次 Home 执行」。
- **核验结果（重要）**：这东西的使用面远比报告暗示的广：
  - `executionregistry.New` **134 处引用**
  - 跨包使用：`internal/home/concurrency_release.go`、`internal/client/codex/live/`（3 处）、`internal/api/server_test.go`、`sdk/api/handlers/`（2 处）
  - 包内测试 515 行（`registry_test.go` 385 行 + `concurrency_release_test.go` + `observation_test.go`）
- **替代方案**：`type dispatchID uint64` + `map[uint64]chan struct{}` + `sync.WaitGroup`；L60 的五个类型塌缩成一个整数 ID 和一个 `map[uint64]context.CancelFunc`；L61 的 `SetReleaseSink(any)` 四态 switch 收敛成单一签名。
- **风险**：**高，且高于 L12 的单位风险**。134 个调用点跨 4 个包，其中 `internal/client/codex/live/` 是 WebSocket 长连接路径，生命周期 bug 会表现为连接泄漏。
- **建议**：**独立 PR，排在 L12 之后**。如果只想要删减量，L12 的性价比远高于这条——同样是高风险，L12 给你 2,800 行，这条只给 350 行。

---

## YAGNI 组（报告标 L15/L17/L25/L40/L49）— 需拆分，不是一刀切

报告说这 5 项共 -420 行。**实测后必须砍掉一半以上**，因为其中最大的两项是真实的多态扩展点，不是 YAGNI。

### 不要动（删除会破坏架构）

| 条目 | 实测依据 | 结论 |
|---|---|---|
| L15 `ProviderExecutor` | **20 个实现**（`internal/runtime/executor/` 各 provider + `internal/pluginhost/adapters_executors.go` + 2 个 examples） | **保留**。这是 provider 插件化的核心接口，删了插件体系就断了 |
| L15 `Selector` | **4 个实现**（`RoundRobinSelector`、`WeightedRoundRobinSelector`、`FillFirstSelector`、`SessionAffinitySelector`），全在 `selector.go`，且被 `scheduler.go` 通过 `selectorStrategy` 分派 | **保留**。报告「1 个实现」的说法不成立 |
| L15 `PluginScheduler` | `PickAuth` 1 个生产实现，但这是 **pluginhost 的外部插件契约** | **保留**。实现数少是因为它面向外部插件，不是因为没用 |

### 可以做（约 -260 行）

| 条目 | 位置 | 实测 | 方案 | 预估 |
|---|---|---|---|---|
| L15 部分 | `conductor.go:92` `Hook` + `NoopHook` | `OnResult` 生产实现**只有 `NoopHook` 一个**（`conductor.go:111`） | 删 `Hook`/`NoopHook`，回调点直接内联 | **-60** |
| L25 | `conductor_home.go` | `HomeDispatchBundle` + 3 个 legacy shim（`SetHomeExecutionRegistry`/`ClearHomeExecutionRegistry`/`HomeExecutionRegistry`） | 删 legacy trio，只留 bundle | **-50** |
| L17 | `api_key_model_capabilities.go` | `apiKeyModelCapabilityTable` 与 `apiKeyModelAliasTable` 并行维护，在同一个 `rebuildAPIKeyModelAliasFromRuntimeConfig` switch 里构建 | 合成单个 `apiKeyModelRoutes` 结构，一 map 一 build | **-120** |
| L40 | `oauth_model_alias.go` | `map[channel]map[alias]entry` + rebuild-on-Set + atomic.Publish + preserve-suffix 路径 | 改成单个 `map[string]string`（`channel.alias → upstream`），注册时解析 | **-100** |
| L49 | `proxy.go:20` | `Mode` 四态枚举 | 见 P0 第 3 项，与 L47/L48 一起做 | **-30** |

> 注意：上面 L17 和 L40 都与模型别名解析有关，**建议合并成一个 PR**，避免两次改同一套 switch。

---

## 标准库改造组（L50 / L64 / L67）— 共 -65 行，风险极低，可随时夹带

| 条目 | 位置 | 当前实现 | 替代方案 | 预估 |
|---|---|---|---|---|
| L50 | `internal/logging/diagnostic.go:13` | 5 个正则做日志脱敏（`accessTokenExpiredLogPattern`、`sensitiveLogAssignmentPattern`、`authorizationLogPattern`、`urlUserinfoLogPattern`、`diagnosticStatusPattern`） | 合并成一个：`regexp.MustCompile("(?i)(token\|secret\|password\|authorization\|key\|credential)[ =:]+[^\\s,;]+")` | **-25** |
| L64 | `internal/runtime/executor/claude_thinking_replay.go:33` | `sha256.Sum256` + `hex.EncodeToString` 只为生成一个「model family」标签 | `auth.ID + ":" + baseModel`。冲突无所谓，cache miss 只是重算一次 | **-10** |
| L67 | `internal/runtime/executor/openai_responses_signature.go:55` | `gjson.GetBytes` 探测 + 3 行手写重写循环 `startRebuild` | 一次 `sjson.DeleteBytes(body, "input.3.id")` | **-30** |

**L50 有个前提必须先确认**：合并正则会**扩大**脱敏范围（`key` 这个 pattern 会误伤含 "key" 的正常字段）。这是安全向改动，方向是「多脱敏一点」而非「少脱敏」，通常是安全的，但要人工过一遍脱敏后的日志样本，确认没有把关键信息抹成不可用。

---

## 汇总与执行顺序

### 删减量核算（两条路径，二选一）

| 分组 | 条目 | 保守 | 激进 |
|---|---|---|---|
| P0 | L7（实测 -52 ✅）, L6（实测 -165 ✅）, L26（**❌ 否决，0**）, L48/L47/L49（**❌ 否决，0**） | **-217** | **-217** |
| P1 | L29（实测 -162 ✅）, L28（实测 -35，部分 ✅）, L30（**❌ 否决**）, L38（**❌ 否决**） | **-197** | **-197** |
| YAGNI（可用部分） | L15部分, L25, L17, L40 | -260 | -330 |
| 标准库 | L50, L64, L67 | -65 | -65 |
| 长尾（报告 L1-L5, L9-L11, L13-L14, L16, L18-L24, L27, L31-L37, L39, L41-L46, L51-L58, L60-L63, L65-L66, L68-L75，未逐条核验） | — | -400 | -900 |
| **路径 A 小计（不动 scheduler）** | | **≈ -1,089** | **≈ -1,659** |
| P2 L12 | scheduler.go + 测试，净额 | -2,800 | -2,800 |
| P2 L59 | executionregistry | 0 | -350 |
| **路径 B 小计（含 scheduler）** | | **≈ -3,889** | **≈ -4,809** |

**结论**：报告的「保守 3500 / 激进 5500」大致成立，但**它不是一条连续曲线**。

- 不做 L12：上限 **~2,600 行**。
- 做 L12：直接 **~4,800 ~ 5,800 行**。

所以真正需要拍板的只有一个决策：**L12 做不做**。我的建议是做，但必须按三步走（先基准测试 → 再加适配层 → 最后删壳），并且独立 PR + 独立灰度。

### 推荐执行顺序（每个 PR 一个主题，参考 `git-workflow-and-versioning`）

| PR | 内容 | 预估 | 前置 |
|---|---|---|---|
| 1 | L7 + L8（auto_refresh 去堆） | -240 | 无，**最高性价比，先做** |
| 2 | L6（cds 持久化塌缩） | -180 | 无 |
| 3 | L48 + L47 + L49（代理走标准库） | -190 | 需真实代理冒烟 |
| 4 | L50 + L64 + L67（标准库夹带） | -65 | 无，可并进任意 PR |
| 5 | L26（home error 类型） | -100 | 先 grep 测试依赖 |
| 6 | L29（合并 MixedOnce 孪生） | -180 | PR1-5 稳定 |
| 7 | L28 + L30（重试循环合并 + 拆函数） | -220 | **必须在 PR6 之后** |
| 8 | L38（home_selection 生命周期） | -200 | 先读 `home_concurrency.go` |
| 9 | L17 + L40（模型别名两套表合一） | -220 | 无 |
| 10 | L15部分 + L25（删 Hook / legacy shim） | -110 | 无 |
| 11 | **L12 三步走（含基准测试）** | -2,800 | **PR1-10 全部合入并稳定后** |
| 12 | L59（executionregistry） | -350 | L12 之后，独立评估 |

### 每个 PR 的验证清单

```bash
gofmt -w .                                   # 必须
go build -o cli-proxy-api ./cmd/server        # 必须（AGENTS.md 要求）
go test ./sdk/cliproxy/auth/ -count=1         # 主战场，76 个测试文件 32005 行
go test ./sdk/cliproxy/executionregistry/ ./sdk/proxyutil/ ./internal/registry/ -count=1
go test ./...                                 # 合并前
```

PR 3 额外需要真实代理冒烟（HTTP / SOCKS5 / HTTPS 三种）。
PR 11 额外需要 `BenchmarkPickSingle` 的前后对比数据。

### 关键提醒

1. **`sdk/cliproxy/auth/` 有 76 个测试文件、32005 行测试代码**，是全仓库最重的包。任何改动都要先 `grep` 测试依赖再动手，不要只看生产代码。
2. **不要动 `internal/translator/`**。按 AGENTS.md，独立改动该目录需要先 `gh repo view --json viewerPermission -q .viewerPermission` 确认有写权限，否则要提 issue 而不是直接改。
3. 删减不是目的。**P1 的第 5 项（合并 MixedOnce）和第 7 项（拆 `executeHomeOnce`）即使行数收益为 0 也值得做**——225 行 6 层嵌套的热路径函数，维护成本远高于它的行数。
