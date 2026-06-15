# CLIProxyAPI Build Scripts

## build-optimized.ps1

Windows 专用构建脚本，用于构建 `cli-proxy.exe`。脚本会设置 `GOTOOLCHAIN=local` 和 `CGO_ENABLED=0`，从 Git 获取版本信息，并尝试使用 UPX 压缩产物。

```powershell
.\build-optimized.ps1
```

构建完成后会在脚本所在目录生成 `cli-proxy.exe`。如果本机未安装 UPX 或 UPX 压缩失败，脚本会保留未压缩的可执行文件并继续完成构建。
