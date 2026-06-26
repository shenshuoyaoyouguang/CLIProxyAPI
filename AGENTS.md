# AGENTS.md

Go 1.26+ proxy server providing OpenAI/Gemini/Claude/Codex compatible APIs with OAuth, round-robin load balancing, and a plugin system.

## Repository
- GitHub: https://github.com/router-for-me/CLIProxyAPI

## Commands
```bash
gofmt -w . # Format (required after Go changes)
go build -o cli-proxy-api ./cmd/server # Build
go run ./cmd/server # Run dev server
go test ./... # Run all tests
go test -v -run TestName ./path/to/pkg # Run single test
go build -o test-output ./cmd/server # Verify compile (REQUIRED after changes)
```
- Remove `test-output` using your current shell after verification
- Common flags: `--config <path>`, `--tui`, `--standalone`, `--local-model`, `--no-browser`, `--oauth-callback-port <port>`

## Config
- Default config: `config.yaml` (template: `config.example.yaml`)
- `.env` is auto-loaded from the working directory
- Auth material defaults under `auths/`
- Storage backends: file-based default; optional Postgres/git/object store (`PGSTORE_*`, `GITSTORE_*`, `OBJECTSTORE_*`)
- SDK config: `internal/config/sdk_config.go`

## Architecture
- `cmd/server/` — Server entrypoint (`main.go`, version/commit ldflags)
- `cmd/fetch_antigravity_models/`, `cmd/fetch_codex_models/` — Model registry update utilities
- `internal/api/` — Gin HTTP API: `server.go` (core routing), `protocol_multiplexer.go` (HTTP/WS protocol detection), `redis_queue_protocol.go` (Redis-backed request queue). Handlers under `handlers/management/`.
- `internal/config/` — Config loading, parsing, cloning, image-gen mode toggle, Vertex compat, plugin config. `config.go` is the main file (~67K).
- `internal/thinking/` — Canonical thinking/reasoning pipeline. `ApplyThinking()` (apply.go) parses suffixes (suffix.go, suffix overrides body), normalizes to canonical `ThinkingConfig` (types.go), validates centrally (validate.go/convert.go), then applies provider-specific output via `ProviderApplier`. Provider logic under `provider/`. Do not break this "canonical representation → per-provider translation" architecture.
- `internal/translator/` — Provider protocol translators. Sub-packages: `antigravity/`, `claude/`, `codex/`, `gemini/`, `openai/`, `reasoning/`, `common/`, `translator/`.
- `internal/runtime/executor/` — Per-provider runtime executors (antigravity, claude, codex, gemini, kimi, mimo, openai-compat, xai, aistudio). Codex has WebSocket + OpenAI images support. Helper files under `helps/`.
- `internal/registry/` — Model registry + remote updater (`StartModelsUpdater`); `--local-model` disables remote updates
- `internal/pluginhost/` — Plugin host system: WASM/native plugin loading (`loader_*.go`), RPC client (`rpc_client.go`), adapters for provider translation (`adapters.go`), auth provider callbacks, model routing, scheduler, management hooks, HTTP/stream bridges. Platform-specific loaders (unix/windows/unsupported).
- `internal/pluginstore/` — Plugin store: GitHub-based registry, install, versioning, checksums.
- `internal/redisqueue/` — Redis queue for deferred request processing and usage toggle.
- `internal/store/` — Storage implementations and secret resolution
- `internal/cache/` — Request signature caching
- `internal/signature/` — Provider-specific request/response validation (claude, gemini, gpt) and compatibility checks
- `internal/watcher/` — Config hot-reload; sub-packages: `diff/`, `synthesizer/`
- `internal/wsrelay/` — WebSocket relay sessions
- `internal/cmd/` — CLI login commands (anthropic, antigravity, kimi, openai, xai, vertex import)
- `internal/auth/` — OAuth and auth management
- `internal/access/` — Config access control and reconciliation
- `internal/home/` — Home directory, certificates, global client, KV helpers
- `internal/interfaces/` — Shared interface types (APIHandler, ClientModels, ErrorMessage)
- `internal/misc/` — Utilities (antigravity version, MIME types, credentials, OAuth helpers, header utils)
- `internal/safemode/` — Safe mode with example API keys
- `internal/tui/` — Bubbletea terminal UI (`--tui`, `--standalone`)
- `internal/managementasset/` — Config snapshots and management assets
- `internal/buildinfo/` — Build version/commit metadata
- `sdk/cliproxy/` — Embeddable SDK entry (service/builder/watchers/pipeline)
- `sdk/` — Also contains SDK-specific `access/`, `api/`, `auth/`, `config/`, `logging/`, `pluginabi/`, `pluginapi/`, `proxyutil/`, `translator/`
- `test/` — Cross-module integration tests (thinking conversion, builtin tools translation, Claude Code compatibility, usage logging)

## Code Conventions
- Keep changes small and simple (KISS)
- Comments in English only
- If editing code that already contains non-English comments, translate them to English (don't add new non-English comments)
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

## Notes
