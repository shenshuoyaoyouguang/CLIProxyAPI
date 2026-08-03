[CmdletBinding()]
param(
    # Align with upstream CI by default (pr-test-build.yml: go build only).
    # Opt-in stricter local gates:
    [switch]$WithTests,  # go test ./... (AGENTS.md recommends; not enforced in CI)
    [switch]$WithLint    # golangci-lint (not used by upstream at all)
)

$ErrorActionPreference = "Stop"

# Run everything from the repo root so relative go package paths (./cmd/server/)
# and output files resolve regardless of the caller's working directory.
# The finally block restores the caller's location even on `exit` paths
# (PowerShell runs finally before exiting).
Push-Location $PSScriptRoot
try {

# --- Pre-build: ensure Go toolchain is functional ---
# The project declares a minimum Go version in go.mod (currently 1.26.0).
# Force GOTOOLCHAIN=local to use the system-installed Go even when the
# version doesn't match exactly. This avoids issues with incomplete
# auto-downloaded toolchain caches (missing src/).
$env:GOTOOLCHAIN = "local"

# Read minimum Go version from go.mod
$goModPath = "$PSScriptRoot/go.mod"
if (Test-Path $goModPath) {
    $goModContent = Get-Content $goModPath -Raw
    if ($goModContent -match '(?m)^go\s+(\d+\.\d+(?:\.\d+)?)') {
        $requiredVersion = $matches[1]
    } else {
        Write-Host "WARNING: Could not parse Go version from go.mod" -ForegroundColor Yellow
    }
} else {
    Write-Host "WARNING: go.mod not found at $goModPath" -ForegroundColor Yellow
}
$currentVersion = (go version).Split(' ')[2].TrimStart('go')
if ($requiredVersion -and ([Version]$currentVersion -lt [Version]$requiredVersion)) {
    Write-Host "WARNING: Go version $currentVersion < go.mod minimum $requiredVersion" -ForegroundColor Yellow
    Write-Host "  Build may succeed but could miss language/runtime features." -ForegroundColor Yellow
    Write-Host "  Consider installing Go $requiredVersion or removing GOTOOLCHAIN=local to auto-download." -ForegroundColor Yellow
}

$goroot = go env GOROOT
if (-not (Test-Path "$goroot\src\net\http")) {
    Write-Host "ERROR: GOROOT ($goroot) is missing standard library sources." -ForegroundColor Red
    Write-Host "This usually means the auto-downloaded toolchain cache is incomplete." -ForegroundColor Yellow
    Write-Host "Fix: Delete the broken cache and let the build use the system Go:" -ForegroundColor Yellow
    $gopath = go env GOPATH
    $fixCmd = "Remove-Item -Recurse -Force `"$gopath\pkg\mod\golang.org\toolchain@*`""
    Write-Host "  $fixCmd" -ForegroundColor White
    Write-Host "Or ensure GOTOOLCHAIN=local is set in the environment." -ForegroundColor Yellow
    exit 1
}

# --- Build metadata ---
# Match docker-build.ps1 / release workflow: git describe (with dirty marker) + UTC build date.
# Fall back to main package defaults when git metadata is unavailable.
$VERSION = "dev"
$COMMIT = "none"
$prevEAP = $ErrorActionPreference
$ErrorActionPreference = "Continue"
try {
    $described = (& git describe --tags --always --dirty 2>$null | Select-Object -First 1)
    if (-not [string]::IsNullOrWhiteSpace($described)) {
        $VERSION = $described.ToString().Trim()
    }
    $shortCommit = (& git rev-parse --short HEAD 2>$null | Select-Object -First 1)
    if (-not [string]::IsNullOrWhiteSpace($shortCommit)) {
        $COMMIT = $shortCommit.ToString().Trim()
    }
} catch {
    # Keep VERSION/COMMIT defaults when git is unavailable.
} finally {
    $ErrorActionPreference = $prevEAP
}
$BUILD_DATE = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')

Write-Host "Building with the following info:"
Write-Host "  Version: $VERSION"
Write-Host "  Commit: $COMMIT"
Write-Host "  Build Date: $BUILD_DATE"
Write-Host "  Quality gate: CI-aligned (go build); optional: -WithTests / -WithLint"
Write-Host "----------------------------------------"

$env:CGO_ENABLED = 0

# --- Optional: Lint (NOT in upstream CI) ---
if ($WithLint) {
    Write-Host "----------------------------------------"
    Write-Host "Running static analysis (golangci-lint) [opt-in; not in upstream CI]..." -ForegroundColor Cyan

    $golangciLint = Get-Command golangci-lint -ErrorAction SilentlyContinue
    if (-not $golangciLint) {
        Write-Host "ERROR: golangci-lint not found in PATH." -ForegroundColor Red
        Write-Host "Install it from https://golangci-lint.run/usage/install/" -ForegroundColor Yellow
        Write-Host "Or omit -WithLint to match upstream CI." -ForegroundColor Yellow
        exit 1
    }

    $lintOutput = & golangci-lint run ./... 2>&1
    $lintExitCode = $LASTEXITCODE

    if ($lintExitCode -ne 0) {
        Write-Host "----------------------------------------" -ForegroundColor Red
        Write-Host "Lint FAILED (exit code: $lintExitCode)" -ForegroundColor Red
        Write-Host "Detailed lint output:" -ForegroundColor Red
        Write-Host "----------------------------------------" -ForegroundColor Red
        $lintOutput | ForEach-Object { Write-Host $_ }
        Write-Host "----------------------------------------" -ForegroundColor Red
        Write-Host "Build aborted due to lint errors." -ForegroundColor Red
        Write-Host "Note: upstream CI does not run golangci-lint. Omit -WithLint for CI-aligned builds." -ForegroundColor Yellow
        exit $lintExitCode
    }

    Write-Host "Static analysis passed." -ForegroundColor Green
    Write-Host "----------------------------------------"
} else {
    Write-Host "----------------------------------------"
    Write-Host "Skipping golangci-lint (matches upstream CI; use -WithLint to enable)." -ForegroundColor DarkGray
    Write-Host "----------------------------------------"
}

# --- Optional: Test (documented in AGENTS.md; not enforced in pr-test-build.yml) ---
if ($WithTests) {
    Write-Host "----------------------------------------"
    Write-Host "Running tests (go test -count=1 -timeout 30m ./...) [opt-in]..." -ForegroundColor Cyan

    $testOutput = & go test -count=1 -timeout 30m ./... 2>&1
    $testExitCode = $LASTEXITCODE

    if ($testExitCode -ne 0) {
        Write-Host "----------------------------------------" -ForegroundColor Red
        Write-Host "Tests FAILED (exit code: $testExitCode)" -ForegroundColor Red
        Write-Host "Detailed test output:" -ForegroundColor Red
        Write-Host "----------------------------------------" -ForegroundColor Red
        $testOutput | ForEach-Object { Write-Host $_ }
        Write-Host "----------------------------------------" -ForegroundColor Red
        Write-Host "Build aborted due to test failures." -ForegroundColor Red
        exit $testExitCode
    }

    Write-Host "All tests passed." -ForegroundColor Green
    Write-Host "----------------------------------------"
} else {
    Write-Host "----------------------------------------"
    Write-Host "Skipping tests (matches upstream PR CI; use -WithTests for local full suite)." -ForegroundColor DarkGray
    Write-Host "----------------------------------------"
}

# --- Build (required; same gate as .github/workflows/pr-test-build.yml) ---
Write-Host "Starting build (go build ./cmd/server)..." -ForegroundColor Cyan
go build -trimpath -ldflags="-s -w -buildid= -X 'main.Version=$VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildDate=$BUILD_DATE'" -o cli-proxy-api.exe ./cmd/server/

if ($LASTEXITCODE -ne 0) {
    Write-Host "Build failed with exit code: $LASTEXITCODE" -ForegroundColor Red
    exit $LASTEXITCODE
}

$sizeBefore = (Get-Item cli-proxy-api.exe).Length / 1MB

Write-Host "Compressing with UPX..." -ForegroundColor Yellow
try {
    upx --best --lzma cli-proxy-api.exe 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "UPX exited with code $LASTEXITCODE"
    }
    $sizeAfter = (Get-Item cli-proxy-api.exe).Length / 1MB
    $saved = $sizeBefore - $sizeAfter
    Write-Host "UPX: saved $([math]::Round($saved, 2)) MB ($([math]::Round($sizeBefore, 2)) -> $([math]::Round($sizeAfter, 2)) MB)" -ForegroundColor Gray
    # Smoke test: stripped + UPX-compressed Go binaries can fail at load time
    # on some platforms. Verify the artifact starts (--help exits 2 via the
    # standard flag package's ErrHelp path) before declaring success.
    $smokeOut = (& .\cli-proxy-api.exe --help 2>&1 | Out-String)
    $smokeExit = $LASTEXITCODE
    if ($smokeExit -notin @(0, 1, 2) -or $smokeOut -notmatch "Usage of") {
        throw "UPX-compressed binary failed smoke test (exit=$smokeExit)"
    }
    Write-Host "UPX smoke test passed." -ForegroundColor Gray
} catch {
    $sizeAfter = $sizeBefore
    Write-Host "UPX compression skipped (continuing with uncompressed build)" -ForegroundColor Yellow
}

# --- Generate checksum ---
$hash = (Get-FileHash -Path cli-proxy-api.exe -Algorithm SHA256).Hash.ToLower()
$hashLine = "$hash  cli-proxy-api.exe"
$hashLine | Set-Content -Path "cli-proxy-api.exe.sha256" -NoNewline

# --- Build summary ---
Write-Host "----------------------------------------"
Write-Host "Build completed successfully!" -ForegroundColor Green
Write-Host "  Executable: cli-proxy-api.exe"
Write-Host "  Size:       $([math]::Round($sizeAfter, 2)) MB"
Write-Host "  SHA256:     $hash"
Write-Host "----------------------------------------"
} finally {
    Pop-Location
}
