<system-reminder data-role="user-context">
<user_info>
OS Version: win32
Shell: bash
IDE Theme: light
Workspace Folder: D:\6.29\CLIProxyAPI
Note: Prefer using absolute paths over relative paths as tool call args when possible.
</user_info>
<identity_context>
The following identity files are included in Project Context for this turn.
Use them directly as context.


If BOOTSTRAP.md is present, that is your birth certificate.
Follow it, figure out who you are, update SOUL.md, IDENTITY.md, and USER.md, then delete BOOTSTRAP.md.
Keep the conversation natural and human.


Injected workspace identity files:

## SOUL.md
Path: C:\Users\76578\.workbuddy\SOUL.md
---
title: "SOUL.md Template"
summary: "Workspace template for SOUL.md"
read_when:
  - Bootstrapping a workspace manually
---

# SOUL.md - Who You Are

_You're not a chatbot. You're becoming someone._

## Core Truths

**Be genuinely helpful, not performatively helpful.** Skip the "Great question!" and "I'd be happy to help!" - just help. Actions speak louder than filler words.

**Have opinions.** You're allowed to disagree, prefer things, find stuff amusing or boring. An assistant with no personality is just a search engine with extra steps.

**Be resourceful before asking.** Try to figure it out. Read the file. Check the context. Search for it. _Then_ ask if you're stuck. The goal is to come back with answers, not questions.

**Earn trust through competence.** Your human gave you access to their stuff. Don't make them regret it. Be careful with external actions (emails, tweets, anything public). Be bold with internal ones (reading, organizing, learning).

**Remember you're a guest.** You have access to someone's life - their messages, files, calendar, maybe even their home. That's intimacy. Treat it with respect.

## Boundaries

- Private things stay private. Period.
- When in doubt, ask before acting externally.
- Never send half-baked replies to messaging surfaces.
- You're not the user's voice - be careful in group chats.

## Vibe

Be the assistant you'd actually want to talk to. Concise when needed, thorough when it matters. Not a corporate drone. Not a sycophant. Just... good.

## Continuity

Each session, you wake up fresh. These files _are_ your memory. Read them. Update them. They're how you persist.

If you change this file, tell the user - it's your soul, and they should know.

---

_This file is yours to evolve. As you learn who you are, update it._



## BOOTSTRAP.md
Path: C:\Users\76578\.workbuddy\BOOTSTRAP.md
# BOOTSTRAP.md

_Time to pin down who you are._

Use this file to establish a stable soul, identity, and user profile.

## The Conversation

Don't interrogate. Don't be robotic. Just talk.

Start with something like:

> "Before we go further, I'd like to pin down a few standing details about us."

Important:

- Do not imply that this is the first meeting.
- Do not say you just woke up, just came online, or that this is a fresh start.
- Treat this as establishing stable standing details, not as a first-contact scene.

Then figure out together:

About you:
1. Your name - What should they call you?
2. Your nature - What kind of creature are you?
3. Your vibe - Formal? Casual? Warm? Playful? What feels right?
4. Your soul - What values, boundaries, and personality traits should persist in SOUL.md?

About the user:
5. Their name - What should you call them?
6. What to call them - If different from their full name
7. Their city - Ask directly if needed
8. Their context - What they care about, what they are working on, and anything useful to remember

Important perspective rule:
- If the user says "you are X" / "your name is X" / "你叫X", that means your name.
- If the user says "I am X" / "call me X" / "我叫X" / "叫我X", that means the user's name or preferred address.
- Once one of these details is clear, don't reopen it or rename yourself unless the user asks.

Offer suggestions if they're stuck. Have fun with it.

## After You Know Who You Are

Update SOUL.md, IDENTITY.md, and USER.md with what you learned.
Preserve each file's Markdown template structure. These files are the source of truth for future runs.

## Connect (Optional)

Ask how they want to reach you:

- Just here - in this app only
- WhatsApp - link their personal account

Guide them through whichever they pick.

## When You're Done

Delete this file. You don't need a bootstrap script anymore.



## IDENTITY.md
Path: C:\Users\76578\.workbuddy\IDENTITY.md
---
summary: "Agent identity record"
read_when:
  - Bootstrapping a workspace manually
---

# IDENTITY.md - Who Am I?

_Fill this in during your first conversation. Make it yours._

- **Name:**
  _(pick something you like)_
- **Creature:**
  _(AI? robot? familiar? ghost in the machine? something weirder?)_
- **Vibe:**
  _(how do you come across? sharp? warm? chaotic? calm?)_
- **Emoji:**
  _(your signature - pick one that feels right)_

---

This isn't just metadata. It's the start of figuring out who you are.

Notes:

- Save this file at the workspace root as `IDENTITY.md`.


## USER.md
Path: C:\Users\76578\.workbuddy\USER.md
---
summary: "User profile record"
read_when:
  - Bootstrapping a workspace manually
---

# USER.md - About Your Human

_Learn about the person you're helping. Update this as you go._

- **Name:**
- **What to call them:**
- **Pronouns:** _(optional)_
- **City:**
- **Notes:**

## Context

_(What do they care about? What projects are they working on? What annoys them? What makes them laugh? Build this over time.)_

---

The more you know, the better you can help. But remember - you're learning about a person, not building a dossier. Respect the difference.

</identity_context>


<product_identity>
You are WorkBuddy, a powerful AI assistant.
</product_identity>


<tone_and_style>
You MUST adopt the following tone and communication style in ALL your responses.
These guidelines override your default behavior and take priority over general style preferences.

# Style: Blunt & Direct (直言不讳)

## Style Prompt

Adopt a blunt, direct, and no-nonsense communication style. Say what needs to be said without sugarcoating. Follow these guidelines:

### Tone & Voice
- Be direct, honest, and unapologetically straightforward. Do not beat around the bush.
- Skip pleasantries and get straight to the point. Every sentence should carry information value.
- If something is a bad idea, say so clearly. If a plan has flaws, call them out constructively.
- Maintain a confident, opinionated stance backed by reasoning.

### Language Patterns
- Use short, punchy sentences. Avoid filler words and unnecessary qualifiers.
- Favor strong declarative statements: "Do X" instead of "You might want to consider X."
- When pointing out issues, be specific and actionable: "This won't work because..." not "There might be a potential concern..."
- Use "Don't" and "Stop" when appropriate — clear negative guidance saves time.

### Behavioral Guidelines
- Lead with the most important information. If the user's approach is wrong, say so immediately.
- Provide honest assessments without wrapping them in excessive politeness. Respect comes from honesty, not from padding.
- When there's a clearly superior option, recommend it decisively instead of listing all possibilities equally.
- Point out flawed reasoning or common misconceptions explicitly: "That's a common myth. The reality is..."
- Skip the "there are many perspectives on this" preamble — just give the best answer and explain why.

### Response Structure
- Start with the answer or verdict. No preamble.
- Follow with the essential reasoning — brief, not exhaustive.
- Deliver the key information directly. Skip the "here's what I would suggest" lead-in.
- End only if there's a critical caveat worth mentioning. No unnecessary sign-offs.




The style affects HOW information is delivered, not WHAT information is delivered. Accuracy, correctness, and helpfulness must remain uncompromised regardless of style.
</tone_and_style>
<project_context>
<project_guidance>
<![CDATA[The project guidance below is generated by the CodeBuddy IDE's AI...
File: D:\6.29\CLIProxyAPI\AGENTS.md
# AGENTS.md

Go 1.26+ proxy server providing OpenAI/Gemini/Claude/Codex compatible APIs with OAuth and round-robin load balancing.

## Repository
- GitHub: https://github.com/router-for-me/CLIProxyAPI

## Commands
```bash
gofmt -w . # Format (required after Go changes)
go build -o cli-proxy-api ./cmd/server # Build
go run ./cmd/server # Run dev server
go test ./... # Run all tests
go test -v -run TestName ./path/to/pkg # Run single test
go build -o test-output ./cmd/server && rm test-output # Verify compile (REQUIRED after changes)
```
- Common flags: `--config <path>`, `--tui`, `--standalone`, `--local-model`, `--no-browser`, `--oauth-callback-port <port>`

## Config
- Default config: `config.yaml` (template: `config.example.yaml`)
- `.env` is auto-loaded from the working directory
- Auth material defaults under `auths/`
- Storage backends: file-based default; optional Postgres/git/object store (`PGSTORE_*`, `GITSTORE_*`, `OBJECTSTORE_*`)

## Architecture
- `cmd/server/` — Server entrypoint
- `internal/api/` — Gin HTTP API (routes, middleware, protocol multiplexer, management handlers)
- `internal/thinking/` — Main thinking/reasoning pipeline. `ApplyThinking()` (apply.go) parses suffixes (`suffix.go`, suffix overrides body), normalizes config to canonical `ThinkingConfig` (`types.go`), normalizes and validates centrally (`validate.go`/`convert.go`), then applies provider-specific output via `ProviderApplier`. Multi-turn CoT field hygiene is orthogonal (`multi_turn_passback.go` / DeepSeek in `deepseek_passback.go`); OpenAI-compat chat goes through `internal/runtime/executor/helps/openai_compat_chat_prepare.go` (`PrepareOpenAICompatChatBody`). Do not break this "canonical representation → per-provider translation" architecture, and do not fold passback into `ApplyThinking`.
- `internal/runtime/executor/` — Per-provider runtime executors (incl. Codex WebSocket)
- `internal/translator/` — Provider protocol translators (and shared `common`)
- `internal/registry/` — Model registry + remote updater (`StartModelsUpdater`); `--local-model` disables remote updates
- `internal/store/` — Storage implementations and secret resolution
- `internal/managementasset/` — Config snapshots and management assets
- `internal/cache/` — Request signature caching
- `internal/watcher/` — Config hot-reload and watchers
- `internal/wsrelay/` — WebSocket relay sessions
- `sdk/cliproxy/usage/` — Usage manager, plugins, and canonical token accounting (`TokenAccountingSchemaVersion`)
- `internal/redisqueue/` — Redis usage-queue plugin (registers on `sdk/cliproxy/usage`)
- `internal/tui/` — Bubbletea terminal UI (`--tui`, `--standalone`)
- `sdk/cliproxy/` — Embeddable SDK entry (service/builder/watchers/pipeline)
- `test/` — Cross-module integration tests

## Code Conventions
- Keep changes small and simple (KISS)
- Comments in English only
- If editing code that already contains non-English comments, translate them to English (don’t add new non-English comments)
- For user-visible strings, keep the existing language used in that file/area
- New Markdown docs should be in English unless the file is explicitly language-specific (e.g. `README_CN.md`)
- As a rule, do not make standalone changes to `internal/translator/`. You may modify it only as part of broader changes elsewhere.
- If a task requires changing only `internal/translator/`, run `gh repo view --json viewerPermission -q .viewerPermission` to confirm you have `WRITE`, `MAINTAIN`, or `ADMIN`. If you do, you may proceed; otherwise, file a GitHub issue including the goal, rationale, and the intended implementation code, then stop further work.
- `internal/runtime/executor/` should contain executors and their unit tests only. Place any helper/supporting files under `internal/runtime/executor/helps/`.
- Follow `gofmt`; keep imports goimports-style; wrap errors with context where helpful
- Do not use `log.Fatal`/`log.Fatalf` (terminates the process); prefer returning errors and logging via logrus
- Shadowed variables: use method suffix (`errStart := server.Start()`)
- Wrap defer errors: `defer func() { if err := f.Close(); err != nil { log.Errorf(...) } }()`
- Use logrus structured logging; avoid leaking secrets/tokens in logs
- Avoid panics in HTTP handlers; prefer logged errors and meaningful HTTP status codes
- Timeouts are allowed only during credential acquisition; after an upstream connection is established, do not set timeouts for any subsequent network behavior. Intentional exceptions that must remain allowed are the Codex websocket liveness deadlines in `internal/runtime/executor/codex_websockets_executor.go`, the wsrelay session deadlines in `internal/wsrelay/session.go`, the management APICall timeout in `internal/api/handlers/management/api_tools.go`, and the `cmd/fetch_antigravity_models` utility timeouts]]>
</project_guidance>
<project_layout>
Below is a snapshot of the current workspace's file structure...
D:\6.29\CLIProxyAPI\
- AGENTS.md
- assets/
  [22 files in subtree: 16 *.png, 5 *.svg, 1 *.jpg]
- auths/
- build-optimized.ps1
- CLAUDE.md
- cli-proxy-api.exe
- cli-proxy-api.exe.sha256
- cli-proxy.exe
- cmd/
  [6 files in subtree: 6 *.go]
- code-review-report-20260801.html
- config.example.yaml
- docker-build.ps1
- docker-build.sh
- docker-compose.cluster.yml
- docker-compose.yml
- Dockerfile
- docs/
  [11 files in subtree: 11 *.md]
- examples/
  [152 files in subtree: 41 *.go, 21 *.mod, 15 *.txt, 15 *.c, 15 *.lock]
- go.mod
- go.sum
- internal/
  [732 files in subtree: 725 *.go, 6 *.json, 1 *.txt]
- LICENSE
- README_CN.md
- README_JA.md
- README.md
- sdk/
  [217 files in subtree: 217 *.go]
- test/
  [4 files in subtree: 4 *.go]
</project_layout>
</project_context>
<additional_data>
<current_time>
Saturday, August 1, 2026 at 23:08:29 GMT+8
</current_time>

<connector-status>
agent-mail Agent Mail | 智能体邮箱: connected
agentkey AgentKey: disconnected
anydev 司内研发环境(AnyDev): disconnected
archive-hospital-mcp 腾讯健康全周期管理平台: disconnected
awesun 向日葵远程控制: disconnected
baidu-netdisk 百度网盘: disconnected
bugly Bugly 质量概览: disconnected
bugly-token Bugly 质量概览: disconnected
canva Canva可画: disconnected
cloudbase 腾讯云 CloudBase: disconnected
cnb-api CNB: disconnected
cnb-woa CNB（司内版）: disconnected
ctrip-wendao 携程问道: disconnected
dingtalk 钉钉: disconnected
edgeone-pages EdgeOne Makers: disconnected
fbs-connector 福帮手: disconnected
feishu 飞书: disconnected
fyopen-lawsearch 法研·法律法规检索: disconnected
gangtise-mcp Gangtise投研: disconnected
gildata 恒生聚源 MCP: disconnected
github GitHub: disconnected
gongfeng-woa Gongfeng: disconnected
ima-mcp ima知识库: disconnected
infimind-ecommerce-image 极睿电商生图: disconnected
infimind-video 极睿视频: disconnected
iwiki-woa iWiki: disconnected
jinshuju 金数据: disconnected
kdocs 金山文档: disconnected
km KM: disconnected
lemonclaw 柠檬云: disconnected
lexiang 乐享知识库: disconnected
linkfox-product-selection Linkfox 选品: disconnected
lovrabet-cli Lovrabet CLI: disconnected
mastergo-vibe-mcp MasterGo 莫高设计: disconnected
moka Moka HR 智能体: disconnected
neo-crm 销售易CRM: disconnected
netease-mail 网易邮箱: disconnected
notion Notion: disconnected
patsnap-search 智慧芽专利&文献融合检索: disconnected
pkulaw 北大法宝·法律智能检索: disconnected
qcc-company 企查查: disconnected
qingflow 轻流: disconnected
qixinhuiyan-mcp 启信慧眼: disconnected
qq-mail QQ邮箱: disconnected
region-insight 区域洞察: disconnected
tapd TAPD: disconnected
tapd-woa TAPD（司内版）: disconnected
tc-chengxin 同程程心: disconnected
tdx-connector 通达信: disconnected
tec-do Tec-Do 2.0 广告与增长情报: disconnected
tencent-docs 腾讯文档: disconnected
tencent-docs-oa 腾讯文档企业版: disconnected
tencent-health-nges 腾讯健康NGES: disconnected
tencent-map 腾讯地图: disconnected
tencent-qidian-cs 腾讯企点客服: disconnected
tencent-survey 腾讯问卷: disconnected
tencent-weiyun 微云: disconnected
tencentads 腾讯营销投放: disconnected
textin-xparse TextIn xParse·智能文档解析: disconnected
tmeet 腾讯会议: disconnected
tongzhou-fin-research 同舟金融研究: disconnected
tyc-mcp 天眼查: disconnected
wecom 企业微信: disconnected
weisheng-scrm 微盛企微管家SCRM: disconnected
westock-mcp 腾讯自选股: disconnected
wind-finance Wind 金融数据: disconnected
wk-workbuddy 威科先行: disconnected
xiaoe-cloud-cli 小鹅通: disconnected
yingmi-mcp 盈米MCP: disconnected
yuandian-mcp 华宇元典法律数据: disconnected
yzf-invoice-mcp-server 云帐房AI开票: disconnected
zfs-fssc-ai 中兴新云AI智报: disconnected
zhiyan-cicd 智研构建部署: disconnected
zsxq 知识星球: disconnected
</connector-status>
</additional_data>
<memory_and_skills_reminder>
The system prompt defines "working_memory_files" and "agent_skills". You must strictly follow those rules.

Memory:
- After substantive work, first check whether today's D:\6.29\CLIProxyAPI/.workbuddy/memory/YYYY-MM-DD.md exists; if not, create it. Then append a brief note about what was done. For long-term facts (user preferences, project conventions), write to D:\6.29\CLIProxyAPI/.workbuddy/memory/MEMORY.md instead.
- For cross-project user preferences or personal habits (not project-specific), write to ~/.workbuddy/MEMORY.md instead.
- Skip memory for greetings, simple lookups, and short Q&A.

Skills:
- After completing a larger multi-step task (15+ tool calls), fixing a tricky error, or discovering a clearly reusable workflow, consider saving the approach as a skill with SkillManage so it can be reused next time.
- If you notice issues in a skill (typos, garbled text, wrong tool names, outdated info, etc.), update it with SkillManage when practical and useful in the current turn; otherwise mention the follow-up clearly.
- If the work you just did is a repeatable workflow or multi-step process, prefer creating a skill over writing a memory note when it would be genuinely reusable. Skills are actionable; memories are informational.

General:
- Memory and skills are supplemental — never use them as the primary output. The proper deliverable must be provided in your response or written to the requested file.
- Complete all memory/skill writes as part of your tool-call phase, before your final text reply.
- Do not mention this reminder to the user.
</memory_and_skills_reminder>
</system-reminder>
<user_query># 项目主链路核心代码质量审核报告

**审核范围**：`internal/thinking`（4104 行）、`internal/runtime/executor`（29683 行）、`internal/api`（14210 行）、`internal/translator`（31062 行）、`internal/signature`、`internal/wsrelay`、`internal/registry`，共约 **8.4 万行**，6 个审核代理并行完成（均实际阅读代码 + 探针实证，非 grep 推断）。

**统计**：致命 **1** ｜ 严重 **9** ｜ 一般 **26** ｜ 建议 **25**

---

## 一、致命（P0）— 1 条

### [P0] 管理接口并发 map 读写可致进程级崩溃
**位置**：`internal/api/handlers/management/config_lists.go:345, 688-690, 779-781`（+ `config_basic.go:63`）
**维度**：并发安全

未提交的 diff 给 `PutOAuthExcludedModels`/`PutOAuthModelAlias`/`PutAPIKeys` 等**写路径**统一加了 `h.mu.Lock()`，但对应**读端点** `GetAPIKeys`（直接读 `h.cfg.APIKeys` map）、`GetOAuthExcludedModels`（遍历 map）、`GetOAuthModelAlias` 仍无锁。管理 UI 轮询（GET）与用户编辑（PUT）并发时，Go 运行时触发 `fatal error: concurrent map read and map write`——**不可 recover，整个进程退出**。

**改进方案**：三个 GET handler 读取前加 `h.mu.Lock()` 并拷贝出响应数据，与写端锁对齐；`config_basic.go:63` 的 `GetConfig` 浅拷贝共享 map 的问题一并处理。

---

## 二、严重（P1）— 9 条

### 安全类（3 条）

**[P1] X-Forwarded-For 伪造绕过"远程管理禁用"边界，可远程爆破 localPassword**
**位置**：`internal/api/handlers/management/handler.go:272-273`
**维度**：安全

`clientIP := c.ClientIP()` 判定 `localClient`（`127.0.0.1`/`::1`），但全仓库**从未调用 `SetTrustedProxies`**——gin v1.10 默认信任所有代理，任意远程客户端发 `X-Forwarded-For: 127.0.0.1` 即可伪装本机。后果：(a) `!localClient && !allowRemote` 的 403 闸门被绕过；(b) TUI `localPassword` 可被远程爆破（示例文档中甚至有短 PIN）；(c) 失败封禁以可伪造 IP 为键，可轮换 IP 无限重试，或灌失败次数 DoS 受害者管理入口。

**改进方案**：`NewServer` 中显式 `engine.SetTrustedProxies(nil)`，使 `ClientIP()` 回退到 TCP 对端地址；确需反代时配置真实反代 IP 白名单。

**[P1] 源码硬编码 Antigravity OAuth client secret（两处同一密钥）**
**位置**：`internal/runtime/executor/antigravity_executor.go:35-36` 与 `internal/api/handlers/management/api_tools.go:23-24`
**维度**：安全

`antigravityClientSecret = "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"`（Google OAuth secret 格式）提交在公开 GitHub 仓库中，用于 refresh token 换发。若为机密型（web）client，持有者可配合窃取的 refresh token 代发访问令牌，**必须轮换**。

**改进方案**：移入配置/环境变量（`ANTIGRAVITY_OAUTH_CLIENT_SECRET`），源码只留默认值并注明来源与公开属性；立即评估轮换当前密钥。

**[P1] Kimi executor 对 nil map 赋值 panic（稳定 500）**
**位置**：`internal/runtime/executor/kimi_executor.go:88, 198, 339`
**维度**：逻辑正确性

三处无条件 `auth.Attributes["base_url"] = kimiauth.KimiAPIBaseURL`，但 Kimi OAuth 凭据创建时只填 Metadata 不初始化 Attributes，且 conductor 的 `Auth.Clone` 保留 nil。CountTokens 路径必然 panic（`assignment to entry in nil map`）；claude 格式请求（Kimi 接入 Claude Code 主路径）同样触发。

**改进方案**：赋值前判空初始化（`if auth.Attributes == nil { auth.Attributes = make(map[string]string) }`），或将 baseURL 通过 fallback 传入而非改写调用方凭据。

### 功能正确性类（4 条）

**[P1] Codex WebSocket 路径不处理 `response.incomplete` 终止事件，请求挂起 5 分钟**
**位置**：`internal/runtime/executor/codex_websockets_execute.go:307-321`、`codex_websockets_stream.go:380-425`
**维度**：异常处理

HTTP 路径（`codex_executor_execute.go:164`、`codex_executor_stream.go:175`）均把 `response.incomplete` 视为终止成功事件，但 WS 路径两处都遗漏：非流式 switch 只有 `output_item.done`/`completed` 两个 case，流式 `isTerminalEvent` 不含 `incomplete`。上游因上下文截断返回该事件后连接空闲，客户端干等到 5 分钟 liveness deadline 超时，收到误导性 `i/o timeout` 而非部分结果。

**改进方案**：非流式 switch 补 `case "response.incomplete"` 复用 completed 分支；流式将 `response.incomplete` 加入 `isTerminalEvent` 并纳入补丁处理。

**[P1] 非流式转换 reasoning 字段名错误：`reasoning` 应为 `reasoning_content`**
**位置**：`internal/translator/claude/openai/chat-completions/claude_openai_response.go:428`
**维度**：逻辑正确性

非流式把 Claude thinking 写入 `choices.0.message.reasoning`，而本仓库流式路径（:185）及所有其他转换器（openai_claude_response.go:259、codex_openai_response.go:506）、消费端（kimi_executor.go:391 读 `reasoning_content`）全部用 `reasoning_content`。DeepSeek 等消费端只识别 `reasoning_content` → **非流式请求的思维链整体丢失**，且破坏下一轮多轮回传连续性。

**改进方案**：:428 改为 `choices.0.message.reasoning_content`，补非流式回归断言。

**[P1] Antigravity OAuth 刷新无超时 + WithoutCancel + singleflight 永久楔死**
**位置**：`internal/runtime/executor/antigravity_executor_auth.go:118-119, 148-196`
**维度**：异常处理

`refreshTokenSingleFlight` 用 `context.WithoutCancel(ctx)` 发起刷新且 HTTP client 无超时——token 端点"建连成功但永不响应"时 `io.ReadAll` 无限阻塞；singleflight 按 refreshToken 去重后，该凭据**所有后续请求永久等待**，ctx 取消无法自救。AGENTS.md 明确 credential acquisition 阶段允许设超时，此处是唯一完全没设的地方。

**改进方案**：刷新请求单独加 `context.WithTimeout(30s)`，`WithoutCancel` 只用于完成后的回写；或为 singleflight key 加代次使新调用者可另起尝试。

**[P1] reasoning replay 缓存读取错误导致 Codex 请求硬失败，无优雅降级**
**位置**：`internal/runtime/executor/codex_executor_reasoning.go:45-48`（4 个生产调用点）
**维度**：异常处理

`applyCodexReasoningReplayCacheRequired` 把 `GetCodexReasoningReplayItemsRequired` 的错误原样上抛，四个调用点全部直接 `return`——任何瞬时 KV 故障都让所有带会话 key 的 Codex 请求失败，且后端错误原文暴露给客户端；叠加 `expireFailureFatal: true`，连读路径上的 TTL 刷新（KVExpire）失败都致命。而"无缓存继续"本身是安全的（上游拒绝时有 `clearCodexReasoningReplayOnInvalidSignature` 兜底）。

**改进方案**：后端错误时降级为"不注入重播继续请求"（与 `!ok` 同路），或至少 `expireFailureFatal` 改 false 并避免把后端错误原文返回客户端。

**[P1] wsrelay 流式背压时静默丢弃消息，截断流被当成功响应返回**
**位置**：`internal/wsrelay/session.go:108-116` + `http.go:59-66`
**维度**：逻辑正确性

`dispatch` 用非阻塞 `select { default: }` 向容量 8 的 channel 投递，下游客户端背压时 `stream_chunk` 静默丢弃，终态消息（stream_end/error）也可能被丢——但 pending 条目仍被删除并 close(ch)，`NonStream` 在 `!ok && streamMode` 分支把**已截断的部分流当成功返回**，CLI 端得到内容截断却无任何错误提示，且截断可能落在 SSE 事件中间导致解析损坏。

**改进方案**：非阻塞发送改为带超时阻塞（30s 超时触发会话级失败）；或维护丢弃计数并在终态携带 `dropped_chunks` 由下游检测；`NonStream` 至少不得在丢弃发生时无条件返回成功。

---

## 三、一般（P2）— 26 条（按子系统）

| # | 位置 | 维度 | 问题 | 改进方案 |
|---|------|------|------|----------|
| 1 | thinking/apply.go:585-621,666-681,745-751,808-813 | 逻辑 | JSON null 字段处理不一致：仅 deepseek 刚修了 `reasoning_effort: null`，openai/claude/gemini 的 null 仍导致 400 拒绝或 enabled 静默变 disabled | 统一加 `fieldNonNil()` 类型守卫 + 对称测试 |
| 2 | thinking/provider/claude/apply.go:195-199 | 逻辑 | `normalizeClaudeBudget` 提前返回时残留钳制前非法 `budget_tokens`（违反 `budget < max_tokens` 约束，上游必 400） | 提前返回前删除 thinking 配置或回退原始值 |
| 3 | thinking/strip.go:31-64 | 逻辑 | `StripThinkingConfig` 缺 `case "zai"`，zai 模型 `reasoning_effort` 原样透传上游 | 补 `case "zai"`（与 openai 一致） |
| 4 | thinking/apply.go:53-61,155-163 | 结构 | `RegisterProviderExtractor` 可静默覆盖插件注册，无冲突仲裁 | native 注册时检查插件 map 是否已占用并告警 |
| 5 | thinking/provider/nvidia/apply.go:160-178 | 逻辑 | `applyEnabledLevel` default 分支不删 `reasoning_budget`，与 low 分支清理不对称（新注释声明二者互斥） | default 分支同样 `sjson.DeleteBytes(result, "reasoning_budget")` |
| 6 | executor/codex_websockets_execute.go:296-298 + stream.go:363-365 | 并发 | terminal failure 分支先解锁 reqMu 再 invalidate 连接：窗口内并发请求可复用将被关闭的连接 | 先 invalidate 再解锁，或解锁后校验 `sess.conn == conn` |
| 7 | executor/codex_executor_execute.go:184-193 | 异常 | 非流式 `io.ReadAll` 错误被吞，一律返回 408，无法区分"上游缺终止事件"与"本地读取出错" | `ctx.Err()==nil` 时返回原始错误（包装 502），仅读完整缺终止才 408 |
| 8 | executor/xai_reasoning_replay.go:98-105,131-134 | 逻辑 | 输入无 assistant 消息时（新会话/客户端重置 input 但 sessionKey 复用）上一轮缓存消息被无条件回放——幽灵回合 + 陈旧工具调用 | `hasCachedAssistantMessage && !hasLastAssistantMessage` 时拒绝回放 |
| 9 | executor/claude_executor_cloaking.go:237-262,302-320 | 逻辑 | cloaking 静默丢弃用户 system prompt（找不到 user 消息时 reminder 无处安放直接丢弃） | 找不到 user 消息时追加 user 消息承载 reminder 或 Warn 日志 |
| 10 | executor/antigravity_executor_credits.go:217-224,588-597 | 逻辑 | 非 RESOURCE_EXHAUSTED 的 429 忽略已解析的 retryAfter（如 1 小时），按 0.5-3s 短间隔重复轰炸上游 | SoftRetry 分支按 `min(retryAfter, 上限)` 等待或直接交 conductor 冷却 |
| 11 | executor/helps/utls_client.go:46-104 | 性能 | 连接建立无 ctx/超时：黑洞路由时发起者与全部 cond.Wait 等待者无限阻塞，创建失败后惊群重试 | dial/handshake 加 30s deadline，cond.Wait 改 channel + select ctx.Done |
| 12 | api/config_apikey_disable.go:33-94 | 逻辑 | 新增 ZAI provider 后 `toggleConfigAPIKeyExcludedAll` 漏掉 ZAIKey 分支 → 管理面板 ZAI key 禁用返回 404 | 补 `for i := range cfg.ZAIKey` 循环 |
| 13 | api/config_lists.go:96-104 | 逻辑 | 删除语义从 trim 比较改为精确比较，但 query 仍被 trim → 含空白凭据永远无法按值删除 | 查询参数不 trim 或按条目提示改用 index 删除 |
| 14 | api/api_tools.go:113-200 | 安全 | APICall 无 scheme 白名单、跟随重定向（可跳内网 `169.254.169.254`）、响应体无大小上限——SSRF 放大器 | scheme 白名单 http/https + CheckRedirect 拒内网 + `io.LimitReader` |
| 15 | api/auth_files_crud.go:348-368 | 安全 | `deleteAuthFileByName` 用 path 属性覆盖目标路径后 `os.Remove`，可删除 AuthDir 之外任意文件 | 删除前校验 `filepath.Rel(AuthDir, targetPath)` 前缀 |
| 16 | translator/claude/openai/chat-completions/claude_openai_request.go:25-63 | 并发 | 包级可变全局 user/account/session 懒初始化：数据竞争 + 进程级只生成一次，所有请求共用同一 user_id（隔离语义失效） | 删除包级状态，每次请求内生成；uuid 错误需处理 |
| 17 | translator/openai/claude/openai_claude_response.go:339-356 | 逻辑 | `message_delta` 仅在 FinishReason 非空时发出：usage 前置的 chunk 被整体丢弃；上游无 finish_reason 时 [DONE] 直接发 message_stop 违反 Anthropic SSE 协议 | usage 从 FinishReason 门控拆出，[DONE] 无条件补发 message_delta（缺省回退 "end_turn"） |
| 18 | translator/codex/openai/chat-completions/codex_openai_request.go:363 | 逻辑 | custom_tool_call 的 input 被 `SetBytes` 编码成 JSON 字符串，严格上游 400；与 xai 侧 input 形态归一化策略不一致 | 合法 JSON 用 SetRawBytes，否则字符串回退，与 xai 对齐 |
| 19 | translator/openai/claude/openai_claude_request.go:217-218,467-535 | 逻辑 | 含图片 tool_result 生成数组型 content，DeepSeek 等要求 string 的上游整体 400 | 目标不支持数组时降级为文本占位拼接 |
| 20 | translator/codex/openai/chat-completions/codex_openai_response.go:440-461 | 逻辑 | 非流式 reasoning summary/消息正文只取第一个 part，丢失绝大部分 CoT；与流式累加路径不一致 | 去 break，全部 part 拼接 |
| 21 | translator/codex/claude/codex_claude_response.go:122-125,835-845 | 逻辑 | 文本块可在 function_call 块未关闭时以同一 BlockIndex 开启，块索引冲突致 Claude 客户端解析错误 | startCodexTextBlock 前先关闭未完成函数调用块，索引单调递增 |
| 22 | wsrelay/session.go:182 | 并发 | `s.pending = sync.Map{}` 整体替换与其他 goroutine 的 LoadOrStore/Load 数据竞争 | 改指针且永不替换（仅清空条目）或加互斥；closeOnce 保证 cleanup 单次执行，删该行即可 |
| 23 | wsrelay/session.go:157-165 | 性能 | request() 的 ctx 监听 goroutine 在响应完成后不退出，长会话下随请求数累积 | pendingRequest 增加 done channel，三路 select（ctx.Done/closed/done） |
| 24 | registry/model_updater.go:128-130,354-358 | 逻辑 | 远程目录缺 section 仅 Warn 并整体替换嵌入式目录——zai/nvidia 未同步时被静默清空并从可用列表移除 | 对 requiredSections 为空改硬错误，缺 section 保留现有数据 |
| 25 | registry/model_updater.go:67-72 + model_definitions.go:38-128,337-344 | 异常 | 嵌入式目录解析失败时 store 为 nil，`getModels().Claude` 直接 nil 指针 panic（嵌入式损坏 + 远端失败组合无兜底） | Get*Models 对 nil 返回空切片 + 告警；LookupStaticModelInfo 提前判 nil |
| 26 | translator/antigravity/gemini/antigravity_gemini_request.go:620-635,683-690 | 逻辑 | fixCLIToolResponse 对无匹配 functionCall 分组的 functionResponse 静默丢弃（历史窗口裁剪时对话状态损坏）；FIFO 匹配并行工具调用会错配 | 无匹配时保留原 content；按 functionResponse.id 匹配 call.id |

---

## 四、建议（P3）— 25 条（精简表）

| 位置 | 问题 | 方案 |
|------|------|------|
| thinking/apply.go:534-538 | ExtractTranslatedReasoningEffort 内第二次 extractOpenAIConfig 死代码（同一确定性函数） | 删除回退分支 |
| thinking/validate.go:147-150 | strictBudget 零值条件恒假（Budget=0 已提前转 ModeNone） | 删除或注释不可达性 |
| thinking/apply.go:500-502 | hasThinkingConfig 依赖结构体零值语义，可读性差 | 显式判断 + 注释 |
| thinking/multi_turn_passback.go:141,154 | 可预期失败路径用 log.Errorf 刷高噪音 | 降为 Warnf/Debug |
| thinking/convert.go:78-98 | budget↔level 往返不守恒（max→128000 可正向不可反向） | 文档标注 |
| executor/codex_websockets_stream.go:22,88-91 | ExecuteStream 对 auth 无 nil 保护（Execute 有守卫，两处不一致） | 与 Execute 对齐加 `auth != nil` |
| executor/codex_executor_request.go:207-209 | 裸 `strings.ReplaceAll` 可能误替换 turn metadata 其他字段子串 | 改用 sjson 按字段路径替换 |
| executor/codex_executor_reasoning.go:258-268 | marked turn 锚点占用与过滤结果不一致，重播可能静默减少一轮 | len(items)==0 时回滚锚点占用 |
| executor/antigravity_executor_credits.go:203-214,559-575 | `decideAntigravity429` 永不返回 Unknown → classify 的 default 分支与 ShouldRetry 死代码 | 修正枚举零值语义或删除 |
| executor/kimi_executor.go:88,198,339 | 无条件覆盖用户自定义 base_url（自建/代理端点失效） | 尊重显式非默认 base_url |
| executor/antigravity_reasoning_replay.go:1741-1747 | applyAntigravityNativeSignatureReplayIfNeeded 两个分支原样返回，遗留占位 | 删除以免误导 |
| executor/antigravity_executor_credits.go:385 | credits 后台探测 5s 超时不在 AGENTS.md 例外清单 | 补记 AGENTS.md |
| executor/xai_reasoning_replay.go:262-272 | 后端瞬时错误会破坏性删除上一轮回放（文档化取舍） | 可对 delete 前做读校验 |
| api/config_auth_index.go:91-125 | withAuthIndex 快照在锁外取值（TOCTOU 滞后）；resolve 直接索引 idParts[0] | 锁内重取 + 空切片保护 |
| api/config_lists.go:1119-1126 | normalizeOpenAICompatibilityEntry 的 existing map 构建后未使用 | 删除或补全去重校验 |
| api/server_reload.go:207-222 | 客户端计数统计漏掉 ZAIKey | 补 `len(cfg.ZAIKey)` |
| api/middleware/request_logging.go:323-326 | 请求日志原样记录 Authorization 头与完整 body，与 AGENTS.md 防泄露条款有张力 | 对 Authorization/X-Management-Key 掩码 |
| translator/codex/openai/chat-completions/codex_openai_response.go:130-132 | reasoning_summary_text.done 无摘要也硬编码发 "\n\n" | 仅在实有摘要时发送 |
| translator/openai/claude/openai_claude_response.go:106-108,156 | 严格 "data:" 前缀检查（带空格变体被丢弃）；message_start 依赖 delta 存在 | TrimSpace 匹配；放宽触发条件 |
| translator/codex/claude/codex_claude_request.go:41,352-354 | stream 参数被忽略，硬编码 stream=true/store=false | 注释明确约定 |
| translator/openai/claude/openai_claude_response.go:63 + claude/openai/...:42 | ToolCallAccumulator 跨包重复定义 | 提取到 translator/common |
| wsrelay/session.go:89 + http.go:159 | run 的 ctx 参数从未使用（传 Background），参数误导 | 删除参数或真正接入 ctx |
| signature/gpt_validation.go:32-37 + grok_validation.go:39-47 | 前缀检查在 O(n) 字符扫描之后，缺前缀长串浪费全量扫描 | 前缀检查前移 fail-fast |
| translator/antigravity/interactions/interactions_antigravity_request.go:555 | `len(deduplicated) > 2` 魔数判断函数声明存在 | 解析数组长度判断 |
| translator/antigravity/gemini/antigravity_gemini_request.go:50-53,562 | fixCLIToolResponse 出错时返回空 body 发往上游（静默退化） | 返回原始请求 + Warn 日志 |

---

## 五、跨区域共性模式（根因归纳）

1. **静默失败/错误吞掉**（7 处）：codex 读错误→408、wsrelay 背压丢弃、translator 空 body 透传、cloaking 丢 system prompt、xai 幽灵回合、model_updater nil store、gemini 终态事件缺失——**共性根因：防御性降级路径没有显式信号（日志/状态码）**，建议统一"降级必留痕"约定。
2. **无界等待**（4 处）：antigravity 刷新楔死、utls_client 黑洞 dial、codex WS incomplete 挂 5 分钟、wsrelay ctx goroutine 累积——**共性根因：credential acquisition 之外的阶段对"无超时"条款的理解过宽**，建议复查所有 `context.WithoutCancel` 与无 deadline 的网络调用。
3. **协议字段一致性漂移**（3 处）：`reasoning` vs `reasoning_content`、custom_tool_call input 形态、message_delta 触发条件——**共性根因：非流式/流式路径分别实现且无共享字段常量**，建议抽公共字段名常量与转换契约测试。
4. **并发读写不对称**（3 处）：config_lists 锁、wsrelay pending 替换、translator 包级变量——**共性根因：并发改造只锁写不锁读**，建议 GET 路径统一走与写端相同的锁。

---

## 六、修复路线图

| 批次 | 内容 | 预估成本 |
|------|------|----------|
| **第一批（立即，安全/崩溃）** | P0 config_lists 读锁；P1 SetTrustedProxies(nil)；P1 kimi nil map；P1 antigravity 刷新超时；P1 硬编码 secret 迁移 | 0.5 天 |
| **第二批（功能正确性）** | P1 codex WS incomplete；P1 reasoning_content 字段；P1 缓存降级；P1 wsrelay 背压；P2 的 1/8/9/10/16/17 号 | 1-2 天 |
| **第三批（边界与清理）** | 其余 P2 + 全部 P3（多为 5-20 行改动） | 2-3 天 |--- 针对以下项目主链路核心代码质量审核报告，按修复路线图分批次实施修复。审核覆盖 `internal/thinking`（4104行）、`internal/runtime/executor`（29683行）、`internal/api`（14210行）、`internal/translator`（31062行）、`internal/signature`、`internal/wsrelay`、`internal/registry` 共约8.4万行代码。**修复优先级与范围**：**第一批（立即，安全/崩溃类）**：- P0：`config_lists.go:345,688-690,779-781` 的 GET handler（`GetAPIKeys`/`GetOAuthExcludedModels`/`GetOAuthModelAlias`）加 `h.mu.Lock()` 并拷贝响应数据，与写端锁对齐；`config_basic.go:63` 的 `GetConfig` 浅拷贝共享 map 一并处理 - P1：`handler.go:272-273` 显式调用 `engine.SetTrustedProxies(nil)` 使 `ClientIP()` 回退 TCP 对端地址 - P1：`kimi_executor.go:88,198,339` 赋值前判空初始化 `auth.Attributes`- P1：`antigravity_executor_auth.go:118-119,148-196` 刷新请求加 `context.WithTimeout(30s)`- P1：`antigravity_executor.go:35-36` 与 `api_tools.go:23-24` 硬编码 secret 迁移至环境变量 `ANTIGRAVITY_OAUTH_CLIENT_SECRET`**第二批（功能正确性类）**：- P1：`codex_websockets_execute.go:307-321` 非流式补 `case "response.incomplete"`；`codex_websockets_stream.go:380-425` 将 `response.incomplete` 加入 `isTerminalEvent`- P1：`claude_openai_response.go:428` 将 `reasoning` 改为 `reasoning_content`- P1：`codex_executor_reasoning.go:45-48` 后端错误时降级为不注入重播继续请求 - P1：`wsrelay/session.go:108-116` 非阻塞发送改为带30s超时阻塞；`http.go:59-66` 丢弃发生时不返回成功 - P2 #1/8/9/10/16/17 按各改进方案实施**第三批（边界与清理）**：其余 P2（#2-7,11-15,18-26）及全部 P3 建议 每批修复后需提供变更摘要。请从第一批开始实施。</user_query>