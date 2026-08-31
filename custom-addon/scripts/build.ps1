# 一键编译数据管理版两种形态 exe
# 用法: powershell -ExecutionPolicy Bypass -File scripts\build.ps1
# 位置: custom-addon/scripts/  ->  本脚本自动定位项目根再编译
$ErrorActionPreference = "Stop"

# ---- 定位项目根（scripts -> custom-addon -> cpa-manage 项目根）----
$scriptsDir   = Split-Path -Parent $MyInvocation.MyCommand.Path
$customAddon  = Split-Path -Parent $scriptsDir
$projectRoot  = Split-Path -Parent $customAddon

$exeGoInject  = Join-Path $customAddon "exe\cli-proxy-api-datamgmt.exe"   # Go 注入版 (8318 形态)
$exePluginUi  = Join-Path $customAddon "exe\cli-proxy-api-8317.exe"       # 插件注入版 (8317 形态)

# ---- 检查 go ----
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "未找到 go。请先安装 Go 并加入 PATH，再重开终端。"
}

# ---- 检查目标 exe 是否正被运行中的进程占用 ----
foreach ($exe in @($exeGoInject, $exePluginUi)) {
    if (Test-Path $exe) {
        $proc = Get-Process | Where-Object { $_.Path -eq $exe }
        if ($proc) {
            Write-Warning "检测到进程 $($proc.Name) (PID $($proc.Id)) 正在使用 $exe。Windows 下运行中的 exe 无法被覆盖，请先停止服务再编译。"
            Write-Host "  停止命令示例: taskkill /PID $($proc.Id) /F" -ForegroundColor Yellow
        }
    }
}

# ---- 编译 ----
Push-Location $projectRoot
try {
    Write-Host "==> 编译数据管理版 exe" -ForegroundColor Cyan
    Write-Host "    go build -o $exeGoInject ./cmd/server"
    go build -o $exeGoInject ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "编译失败 (exit $LASTEXITCODE)" }

    Copy-Item -Force $exeGoInject $exePluginUi
    Write-Host "==> 已复制为插件注入版启动器使用的 $exePluginUi（运行时设 CPA_PLUGIN_UI=1）" -ForegroundColor Cyan

    Write-Host ""
    Write-Host "编译完成：" -ForegroundColor Green
    Get-Item $exeGoInject, $exePluginUi | ForEach-Object {
        Write-Host ("  {0}  ({1:N0} bytes, 修改于 {2})" -f $_.FullName, $_.Length, $_.LastWriteTime)
    }
    Write-Host ""
    Write-Host "启动方式见 docs/使用指南.md 第 2.4 节。" -ForegroundColor Cyan
}
finally {
    Pop-Location
}
