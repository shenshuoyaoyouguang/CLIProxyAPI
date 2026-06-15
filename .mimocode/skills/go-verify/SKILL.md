---
name: go-verify
description: "Standardized Go build+test verification after code changes. Runs format, build, vet, and test in the correct order with clear pass/fail reporting."
---

# Go Verify

Run this after any Go code change to confirm the change compiles, passes formatting, and does not break tests.

## Steps

### 1. Format
```bash
gofmt -w <changed-files>
```
Format all modified `.go` files. If the user did not specify files, format the package(s) they changed.

### 2. Build
```bash
go build -o test-output ./cmd/server && rm test-output
```
Verify the full server binary compiles. This is **required** after every change per AGENTS.md.

### 3. Vet (optional, run when touching internal packages)
```bash
go vet ./internal/<changed-package>/...
```
Run vet on the touched package subtree. Skip if the change is in `sdk/`, `cmd/`, or `test/` only.

### 4. Test — scoped first, full suite second
```bash
# Package-scoped (fast feedback):
go test ./path/to/changed/pkg/... -count=1

# Full suite (background, when ready):
go test ./... 2>&1
```

**Rules:**
- Always run the **package-scoped** test first. This gives fast feedback.
- Run the **full suite** only after package-scoped tests pass.
- If the full suite shows failures in packages you did not touch, treat them as baseline noise (see AGENTS.md §Baseline full-suite failures).
- For flaky test stability checks, run the same test 5–10 times: `for i in $(seq 1 5); do go test ./pkg -run TestName -count=1; done`

## Reporting

After each step, report:
- ✅ PASS or ❌ FAIL
- Which step failed (format/build/vet/test)
- For test failures: the failing test name and package

Stop on the first failure. Do not proceed to later steps if an earlier step fails.

## Notes

- This skill does **not** commit. Commit is a separate action.
- `go build -o test-output ./cmd/server` (not `go build ./...`) is the canonical build check because it exercises the full `cmd/server` entrypoint.
- On Windows (PowerShell), use `;` instead of `&&` if the shell does not support `&&`.
