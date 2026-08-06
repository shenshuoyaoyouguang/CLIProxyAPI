# CLIProxyAPI 团队技术提升方案

> 基于对代码库 **1,077 个 Go 文件**（599 源文件 + 478 测试文件）的全面审计，结合 CI / lint / 测试工程化现状，输出可落地的三阶段提升路线图、Code Review 规范与团队培养计划。
>
> 日期：2026-08-06 ｜ 定位：诊断 → 止血 → 加固 → 沉淀

---

## 1. 现状诊断（数据说话）

| 指标 | 数值 | 评价 |
|---|---|---|
| 生产源文件 | 599 | — |
| 测试文件 | 478 | 源:测 ≈ 1:0.8，总体健康 |
| CI 中运行的测试 | **0** | ⚠️ CI 只跑 `go build`（pr-test-build.yml） |
| lint 检查范围 | 仅新增（`new: true`） | ⚠️ 存量问题永远不进门禁 |
| log.Fatal / panic 越权 / TODO | 0 | ✅ 纪律优秀 |

**三个工程化断层：**
1. **CI 不跑测试** —— 回归只能靠人肉，478 个测试文件形同「离线资产」。
2. **lint 只查新增** —— 技术债合法化，旧代码永不修复。
3. **关键包测试失衡** —— auth 测试比 0.35、store 0.50，`objectstore.go` / `postgresstore.go` 零测试。

> 💡 结论：这是一支「写码纪律很好、但工程防线没有闭环」的团队。提升空间不在「会不会写」，而在「质量如何被强制」。

---

## 2. 审计关键发现（已逐条人工核实）

### 🔴 Critical —— 数据完整性风险

| # | 位置 | 问题 |
|---|---|---|
| C1/C2 | `sdk/cliproxy/auth/conductor_lifecycle.go:98,151`、`conductor_cooldown.go:910,976` | `_ = m.persist(ctx, auth)` 吞掉凭证持久化错误。落盘失败仍对外宣告成功，API key/refresh token 可能丢失且零日志；cooldown 状态丢失会绕过冷却限制。 |

### 🟠 High —— 日志安全与审计完整性

| # | 位置 | 问题 |
|---|---|---|
| H4 | `internal/runtime/executor/helps/logging_helpers.go:729` | `SummarizeErrorBody` 非 HTML/JSON 错误时 `return string(body)` 原样返回，调用点写入 Debugf 日志——上游错误体可能回显请求内容（含 prompt）。 |
| H2/H3 | `internal/api/middleware/response_writer.go:347-356,371-374,211` | 审计日志 `WriteAPIRequest/WriteAPIResponse/WriteStatus` 全部吞错，审计数据可能静默丢失。 |
| H1 | `internal/config/config_load.go:113` | 管理密钥哈希写回吞错，无日志不可观测。 |

### 🟡 Medium —— 可靠性隐患

| # | 位置 | 问题 |
|---|---|---|
| M4 | `internal/logging/request_logger_writer.go:231-241` | 请求/响应 body 明文落盘未脱敏（header/query 已脱敏）。 |
| M1/M2/M3 | `server_routes.go:280`、`config_reload.go:113`、`service_auth.go:367` | 路由解析、热重载解析、cooldown 仓库配置吞错，行为隐式降级。 |
| M5-M8 | `openai_auth.go:150` 等 | 注释掉的 token 调试日志残留（死代码）；生产代码中文注释违反 AGENTS.md。 |

### ⚪ Low

| # | 位置 | 问题 |
|---|---|---|
| L1-L5 | `reconcile.go:20`、`gitstore.go:1802`、`auth_files_fields.go:97`、`media.go:325-399` | 死参数、git repack / token 删除 / session.Close 吞错（多为资源清理，可接受但建议留 Debug 日志）。 |

---

## 3. 团队做得好的地方（保持并写进培训教材）

1. **错误命名规范**：全库一致 `errXxx` 方法后缀，未发现变量遮蔽 bug。
2. **并发设计成熟**：RoundRobinSelector 以 Mutex 保护、注释标注锁顺序、goroutine 均有生命周期协调。
3. **敏感信息防护意识**：query/header 专门脱敏、1 MiB body 上限 + 解压炸弹防护、token 调试日志全部注释化。
4. **工程纪律**：`log.Fatal` 为零、TODO/FIXME 为零、`//nolint` 仅 2 处带理由；lint 样本包 0 issues。
5. **测试质量整体高**：t.Skip 全部带理由，无外网依赖，多个包测试密度超源码。

---

## 4. 三阶段提升路线图

### Phase 0（本周）· 止血 — ✅ 已执行（2026-08-06）
- 修复 C1/C2：`m.persist()` 错误改为 `log.WithError(err).Error(...)`，内存态不回滚（conductor_lifecycle.go / conductor_cooldown.go 共 4 处）。
- 修复 H4：`SummarizeErrorBody` 新增 `sanitizeBodySummary`（控制字符剥离 + UTF-8 修复 + 512B 限长），27 个调用点零改动，新增 5 个测试。
- 修复 H2/H3：response_writer.go 7 处流式审计日志吞错降级 Debug。
- 修复 H1：config_load.go 管理密钥写回吞错 → Warn 日志。
- **CI 接入 `go test ./...`**：pr-test-build.yml 新增独立 test job。
- ✅ 验证：gofmt / go build / 4 个受影响包测试全绿 / diff --check 干净（7 文件 +143/-15）。
- ⚠️ 全量 `go test ./...` 有 2 个失败：均已验证为**既有慢/脆测试**（TestDeletePlugin* 单跑 0.34s 通过；TestEnsureRepositoryReconciles* 单跑近 10 分钟），非本次改动回归。

### Phase 1（第 2-4 周）· 上闸门 — 摸底已完成，待执行
**存量摸底（2026-08-06，逐叶子包全量扫描，原始数据见 `docs/lint-baseline-20260806.txt`）**：

| 维度 | 数据 |
|---|---|
| 存量问题总数 | **505**（生产代码 369 / 测试文件 136） |
| staticcheck | 295（生产 195）——其中 **SA1029 time.Duration 误用 82 个**、SA1019 废弃 API 12、SA1008 11、SA5011 潜在 nil 解引用 7 |
| govet | 108（生产 101） |
| unused | 59（生产 58） |
| errcheck | 26（**生产仅 2**：wsrelay/session.go:153,155 SetReadDeadline；其余 24 在测试） |
| ineffassign / goimports / gofmt | 10 / 4 / 3 |
| 问题最集中包 | sdk/api/handlers 115、internal/runtime/executor 100、sdk/cliproxy/auth 30 |

**执行计划（分 3 批，每批独立 PR，红转绿为完成标志）**：
1. **批 1（机械修复）**：gofmt + goimports + ineffassign（17 个）+ errcheck 生产 2 个（wsrelay）→ 改 `.golangci.yml` 全量强制。
2. **批 2（静态分析）**：unused 59 个（删死代码）+ govet 101 个（多为 printf 格式/结构体标签，机械）。
3. **批 3（语义修复，需人工）**：staticcheck 295 个——先修 **SA1029 82 个（time.Duration 单位错误，可能是真实超时/sleep bug）** 与 SA5011 7 个（nil 解引用风险），再批量处理 SA 系列。
4. **吞错扫描进 CI**：errcheck 已接近清零，直接以全量 errcheck 0 issue 为门禁。
5. **覆盖率门禁**：auth/store/pluginhost 先设 40% 阈值，逐周上调。
- ✅ 验收：lint 全树 0 issue；吞错扫描进 CI；覆盖率进 PR 评论。

### Phase 2（第 2 个月）· 补测试
- 为 `objectstore.go`、`postgresstore.go` 补存储后端测试（当前零测试，运维高危区）。
- auth（0.35）、pluginhost（0.58）测试密度提升至 ≥ 0.8。
- 巨型文件拆分：54 个超 800 行文件（最大 2043 行）按职责拆 200-400 行模块。
- ✅ 验收：关键包覆盖率 ≥ 60%；测试比例 ≥ 0.8。

### Phase 3（第 3 个月）· 沉淀
- Code Review 规范固化为强制 checklist（见下节）。
- 「代码博物馆」bad→good 对照沉淀为培训材料，新人 onboarding 复用。
- 知识分享体系上线。
- ✅ 验收：评审规范执行率 100%；连续一月无新增 Critical/High 吞错。

---

## 5. Code Review 强制 Checklist（评审人逐项勾选）

- [ ] **吞错**：禁止 `_ = f()`（error 返回）。至少记日志；关键路径传播 + `%w` 包装。
- [ ] **日志安全**：禁止日志出现 token / apiKey / Authorization / 原始 body；用脱敏 + 限长。
- [ ] **裸 return**：跨 3 层调用链的 `return err` 必须带上下文，错误信息可独立定位。
- [ ] **竞态**：包级共享状态禁止并发写；Mutex/atomic；注释标注锁顺序。
- [ ] **panic / Fatal**：库代码禁 panic；禁 log.Fatal。
- [ ] **goroutine 泄漏**：每个 `go func()` 必须回答「谁来停它」：context / closed channel / WaitGroup。
- [ ] **重试**：必须有退避 + 上限 + 幂等性论证；禁无界重试。
- [ ] **测试**：必须有断言；禁 t.Sleep 时间依赖；禁真实网络。
- [ ] **死代码**：注释掉的调试日志、未用参数、中文注释——合并前清理。
- [ ] **巨型文件**：单文件 > 800 行打回重构。
- [ ] **超时规则**：上游连接建立后禁设网络超时（见 AGENTS.md）；仅凭证获取阶段允许。
- [ ] **覆盖缺口**：新增逻辑必须带测试；改动包测试比 < 0.5 的必须补。

---

## 6. CI 门禁设计（从「构建检查」升级为「质量防线」）

| 门禁 | 当前 | 目标 |
|---|---|---|
| 构建 | ✅ 已有 | ✅ 不变 |
| 测试 | ❌ 未运行 | ✅ `go test ./...`（分片并行） |
| 静态检查 | ⚠️ 仅本地/仅新增 | ✅ golangci-lint 全树 0 issue |
| 覆盖率 | ❌ 无 | ✅ PR 评论 diff 覆盖率，阈值逐周上调 |
| 吞错扫描 | ❌ 无 | ✅ errcheck + 自定义规则 = 构建失败 |
| 竞态检测 | ❌ 无 | ✅ `go test -race` 常驻 |

渐进式：Phase 0 加测试 → Phase 1 加 lint 全树 + errcheck → Phase 2 加覆盖率 + race。每步「红转绿」为完成标志。

---

## 7. 团队培养计划

### 主题工作坊（每两周一期，45-60 分钟）
| 期次 | 主题 | 核心内容 |
|---|---|---|
| 1 | Go 错误处理哲学 | 吞错代价、%w 包装链、errXxx 惯例实战 |
| 2 | 并发模式与竞态 | Mutex/atomic/context、本库 RoundRobinSelector 解剖、-race 实战 |
| 3 | 安全日志与脱敏 | token 泄露事故复盘、脱敏函数库、body 落盘策略 |
| 4 | 测试金字塔与表驱动 | 本库 478 个测试复盘、fake clock 替代 t.Sleep、store 包攻坚 |

### 日常机制
- **评审轮换**：每 PR ≥ 1 名非作者评审，每两周轮换「评审主理人」。
- **结对攻坚**：存储后端补测、巨型文件拆分 2 人结对认领。
- **代码博物馆**：8 类 bad smell 的 before/after 对照卡片。
- **月度复盘**：CI 红绿率、吞错新增数、覆盖率趋势，用数据替代感觉。

---

## 8. 度量指标（每项设 owner，月度复盘）

| 指标 | 目标 | 说明 |
|---|---|---|
| 吞错点 | 0 | Phase 0 清零；新增 = 评审事故，月度审计回归 |
| CI 测试全绿率 | 100% | Phase 0 接入后常驻 |
| 关键包覆盖率 | ≥ 60% | auth / store / pluginhost，Phase 2 达成，逐月 +5% |
| lint 全树 issue | 0 | Phase 1 达成后永久全量强制 |

---

*所有审计发现均附 `文件:行号`，可直接转 issue 追踪器。*
