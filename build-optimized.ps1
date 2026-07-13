$ErrorActionPreference = "Stop"

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
    if ($goModContent -match '^go\s+(\d+\.\d+(?:\.\d+)?)') {
        $requiredVersion = $matches[1]
    }
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

# --- Build ---
$VERSION = git describe --tags --always
$COMMIT = git rev-parse --short HEAD
$BUILDDATE = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')

$env:CGO_ENABLED = 0
go build -trimpath -ldflags="-s -w -buildid= -X 'main.Version=$VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildDate=$BUILDDATE'" -o cli-proxy-api.exe ./cmd/server/

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
    Write-Host "Build successful: cli-proxy-api.exe" -ForegroundColor Green
    Write-Host "  Before: $([math]::Round($sizeBefore, 2)) MB" -ForegroundColor Gray
    Write-Host "  After:  $([math]::Round($sizeAfter, 2)) MB (saved $([math]::Round($saved, 2)) MB)" -ForegroundColor Gray
} catch {
    $sizeRounded = [math]::Round($sizeBefore, 2)
    Write-Host "UPX compression failed, but executable is ready: cli-proxy-api.exe ($sizeRounded MB)" -ForegroundColor Yellow
}