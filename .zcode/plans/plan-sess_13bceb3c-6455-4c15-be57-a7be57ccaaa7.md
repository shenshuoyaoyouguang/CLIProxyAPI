## 目标

消除 `internal/api/handlers/management/config_lists.go` 中 8 个模块（GeminiKey / InteractionsKey / ClaudeKey / CodexKey / XAIKey / ZAIKey / VertexCompatKey / OpenAICompatibility）GET/PUT/PATCH/DELETE 处理程序约 1370 行的重复代码（约占文件 80%），使"BaseURL 删除行为"这类逻辑只存在一处。

## 设计：Go 泛型辅助函数 + per-module 声明式规格

利用项目 Go 1.26 的泛型，新增 3 个通用 helper（放在文件顶部现有 `putStringList` 等旁边），每个模块的 handler 收缩为声明式调用。

### 1. `putKeyList[T any]` — PUT 共享（约 35 行）
- 解析 raw body（数组 或 `{"items": [...]}`，与现状逐字一致），400 错误信息不变
- `normalize func(int, *T) error`：逐项规范化（Vertex 用它做 api-key 必填校验，错误信息含 `[%d]` 索引，与现状一致）
- `keep func(*T) bool`：过滤（Codex/XAI/ZAI/OpenAI 过滤空 base-url）
- 加锁 → `apply([]T)` 赋值 → sanitize → `persistLocked`

### 2. `keyPatchSpec[T, V]` + `patchKeyEntry[T, V]` — PATCH 共享（约 70 行）
- 共享 envelope `keyPatchRequest[V]`：`{index, match, name, value}`（OpenAI 用 `name` 定位，其余用 `match`）
- 定位：index 优先，其次按 match key 查找；未命中 404
- **BaseURL/APIKey 删除策略集中在此一处**：`DeleteOnEmptyAPIKey` / `DeleteOnEmptyBaseURL` 两个 flag 声明策略，命中空值即删除条目
- 未删除则调用 per-module `Apply func(*T, *V)`（只含该类型特有字段：Models / Priority / Websockets / DisableCooling / RebuildMidSystemMessage / Disabled / Name / APIKeyEntries 等）
- sanitize + persistLocked；流程与现状完全一致（含"空值删除时忽略其他字段"的语义）

### 3. `deleteKeyEntry[T]` + `deleteNamedEntry[T]` — DELETE 共享（约 75 行）
- `?api-key[&base-url]`（用 `GetQuery` 区分"显式空 base-url"，这是现有测试覆盖的行为）→ `?index` → 400
- `strict404 bool`：Gemini/Interactions=true（无匹配 404），其余=false（静默 200）
- OpenAI 单独走 `deleteNamedEntry`（按 name 过滤全部匹配项）

## Per-module 规格（严格保持现状，flag 取值如下）

| 模块 | PATCH 空 api-key 删除 | PATCH 空 base-url 删除 | DELETE strict404 |
|---|---|---|---|
| Gemini / Interactions | true | false | true |
| Claude | false | false | false |
| Codex / XAI / ZAI | false | true | false |
| Vertex | true | true | false |
| OpenAI | n/a | true | 按 name |

GET handler 与 `*WithAuthIndex` helper、`normalize*` 函数、OAuth 两个模块、`[]string` 三个 helper 均不动。handler 方法签名不变 → `server_management.go` 路由零改动。

## 行为保持说明（1 处微统一）

Claude/Codex/XAI/ZAI/OpenAI 的 PATCH match 查找缺少 `match != ""` 守卫（Gemini/Interactions/Vertex 有）。统一为有守卫版本：仅在"match 为空字符串且存在 APIKey 为空条目的退化场景"下行为不同（原本会命中空 key 条目，统一后 404），实际使用中不可达。其余行为逐字保持。

## 验证

1. `gofmt -w` 格式化
2. `go build ./...` 编译通过
3. 现有测试全绿（含 DELETE 语义测试）：
   - `go test ./internal/api/handlers/management/ -run 'TestDeleteGeminiKey|TestDeleteClaudeKey|TestDeleteVertexCompatKey|TestDeleteXAIKey|TestDeleteCodexKey'`
   - `go test ./internal/api/handlers/management/ -run 'TestPut|TestPatch|TestZAI|TestOpenAICompat'`
   - `go test ./internal/api/handlers/management/`（整包）
4. `go vet ./internal/api/handlers/management/`

## 预期结果

- 8 个模块从 ~1370 行缩至 ~450 行（每模块 GET+PUT+DELETE 各 3-5 行，PATCH ≈ 30 行含类型定义）
- 新增 helper ~180 行；文件总量 1740 → ~800 行
- 后续修改"BaseURL 删除行为"只需改 `patchKeyEntry` / `deleteKeyEntry` 一处逻辑 + 单行 flag