# CLIProxyAPI 全面问题排查报告

日期：2026-08-31 ｜ 基准：local main `8d3cfc15`（vs upstream/main `d31b1591`，本地独有 74 commits）

## 排查方法

- 提交历史：2025-07-02 → 2026-08-31，共 3657 commits；重点分析本地 74 个独有提交（2026-08-20~31，226 文件 +6454/−7786 的 AI 批量重构）与近 6 个月 543 个 fix 提交的模式分布
- 实际执行：`go build`（通过）、`go vet ./internal/... ./sdk/...`、全量 `go test`、`go test -race`（cache + sdk/cliproxy/auth 通过）、时钟精度探针实验
- 代码级核查：replay 缓存引擎、OAuth 合并、管理 API 鉴权、pluginhost 流桥、mime 表收缩、未提交工作区改动

---

## A. 已实证问题（可修复）

### A1.【中高】pluginhost HTTP 流桥缺少兜底清理 → 持久泄漏
- **位置**：`internal/pluginhost/http_stream_bridge.go`（整个文件）、`internal/pluginhost/host_callbacks.go:154-181`（`callHostHTTPDoStream`）
- **提交关联**：`760ca355 fix: deterministic cache eviction tie-break and reliable plugin conn cancel`（8-22）说明此区域修过一次，但只修了"cancel 可靠性"，没加兜底
- **问题**：`cancel` 的所有权在 `open()` 后转移给 bridge，仅在 `read()` 被再次调用时才会在流结束/ctx 取消/出错路径上触发 `close()`。若插件拿到 streamID 后**不再调用 StreamRead、也不调 StreamClose**（插件崩溃、逻辑提前 return），则 `cancel` 永不执行：上游 HTTP 连接 + goroutine + `streams` map 条目全部永久保留，进程重启才释放。
- **影响**：插件生态场景下逐渐泄漏连接与内存；`streams` map 无界增长
- **修复建议**：`open()` 时记录创建时间，加一个后台 sweeper 定期 cancel 超时无活跃读的流；或 Host 关闭时 drain 全部流
- **注**：go vet 对该函数的 lostcancel 警告本身是误报（cancel 所有权转移是有意设计），但缺兜底是真问题

### A2.【中】两个 TTFT 测试在 Windows 上必挂（upstream 引入）
- **位置**：`internal/runtime/executor/helps/responses_ttft_helpers_test.go:278`、`usage_helpers_test.go:639`
- **提交关联**：`6a489fa8 fix(auth): prefer errors from upstream attempts`（8-29，upstream）等近期 usage 改动；usage_helpers.go 与本地重构无关
- **实证**：探针实验显示本机连续两次 `time.Now()` 差值 **1000/1000 全为 0**（时钟分辨率 ≈15.6ms）；测试断言 `tokenTTFT > 0` 但 StartResponseTTFT 与 token 事件之间无耗时操作 → 恒为 0
- **产品影响**：极快响应（<15ms，如快速拒绝/本地回退）的 TTFT 记为 0，usage 统计数据质量受损
- **修复建议**：测试里插入 `time.Sleep(50*time.Microsecond)`（实测可测到非 0 值）或注入 mock 时钟；产品侧 TTFT=0 视为可接受

### A3.【中·需决策】未提交改动：Claude MCP alias 由 fail-closed 改为 fail-open
- **位置**：`internal/runtime/executor/claude_executor_request.go:1835-1940`（**工作区未提交**，含配套测试 +183 行）
- **改动内容**：漂移工具名"匹配多个别名/语义后缀多匹配/无唯一匹配"三种场景，从返回 `claudeMCPAliasRestoreError`（拒绝转发）改为 `log.Warnf` + **原样透传漂移名**
- **风险**：透传路径放弃了防注入保障——被 prompt injection 污染的 tool_use 名可能原样到达客户端。新加的 `IsClaudeMCPToolName` 校验只堵了"已知虚拟服务器下非合法 MCP 名"这一条路（该路径仍 fail-closed，正确）
- **修复建议**：三选一——a) 接受现状（可用性优先）并保留审计日志；b) 仅对"模型漂移"高发场景开放透传，其余维持 fail-closed；c) 透传前二次校验名字必须在本次请求声明的工具集合内。决定后提交

### A4.【低】`FileBodySource.WriteTo` 签名不满足 `io.WriterTo`
- **位置**：`internal/logging/request_logger_body_source.go:170`（go vet 报告；upstream `fe4ae498` 7-26 引入）
- **问题**：`WriteTo(w io.Writer) error` 缺少 `(int64, error)` 返回值。当前所有调用点均为直接方法调用，功能正常；但未来任何 `io.Copy(dst, fileBodySource)` 会因接口断言失败而静默走 Read 回退路径
- **修复建议**：改签名 `WriteTo(w io.Writer) (int64, error)`，调用点已全部收敛在 logging 包内，改动面小

### A5.【低】缓存驱逐循环缺防御：`EvictBatch<=0` 时持锁死循环
- **位置**：`internal/cache/replay_engine.go:322`（`enforceFencedReplayLimitsLocked`，本地重构 `af0b646a` 引入）
- **问题**：`for len > Max || bytes > Max` 循环内依赖 `evictOldestReplayEntriesLocked` 减少条目，后者对 `count<=0` 直接 return。当前 Claude/Kimi 常量均为 128，安全；但未来有人改成 0/负数或配置化后即触发持锁死循环，冻结整个 provider
- **修复建议**：循环开头加 `if limits.EvictBatch <= 0 { break }`

### A6.【低中】`purgeExpiredReplayEntries` 单锁内全量扫描
- **位置**：`internal/cache/replay_engine.go:49`
- **问题**：TTL 清理在**一次锁持有内遍历整个 map**。条目数大时（MaxEntries 上万）一次 purge 阻塞该缓存所有并发读写。与 `760ca355` 的确定性驱逐同理，但 purge 是定期全量触发
- **修复建议**：分批 purge（每次锁内最多 N 条，循环外推进游标），或用最小堆维护过期时间

### A7.【中】wsrelay `dispatch` 静默丢弃超量消息
- **位置**：`internal/wsrelay/session.go:107-117`（`dispatch`）
- **问题**：`pendingRequest.ch` 容量 8，投递用 non-blocking `select`：channel 满时**直接丢弃消息，无日志**。流式场景下慢消费者 + 快生产者会丢 chunk，客户端收到残缺响应且无法察觉（StreamEnd 仍会正常到达）
- **修复建议**：丢弃时计数并附在 StreamEnd 里上报，或 ch 满时直接触发 `cleanup`（坏连接早断好过静默丢数据）；至少加 `logWarnf`
- **注**：该包零测试（见 C 节），改动建议连带补并发测试

### A8.【低中】`ws-auth: false` 时 ws 网关完全开放且自动注册凭证
- **位置**：`internal/wsrelay/manager.go:58`（`CheckOrigin` 恒真）、`manager.go:131-155`（握手无鉴权）、`sdk/cliproxy/service_auth.go:217-260`（`wsOnConnected` 自动创建 Active 状态的 aistudio Auth）、`internal/api/server_routes.go:523-533`（`wsAuthEnabled=false` 时跳过鉴权中间件）
- **链路分析**：`ws-auth` **默认 true**（`internal/config/config_load.go:76`），默认配置下 `/v1/ws` 有 API key 鉴权，风险可控。但用户显式配置 `ws-auth: false` 后：任意来源可无限建立 WebSocket 连接，每个连接自动注册成一个 Active 凭证记录（uuid 随机不可抢注，无法劫持既有流量，但**无连接数上限** → 资源耗尽 DoS + 凭证表膨胀）
- **修复建议**：关闭鉴权时强制 localhost-only 或加连接数上限；`wsOnConnected` 自动注册加频控
- **附**：`CheckOrigin` 恒真在鉴权开启时可接受（握手要过 AuthMiddleware），记录备查

### A9.【低】`session.cleanup` 的 map 重置存在理论数据竞争
- **位置**：`internal/wsrelay/session.go:184`（`s.pending = sync.Map{}`）
- **问题**：`cleanup`（closeOnce 保护）里对 `pending` 字段整体赋值，与 `dispatch`/`request` 并发的 `s.pending.Load(...)` 字段读取构成 Go 内存模型意义上的 data race。触发窗口极小（cleanup 后 session 即作废），race detector 难复现，但属未定义行为
- **修复建议**：删掉整行——`Range` 已对全部 entry 调用 `close()`，重置只为 GC；改为不清空或改用 `atomic.Pointer[sync.Map]`

### A10.【低·纵深防御】`deleteAuthFileByName` 的 path 属性分支未复检路径
- **位置**：`internal/api/handlers/management/auth_files_crud.go:355-358`
- **问题**：当删除目标匹配到已注册 auth 时，`targetPath` 直接取 `auth.Attributes["path"]` 并执行 `os.Remove`，无基目录约束。已核实该属性的全部写入点（`watcher/synthesizer/file.go:121`、`sdk/auth/filestore.go`、各存储后端、`pluginhost/auth_provider.go:538`）均为服务端内部来源，**无 API 直写入口，当前不可利用**；但任何未来新增的"导入 auth"功能若未过滤该字段，此分支即成任意文件删除原语
- **修复建议**：删除前校验 `targetPath` 必须位于 `AuthDir` 内（`strings.HasPrefix(filepath.Clean(targetPath), AuthDir+sep)`）

---

## B. 提交历史揭示的系统性风险（背景参考）

### B1. 8 月 fix 比例异常高
| 月 | 总提交 | fix | 占比 |
|---|---|---|---|
| 2026-03 | 328 | 137 | 42% |
| 2026-04 | 250 | 93 | 37% |
| 2026-05 | 198 | 42 | 21% |
| 2026-06 | 306 | 43 | 14% |
| 2026-07 | 350 | 103 | 29% |
| **2026-08** | **417** | **222** | **53%** |

8 月一半以上提交是修复，且叠加了 74 个本地 AI 批量重构提交——churn 高峰期，回归概率显著高于平时。

### B2. Claude 是最大修复热点，与本地重构高危交叉
近 6 个月 fix 分布：**claude 60**、auth 33、xai 30、codex 28、translator 26、openai 19、antigravity 18、gemini 17。Claude 是第二名的近两倍，且全是协议适配细节（reasoning chain 重建 `9b114239`、system blocks `6f8f11a3`、OAuth 工具名 `5b5f428a`、legacy-model 校验 `189776aa`、context_management `8638f28d`、max_tokens 兼容 `3230e370`）。**本地恰好重构了 `claude_executor_request.go`（333 行）且工作区还有未提交改动（见 A3）**——这是全仓库风险最高的一块交叉区，建议重点回归测试 Claude 全协议（chat/completions/responses/interactions × 流式/非流式）。

### B3. 两次 revert 值得追踪
- `a63da8ae` Revert "fix/home-401-refresh-recovery"（7-31）：home 401 刷新恢复功能被整单回退，**回退后未见替代修复**——原始问题（401 后恢复逻辑）可能仍存在
- `3e20d662` Revert "fix(home): allow home config to override default port"（8-28）：8 月末刚回退，注意后续是否复发

### B4. Antigravity replay 是慢性病区
`d2c0c58b` thought signature drift、`e47c4365` tool provenance、`bb7278a1` replay index rebuild 性能、`b7a44152` 大 payload 放大、`3904c40d` propertyNames 嵌套、`a834917e` fallback client version——同一 replay 机制反复打补丁。本地重构把 antigravity interactions 与 gemini 合并进 `interactionscommon`（`070b05ef`），作者已用 golden 测试（`twins_golden_test.go` ×2）钉住 wire 输出，但 `interactionscommon` 包**自身零测试**。

### B5. 大 payload 内存问题曾反复出现
`34364fff Prevent large Gemini payloads from being duplicated during normalization`、`b7a44152 Reduce Antigravity request amplification for large payloads`、`b921b5d0 tighten the in-place byte write guard`、`1737596e Make the payload-reuse guards able to fail`——同一周内连续四个大 payload 相关提交，说明转译层对大请求的拷贝放大曾是现实事故。`replay_engine.go` 的 `cloneItemReplayItems` 每次 lookup 都深拷贝整个 item 列表，同类模式的存留点，建议留意。

---

## C. 测试覆盖空洞（零测试的敏感包）

| 包 | 文件数 | 风险说明 |
|---|---|---|
| `internal/wsrelay` | 4 | WebSocket relay 会话（AGENTS.md 认可的 deadline 例外所在地），并发复杂却零测试 |
| `internal/cmd` | 11 | 命令行入口含 OAuth 登录流程 |
| `internal/translator/interactionscommon` | 1 | 本次重构新合并产物，golden 测试放在调用方包，本体零测试 |
| `sdk/pluginhost` / `sdk/api` / `sdk/config` | 1~2 | SDK 公共层 |

另：mime 回退表从 737 行收缩到 26 条目（`d0255148`/`ec3deb30`），依赖 `mime.TypeByExtension` 兜底。已核对调用点 `internal/translator/common/file_data.go:26` 逻辑正确，作者补了测试；但 Linux 精简容器（无 /etc/mime.types）下的长尾扩展行为建议实测一次。

---

## D. 已排查无问题项

- **管理 API 鉴权**（`internal/api/handlers/management/handler.go:330-391`）：常量时间比较 + bcrypt + IP 失败封禁 + 本地/远程分离 + 空 key 全 404，实现扎实
- **OAuth 回调合并**（`oauthcommon/oauth_server.go`）：state 提取语义与 upstream 一致（CSRF 校验本就由调用方承担），作者补了 442 行 golden 测试
- **路径遍历（auth 文件 CRUD）**：`isUnsafeAuthFileName`（禁分隔符/盘符）+ `filepath.Base` 双重校验覆盖下载/上传/删除入口；OAuth state 有严格白名单校验（`ValidateOAuthState`：禁 `/` `\` `..`、限字符集）；唯一例外见 A10
- **日志脱敏**：请求/响应头经 `util.MaskSensitiveHeaderValue`，错误摘要经 `diagnostic.go` 三层正则脱敏（URL userinfo / token 赋值 / Authorization），代理 URL 经 `proxyutil.Redact`；未发现明文 token 入日志
- **A3 注入语义对照**：upstream（d31b1591）的 `claudeMCPAliasResolver.resolve` 在全部"无法唯一恢复"路径均 fail-closed（返回 `claudeMCPAliasRestoreError`，且该错误类型带 `IsRequestScoped()` 标记不触发凭证冷却）；本地未提交改动改为 fail-open 属**相对 upstream 的安全语义变更**，非 upstream 原意
- **race detector**：`internal/cache/...` + `sdk/cliproxy/auth/...` 全部通过
- **go build**：通过（go1.26.5）
- 无硬编码密钥/默认凭据

---

## E. 工程卫生

1. **本地 main 领先 origin/main 80 个提交未推送**——单点丢失风险，建议尽快 push
2. 全量测试当前红（A2 的两个 Windows 时钟测试），CI 基线不绿会掩盖后续真回归，建议优先处理
3. 本次并行深度审查因模型用量限流（429，2026-09-01 03:46 重置）中断，安全审查只完成了快扫（鉴权/密钥/常量时间比较）；限流重置后可对 A3 的安全语义、wsrelay 并发、路径遍历做一轮完整深查

## 修复优先级建议

1. **A3**（需要决策，阻塞未提交改动的落地）
2. **A2**（让测试基线回绿）
3. **A1**（泄漏，改动小收益大）
4. **A7**（静默丢数据，改动小）
5. **A8**（条件触发的 DoS 敞口）
6. **A4/A5/A6/A9/A10**（低风险顺手修）
7. push 本地 80 个提交

## 附：深查轮补充说明（2026-08-31 第二轮）

本轮（限流重置后自查，未用并行 Agent）完成：路径遍历全链核查、密钥泄漏日志扫描、wsrelay 逐行并发审查、A3 注入语义 upstream 对照。新增 A7~A10 四项，D 节新增五项"已排除"。至此排查覆盖：本地 74 提交重构区（缓存引擎/OAuth 合并/mime/translator 抽取——golden 测试兜底）、并发（race + 人工审查）、安全（鉴权/遍历/泄漏/注入）、历史模式（fix 分布/revert/热点）。未覆盖：`internal/client/codex/live`（WebRTC 媒体面）、`internal/tui`、上游依赖 CVE 扫描（govulncheck 未跑）。
