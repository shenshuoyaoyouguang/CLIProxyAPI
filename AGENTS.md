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
- `internal/api/` — Gin HTTP API (routes, middleware, modules)
- `internal/api/modules/amp/` — Amp integration (Amp-style routes + reverse proxy)
- `internal/thinking/` — Main thinking/reasoning pipeline. `ApplyThinking()` (apply.go) parses suffixes (`suffix.go`, suffix overrides body), normalizes config to canonical `ThinkingConfig` (`types.go`), normalizes and validates centrally (`validate.go`/`convert.go`), then applies provider-specific output via `ProviderApplier`. Do not break this "canonical representation → per-provider translation" architecture.
- `internal/runtime/executor/` — Per-provider runtime executors (incl. Codex WebSocket)
- `internal/translator/` — Provider protocol translators (and shared `common`)
- `internal/registry/` — Model registry + remote updater (`StartModelsUpdater`); `--local-model` disables remote updates
- `internal/store/` — Storage implementations and secret resolution
- `internal/managementasset/` — Config snapshots and management assets
- `internal/cache/` — Request signature caching
- `internal/watcher/` — Config hot-reload and watchers
- `internal/wsrelay/` — WebSocket relay sessions
- `internal/usage/` — Usage and token accounting
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
- Timeouts are allowed only during credential acquisition; after an upstream connection is established, do not set timeouts for any subsequent network behavior. Intentional exceptions that must remain allowed are the Codex websocket liveness deadlines in `internal/runtime/executor/codex_websockets_executor.go`, the wsrelay session deadlines in `internal/wsrelay/session.go`, the management APICall timeout in `internal/api/handlers/management/api_tools.go`, and the `cmd/fetch_antigravity_models` utility timeouts

## Known Pitfalls

### [nil-applier] nativeProviderAppliers 包含 nil 初始值
`internal/thinking/apply.go:22-32` — 所有内置 provider 在 `nativeProviderAppliers` 中的初始值为 nil，只有通过 `RegisterProvider` 注册后才变为非 nil。`GetProviderApplier()` 曾直接返回 nil 值，如果 applier 尚未注册就被调用，调用方可能因 nil 接口调用而 panic。已于 2026-06-15 修复：添加 `&& nativeApplier != nil` 检查。
→ 详见: `.omc/reports/merge-audit-2026-06-15.md`

### [LimitReader-inconsistency] LimitReader 使用模式不统一
`80ccf125` 提交在 8 处添加了 `io.LimitReader`，但工作区未提交修改中的 3 处使用了 `N+1` 模式（`1<<20+1` + 显式超限检查），其他位置没有显式超限检查。如果未来 LimitReader 实现有 bug，可能静默失败。建议统一使用 helper函数 `readLimited()`。
→ 详见: `.omc/reports/merge-audit-2026-06-15.md`

### [merge-divergence] main 与 origin/main 深度分叉
本地 main 与 origin/main 有 113 个共同修改文件但不同的提交 SHA（两边都对同一批上游内容做了 merge）。推送前必须先 `git merge origin/main` 解决冲突。不要直接 `git push --force`。
→ 详见: `.omc/reports/merge-audit-2026-06-15.md`

### [compilation-bug] origin/main conductor.go 有预存编译错误
`origin/main` 的 `sdk/cliproxy/auth/conductor.go:3631,3797` 调用了 `selector.Strategy()`，但 `Selector` 接口没有定义 `Strategy()` 方法，且所有 selector 实现也都没有此方法。这是上游 commit `6296f79e` 引入的 bug。合并 origin/main 后需 revert conductor.go 到本地版本。
→ 详见: `.omc/reports/merge-audit-2026-06-15.md`
