@AGENTS.md

- CI (`pr-test-build`) only builds the server; still run `go test ./...` (and the compile check in AGENTS.md) after changes.
- Optional lint (not in CI): `golangci-lint run ./...` or `./build-optimized.ps1 -WithLint` (config: `.golangci.yml`; reports only new issues by default).
- For thinking / multi-turn CoT traps, check `docs/PITFALLS.md` before changing passback or translator paths.
