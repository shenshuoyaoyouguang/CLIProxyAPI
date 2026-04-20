# 构建指南

## 环境要求

- Go 1.21+
- Git
- PowerShell (Windows) 或 Bash (Linux/macOS)

## Windows PowerShell 构建

```powershell
$VERSION = git describe --tags --always
$COMMIT = git rev-parse --short HEAD
$BUILDDATE = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')

go build -ldflags="-s -w -X 'main.Version=$VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildDate=$BUILDDATE'" -o cli-proxy.exe ./cmd/server/
```




  $VERSION = git describe --tags --always
  $COMMIT = git rev-parse --short HEAD
  $BUILDDATE = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')

  # 优化构建：保留所有版本信息 + 最大程度剥离调试符号
  $env:CGO_ENABLED = 0
  go build -trimpath -ldflags="-s -w -buildid= -X 'main.Version=$VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildDate=$BUILDDATE'"
   -o cli-proxy.exe ./cmd/server/

  # UPX 最大压缩
  upx --best --lzma cli-proxy.exe

  验证版本信息仍然存在：

  .\cli-proxy.exe --version





## Linux/macOS Bash 构建

```bash
VERSION=$(git describe --tags --always) && \
COMMIT=$(git rev-parse --short HEAD) && \
BUILDDATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) && \
go build -ldflags="-s -w -X 'main.Version=$VERSION' -X 'main.Commit=$COMMIT' -X 'main.BuildDate=$BUILDDATE'" -o cli-proxy ./cmd/server/
```

## ldflags 参数说明

| 参数 | 说明 |
|------|------|
| `-s` | 禁用符号表，减小文件大小 |
| `-w` | 禁用 DWARF 调试信息，进一步压缩 |
| `-X 'main.Version=...'` | 注入 Git 标签版本 |
| `-X 'main.Commit=...'` | 注入 Git 提交 SHA |
| `-X 'main.BuildDate=...'` | 注入构建时间 (UTC) |

## 快速构建（无版本信息）

```bash
go build -o cli-proxy ./cmd/server/
```

## Docker 构建

```bash
docker build -t cli-proxy .
```

或使用提供的脚本：

```bash
# Windows
./docker-build.ps1

# Linux/macOS
./docker-build.sh
```

## 验证构建

构建完成后，版本信息会在以下位置显示：

1. **启动日志** - 控制台输出
2. **HTTP 响应头** - `X-CPA-VERSION`, `X-CPA-COMMIT`, `X-CPA-BUILD-DATE`
3. **请求日志** - 记录中包含版本信息