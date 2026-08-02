---
alwaysApply: true
scene: git_message
---

# Git 提交信息规则

本项目遵循 **Conventional Commits** 规范。生成提交信息时必须遵守以下规则。

## 一、头部格式（必填）

```
<type>(<scope>): <subject>
```

- `type` 和 `scope` 全小写
- `subject` 祈使句，首字母小写（专有名词除外），结尾不加句号
- 整行建议 ≤ 72 字符

### 1.1 type 取值

| type | 含义 | 示例 |
|------|------|------|
| `feat` | 新功能 | `feat(kimi): implement native thinking object extraction` |
| `fix` | Bug 修复 | `fix(xai): normalize image refs with special JSON keys` |
| `perf` | 性能优化 | `perf(executor): avoid O(n^2) rebuilds when sanitizing reasoning encrypted_content` |
| `refactor` | 重构（无行为变化） | `refactor(xai): remove unused assistant content equal helpers` |
| `build` | 构建系统/脚本 | `build: add build-optimized.ps1 helper script` |
| `chore` | 杂项维护 | `chore(gitignore): ignore .omc/ .omo/ .reasonix/ tooling metadata` |
| `docs` | 文档 | `docs(readme): add Grok Search MCP translations` |
| `ci` | CI 配置 | `ci: ...` |
| `test` | 测试补充/修复 | `test(executor): add characterization tests for stream truncation` |

### 1.2 scope 取值

优先使用模块名作为 scope。本项目高频 scope：

- **provider 相关**：`xai`、`codex`、`kimi`、`claude`、`gemini`、`antigravity`、`interactions`
- **架构层**：`executor`、`translator`、`handlers`、`auth`、`config`、`registry`、`pluginstore`、`gitstore`
- **横切关注点**：`stream`、`usage`、`logging`、`release`、`ui`、`tests`
- **文件/配置**：`gitignore`、`readme`

跨模块改动用功能名（如 `stream`、`auth`），单模块改动用模块名。无明确归属时可省略 `scope`（保留括号前的冒号）。

## 二、标题（subject）风格

- **祈使句**：用 "update"、"add"、"remove"、"fix"、"implement"，不用 "updated"、"adds"
- **首字母小写**：`fix(xai): update compact request handling` ✅
- **专有名词保留大写**：`feat(registry): remote-refresh Codex client model catalog` ✅
- **不加句号**：`fix(executor): ensure top_p is removed` ✅
- **简洁精准**：一行说清"做了什么"，不解释"为什么"（为什么放正文）
- **语言**：英文为主；若仅触及中文模块文案或文档可使用中文

## 三、正文（body，可选但推荐）

当改动涉及多个文件、有非显然的决策、或修复了 issue 时，必须写正文。

### 3.1 格式

- 与标题之间用**一个空行**分隔
- 使用 `- ` 开头的列表项，每项描述一个改动点
- 每行 ≤ 80 字符，超长换行缩进 2 空格对齐
- 引用代码标识符用反引号：`xaiChatBaseURL`、`normalizeClaudeSamplingForUpstream`

### 3.2 内容要点

- 说明 **做了什么** 和 **为什么**，不只是复述 diff
- 引用具体函数/文件名，便于检索
- 多文件改动按文件/模块分组列出

### 3.3 示例

```
fix(xai): update compact request handling to use dedicated base URL
- Switched from `xaiChatBaseURL` to `xaiCompactBaseURL` for compact requests to avoid 404 errors from CLI chat-proxy.
- Updated headers to use standard API headers for compact endpoints.
- Added `xaiCompactBaseURL` helper function for dedicated compact request base URL resolution.
- Adjusted comments to clarify handling of compact and websocket transports.

Closes: #4376
```

```
fix(stream): resolve SSE/streaming bugs and goroutine leak
- handlers.go: remove unreachable dead code after infinite inner for loop
- interactions_handlers.go: guard producer goroutine sends with cliCtx.Done()
  to prevent permanent block/leak when client disconnects
- openai/gemini/responses/images handlers: surface pending upstream error on
  data-channel close instead of faking a successful [DONE] (select race)
- openai_images_handlers.go: add streamStarted guard in new pending-error
  branch to avoid writing JSON error body into an already-committed SSE stream
```

## 四、Footer

### 4.1 Issue 引用

用 `Closes: #XXXX`，独占一行，与正文之间空一行。

```
fix(registry): update Gemini 3.1 Flash Lite model ID and add test for validation

Closes: #4391
```

### 4.2 BREAKING CHANGE

如有破坏性变更，footer 写 `BREAKING CHANGE:` 并说明迁移路径。

## 五、禁止事项

- ❌ 标题使用过去时或第三人称：`fixed bug` / `updates handler`
- ❌ 标题末尾句号：`fix(xai): update base URL.`
- ❌ 模糊标题：`update code` / `fix bug` / `misc changes`
- ❌ 正文复述 diff：`- changed line 42 in xai_executor.go from 408 to 502`
- ❌ scope 使用大写：`fix(XAI): ...` → 应为 `fix(xai): ...`
- ❌ 标题包含 issue 编号：`fix(xai): resolve #4376` → 放到 footer `Closes:`
- ❌ 中英混排标题（除非触及中文文案模块）

## 六、好/坏示例对照

### ✅ 好

```
fix(executor): surface incomplete-stream error instead of forging [DONE]
- gemini_executor.go: detect missing finishReason on clean socket close and
  emit a 502 statusErr instead of synthesizing a successful [DONE]
- xai_executor.go: detect missing response.completed on clean socket close
  and emit a 502 statusErr, matching the Gemini path
- reasoning_stream_fault_test.go: add characterization tests for truncation,
  client disconnect, and malformed-chunk scenarios across providers
```

### ❌ 坏

```
update: 改了一些代码

修改了 xai_executor.go 和 gemini_executor.go，把 408 改成 502。
```

问题：type 缺失、scope 缺失、标题模糊、正文未用列表项、未说明动机。
