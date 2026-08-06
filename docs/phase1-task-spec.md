# Phase 1 · 上闸门 — 任务规范（Task Specification）

> 版本：v1.1（执行粒度已按「合并 3 大 PR」调整） ｜ 日期：2026-08-06 ｜ 基线：`docs/lint-baseline-20260806.txt`（505 个存量问题，逐叶子包全量扫描）
> 本规范将 Phase 1「lint 全量清零 + 吞错门禁 + 覆盖率门禁」拆解为 **9 个任务单元 + 3 个合并 PR**。
> 每个任务按「目标 / 范围 / 动作 / 验收 / 工作量 / 风险 / 依赖」定义，团队可直接认领执行。

---

## 0. 总体说明

### 0.0 执行粒度决策（v1.1，团队负责人确认）
**不按 9 个任务拆 15+ 个 PR，合并为 3 个 PR**，以减少合并/评审/CI 排队开销：

| PR | 内容（任务映射） | 问题数 | 性质 |
|---|---|---|---|
| **PR-1 机械批** | T01 errcheck(2) + T02 格式(17) + T03 unused(58) + T04 govet(101) | ~178 | 全部机械/静态，可批量 |
| **PR-2 语义批** | T05 SA1029(82) + T06 SA5011/1019/1008(30) + T07 其余 SA/S(~100) | ~212 | 含行为级改动，需人工 |
| **PR-3 门禁上线** | T09 门禁 + T08 测试文件(136) | — | 依赖 PR-1、PR-2 全绿 |

**风险控制（大 PR 的配套机制）**：
1. **PR-2 必须按 commit 拆分提交**：SA1029/SA5011 行为级改动**每处（或每组同文件）独立 commit**，commit message 标注 `fix(semantic): <文件> SA1029 <原值→新值>`——评审按 commit 逐个审，出问题可单独回退，不阻塞整个 PR。
2. **PR-1 内 unused 删除前 grep 验证**（防插件动态引用），govet copylocks 处人工确认。
3. **每个 PR 的 diff 统计与验证命令见第 5 节**；PR-2 建议预留 1-2 天评审窗口（行为级改动不能 fast-merge）。
4. 若评审人力不足，PR-2 可进一步拆为 PR-2a（行为级：SA1029+SA5011，约 90 个，逐个 commit）与 PR-2b（机械 SA/S 简化，约 120 个）——数量仍为 4 个 PR，优于 15+。

### 0.1 目标
1. 生产代码存量 lint 问题清零（369 个），达成 lint 全树 0 issue。
2. 吞错门禁上线：errcheck 全量 0 issue 作为 CI 强制门禁。
3. 覆盖率门禁上线：auth / store / pluginhost 阈值 40% 起步，逐周上调。
4. 测试文件问题（136 个）作为低优先级清理项，**门禁豁免测试文件**（lint 命令 `--tests=false`），由 T08 独立跟踪、不阻塞门禁。

### 0.2 执行原则
- **任务 = 所有权与提交单元**：任务不是 PR 单元——PR 分组按 §0.0 的 3 个合并 PR 模型（PR-1 机械批 / PR-2 语义批 / PR-3 门禁），一次可承载多个任务的提交；大 PR 是被接受的执行形态，配合 commit 级拆分评审。
- **红转绿**：每任务以「目标包 lint 0 issue + go build + go test 通过」为完成标志。
- **只改目标问题**：任务范围内不夹带重构/功能改动，保持 diff 最小。
- **改动文件边界**：任务范围按文件划分，**同一文件最多属于一个进行中任务**，避免合并冲突。
- **translator 目录约束**：`internal/translator/` 受 AGENTS.md 规则约束（PR 被 pr-path-guard 拦截）——含 translator 文件的任务需先确认仓库 WRITE 权限，或将 translator 改动随批合并说明。

### 0.3 全局验证命令（每个任务提交前必须执行）
```bash
gofmt -w .                              # 格式（Go 改动后必须）
go build -o test-output ./cmd/server && rm -f test-output   # 编译（必须）
go test ./<受影响包>/...                # 受影响包回归
# lint 局部验证（Windows 上逐叶子包扫描，见 go-error-hygiene 技能）
```

### 0.4 提交规范
- 注释仅英文；错误包装用 `%w`；变量遮蔽用 `errXxx` 后缀；禁止 `log.Fatal`。
- Commit message 建议：`lint(pkg): fix <linter> <简述>`。

---

## 1. 任务总览

| ID | 任务 | 数量 | 优先级 | 依赖 | 负责人 | 状态 |
|---|---|---|---|---|---|---|
| P1-T01 | errcheck 生产清零 | 2 | 🔴 P0 | 无 | 待认领 | 待执行 |
| P1-T02 | 格式批（gofmt+goimports+ineffassign） | 17 | 🔴 P0 | 无 | 待认领 | 待执行 |
| P1-T03 | unused 生产清零（4 子任务） | 58 | 🟠 P1 | 无 | 待认领 | 待执行 |
| P1-T04 | govet 生产清零（2 子任务） | 101 | 🟠 P1 | 无 | 待认领 | 待执行 |
| P1-T05 | SA1029 time.Duration 修复（3 子任务） | 82 | 🔴 P0（真实 bug 类） | 无 | 待认领 | 待执行 |
| P1-T06 | SA5011/SA1019/SA1008 语义修复 | 30 | 🟠 P1 | 无 | 待认领 | 待执行 |
| P1-T07 | staticcheck 其余 SA/S 系列 | ~100 | 🟡 P2 | 无 | 待认领 | 待执行 |
| P1-T08 | 测试文件 lint 清理（可选） | 136 | ⚪ P3 | 无 | 待认领 | 待执行 |
| P1-T09 | 门禁上线（lint 全量 + errcheck 0 + 覆盖率 40%） | — | 🔴 P0 | T01-T07 | 待认领 | 待执行 |

> 并行策略：T01-T08 文件边界互不重叠，**可全部并行**；T09 必须在 T01-T07 全部绿后执行。

---

## 2. 任务详情

### P1-T01 · errcheck 生产清零（2 个）🔴 P0
- **范围**：`internal/wsrelay/session.go:153,155`（`conn.SetReadDeadline` 返回值未检查）
- **动作**：
  ```go
  if err := conn.SetReadDeadline(deadline); err != nil {
      // log + return/continue as appropriate; deadline loss on a relay session
      // should not be silent
  }
  ```
  参考仓库既有风格（`errXxx` 后缀命名），按该文件现有错误处理惯例实现。
- **验收**：`golangci-lint run -c <全量配置> ./internal/wsrelay` → 0 issue；`go test ./internal/wsrelay/...` 通过。
- **工作量**：0.5h ｜ **风险**：低（仅 2 处，语义明确）

### P1-T02 · 格式批（17 个）🔴 P0
- **范围**（11 个文件）：
  - gofmt 3：`internal/runtime/executor/codex_websockets_execute.go`、`codex_websockets_stream.go`、`internal/translator/antigravity/claude/signature_validation.go`
  - goimports 4：`internal/translator/claude/openai/responses/claude_openai-responses_request.go`、`internal/translator/gemini/openai/responses/signature_carrier.go`、`sdk/api/handlers/handlers_routing.go`、`sdk/cliproxy/service_lifecycle.go`(×2)
  - ineffassign 10：`sdk/api/handlers/openai/openai_images_handlers.go`(×2)、`openai_responses_handlers.go`、`sdk/auth/claude.go`、`sdk/auth/codex.go` 等
- **动作**：`gofmt -w` + `goimports -w` 直接修复；ineffassign 逐个删除无效赋值（读上下文确认赋值确实未被使用）。
- **注意**：含 3 个 translator 文件——按 0.2 节 translator 约束处理。
- **验收**：对应包 lint 0 issue；`go build` 通过。
- **工作量**：2h ｜ **风险**：低（机械）

### P1-T03 · unused 生产清零（58 个）🟠 P1
- **子任务（按文件归属拆分，避免冲突）**：
  - T03a：`internal/tui/styles.go`(6) + `dashboard.go`(3) —— 9
  - T03b：`sdk/cliproxy/auth/conductor_models.go`(5) + `sdk/cliproxy/service_models.go`(1) + `service_config.go`(1) —— 7
  - T03c：`internal/pluginstore/install.go`(5) + `internal/homeplugins/sync.go`(2) —— 7
  - T03d：其余 35 个（`internal/runtime/executor/antigravity_*`、`internal/logging/request_logger_format.go`、`sdk/cliproxy/home_plugins.go` 等）
- **动作**：删除未使用的私有函数/变量/字段。**删除前必须 grep 确认无反射调用/动态引用**（尤其 `pluginhost`、`homeplugins` 等插件相关代码）。
- **验收**：对应子任务包 lint 0 issue；`go build` + 受影响包测试通过。
- **工作量**：T03a-d 各 2-3h ｜ **风险**：中（误删动态引用会运行时 panic，验收必须含测试）

### P1-T04 · govet 生产清零（101 个）🟠 P1
- **分布**：`sdk/api/handlers` 系 92 个（handlers_interceptors.go 22 / handlers_context.go 22 / handlers_execution.go 13 / handlers_stream.go 11 / model_execution.go 10 / handlers.go 9 / handlers_routing.go 4 / handlers_errors.go 1）+ `internal/pluginhost/loader_windows.go` 8 + `internal/logging/request_logger_body_source.go` 1。
- **子任务**：
  - T04a：`sdk/api/handlers`（92 个）——最大单包，建议 2 人按文件认领
  - T04b：`internal/pluginhost` + `internal/logging`（9 个）
- **动作**：多为 printf 格式串不匹配、struct tag 问题、copylocks 等。**逐条看 message 再改**；涉及 `%v/%d` 与实参类型不匹配时，优先修正格式串（不动调用语义）。
- **验收**：对应包 lint 0 issue；`go vet ./<包>/...` 干净。
- **工作量**：T04a 4-6h、T04b 1h ｜ **风险**：低-中（format 类机械，copylocks 需人工确认）

### P1-T05 · SA1029 time.Duration 修复（82 个）🔴 P0 — 真实 bug 类
- **含义**：`time.Duration` 与整型单位混用（如把毫秒/秒当纳秒传），可能导致超时/sleep 失效或缩短 1000 倍。
- **分布**（按包拆分）：
  - T05a：`sdk/cliproxy/auth`（conductor_execution.go 3 / conductor_home.go 2 / conductor_home_execution.go 1 / conductor_stream.go 1 等）—— 7+
  - T05b：`sdk/api/handlers/openai`（openai_responses_websocket_forward.go 3 / openai_responses_websocket.go 2 / openai_responses_handlers.go 1）—— 6+
  - T05c：`internal/runtime/executor`（antigravity_executor_credits.go 2 / gemini_executor.go / gemini_vertex_executor.go / antigravity_executor_tokens.go 等）+ 其余包
- **动作**：**每处必须人工判断语义**（是超时/间隔/截止时间？期望单位是什么？），再改为正确的 `time.Duration` 常量或 `x * time.Millisecond`。**禁止机械替换**——SA1029 往往有真实意图。
- **验收**：对应包 lint 0 issue + **相关包测试全绿**（尤其含超时/重试逻辑的包）；建议为涉及超时的修复补行为断言测试。
- **工作量**：T05a-c 各 3-4h ｜ **风险**：**高**（改错单位会改变运行时行为，必须逐条评审 + 测试护航）

### P1-T06 · SA5011 / SA1019 / SA1008 语义修复（30 个）🟠 P1
- **范围**：SA5011 nil 解引用风险 7 个（`internal/api/server_options.go`×2、`sdk/cliproxy/auth/conductor_stream.go`、`sdk/api/handlers/*` 5 个）；SA1019 废弃 API 12 个；SA1008 11 个。
- **动作**：SA5011 加 nil 守卫或调整控制流（**最高优先**）；SA1019 按官方迁移建议替换（`grep -rn` 确认替换后行为等价）；SA1008 修格式化。
- **验收**：对应包 lint 0 issue + 测试全绿。
- **工作量**：4-6h ｜ **风险**：中（SA5011 涉及控制流改动）

### P1-T07 · staticcheck 其余 SA/S 系列（~100 个）🟡 P2
- **范围**：除 T05/T06 外的 staticcheck（S 系列简化建议、SA9003 空分支、SA4000 重复表达式、SA6006/SA2001 等）。
- **动作**：S 系列多为机械简化（`strings.Replace` → `ReplaceAll` 等），可批量；SA9003/SA4000 人工确认。
- **验收**：对应包 lint 0 issue。
- **工作量**：1-2 天 ｜ **风险**：低-中

### P1-T08 · 测试文件 lint 清理（136 个）⚪ P3（可选）
- **范围**：全部 `_test.go` 中的问题（errcheck 24 / staticcheck / unused 等）。
- **动作**：测试中的 errcheck（如 mock 调用返回值）用 `require.NoError`/`assert.NoError` 或显式处理；unused 删除。
- **验收**：测试文件 lint 0 issue；测试全绿。
- **工作量**：1-2 天 ｜ **风险**：低（不触生产路径）

### P1-T09 · 门禁上线 🔴 P0（依赖 T01-T07 全部绿）
- **动作**：
  1. `.golangci.yml`：删除 `new: true`（或改为 `new: false`），全树强制；确认 `issues.exclusions` 保留（generated/examples）。
  2. `pr-test-build.yml`：新增 `lint` job——逐叶子包扫描（Windows 经验）或 CI 上直接 `golangci-lint run --tests=false ./...`（Linux 无 Windows bug，可直接全量；**`--tests=false` 豁免测试文件**，测试问题由 T08 跟踪）；`errcheck` 0 issue 硬门禁。
  3. 覆盖率门禁：`go test -coverprofile`，对 `internal/auth`、`internal/store`、`internal/pluginhost` 设 40% 阈值（先 `P1-T09a` 出基线报告，再 `P1-T09b` 加阈值 gate）。
  4. 可加 `go test -race ./internal/... ./sdk/...`（与 Phase 2 衔接）。
- **验收**：新 PR 上 CI 全绿（build + test + lint + race；lint 覆盖**生产代码**，测试文件豁免）；吞错扫描 = 构建失败。
- **工作量**：1 天 ｜ **风险**：中（门禁过严会阻塞团队，建议 T09 上线后给 1 周缓冲期）

---

## 3. 依赖关系

```
T01 ─┐
T02 ─┤
T03 ─┼──► T09（门禁上线）
T04 ─┤
T05 ─┤
T06 ─┼──► T08（可选，独立）
T07 ─┘
```

## 4. 并行建议（按包认领，文件零重叠）

| 认领人 | 包 | 任务 |
|---|---|---|
| A | sdk/api/handlers | T04a、T06 该包部分 |
| B | sdk/cliproxy/auth | T05a、T06 该包部分 |
| C | internal/runtime/executor | T05c、T03d 该包部分 |
| D | 其他包（tui/pluginstore/logging/wsrelay...） | T01、T02、T03a/c、T04b |

> 建议每任务 1 名负责人 + 1 名评审（评审轮换制，见方案文档第 7 节）。

## 5. 验收（PR 级 + 总闸门）

### 5.1 PR 级验收（每个 PR 合并前必须）

```bash
# PR-1（机械批）
gofmt -l <改动的包路径> | head          # 应为空
go build -o test-output ./cmd/server && rm -f test-output
go test ./sdk/api/handlers/... ./internal/runtime/executor/... ./sdk/cliproxy/...  # 受影响包
# lint 局部验证（逐叶子包扫描，见 go-error-hygiene 技能）→ 目标文件 0 issue

# PR-2（语义批）—— 额外要求
go test -race ./<行为级改动包>/...      # 超时/重试相关改动必须 race 跑
# 每个 SA1029 commit 附「原值→新值」说明，评审逐 commit 通过

# PR-3（门禁上线）
golangci-lint run ./...                 # Linux CI 全量，0 issue（含测试文件）
go test -cover ./internal/auth/... ./internal/store/... ./internal/pluginhost/...  # 覆盖率基线 ≥ 40%
```

### 5.2 验收总闸门（PR-3 完成后执行）

```bash
gofmt -l . | head                  # 应为空
go build -o /dev/null ./cmd/server # 编译通过
go vet ./internal/... ./sdk/...    # vet 干净
go test ./...                      # 全量测试通过
go test -race ./internal/... ./sdk/...   # race 干净
golangci-lint run ./...            # 0 issue（Linux CI 直接全量）
```

## 6. 风险与注意事项

1. **SA1029（T05）是行为级改动**：改错单位 = 超时/重试语义变化，可能引发线上回归。每处必须写明「原值/新值/理由」，评审重点。
2. **unused（T03）删除风险**：插件体系（pluginhost/homeplugins/pluginstore）可能有运行时动态引用，删除前 `grep -rn` 全仓确认。
3. **translator 目录**：T02 含 3 个 translator 文件，先按 AGENTS.md 确认仓库权限（`gh repo view --json viewerPermission`），无权限则提 issue 附代码，不自改。
4. **测试文件 136 个问题（T08）**：不阻塞门禁，但建议在 T09 上线前完成，否则 `tests: true` 配置下 lint 会扫到测试文件报错。
5. **Windows vs Linux CI 差异**：本地 Windows 用逐叶子包扫描；CI（Ubuntu）可直接 `./...`，两者结果等价。
6. **T09 缓冲期**：门禁上线后给团队 1 周缓冲（允许 merge 但记录 lint 违规），第 2 周起硬强制。

---

*本规范配套：`docs/engineering-upgrade-plan.md`（路线图）、`docs/lint-baseline-20260806.txt`（原始问题清单）、`go-error-hygiene` 技能（扫描/修复 SOP）。*
