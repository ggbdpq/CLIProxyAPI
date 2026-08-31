# 一键编译数据管理版 exe（前端 SPA 构建 + Go 编译串联）
# 用法: powershell -ExecutionPolicy Bypass -File custom-addon\scripts\build.ps1
#       加 -SkipFrontend 跳过前端构建（只改了后端时省时；首次构建或改过前端不要跳）
# 位置: custom-addon/scripts/  ->  本脚本自动定位项目根再编译
param(
    [switch]$SkipFrontend
)
$ErrorActionPreference = "Stop"

# ---- 定位项目根（scripts -> custom-addon -> 项目根）----
$scriptsDir   = Split-Path -Parent $MyInvocation.MyCommand.Path
$customAddon  = Split-Path -Parent $scriptsDir
$projectRoot  = Split-Path -Parent $customAddon
$frontendDir  = Join-Path $customAddon "frontend"

$exeOut       = Join-Path $customAddon "exe\cli-proxy-api-datamgmt.exe"

# ---- 检查 go ----
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "未找到 go。请先安装 Go 并加入 PATH，再重开终端。"
}

# ---- 检查目标 exe 是否正被运行中的进程占用 ----
if (Test-Path $exeOut) {
    $proc = Get-Process | Where-Object { $_.Path -eq $exeOut }
    if ($proc) {
        Write-Warning "检测到进程 $($proc.Name) (PID $($proc.Id)) 正在使用 $exeOut。Windows 下运行中的 exe 无法被覆盖，请先停止服务再编译。"
        Write-Host "  停止命令示例: taskkill /PID $($proc.Id) /F" -ForegroundColor Yellow
    }
}

Push-Location $projectRoot
try {
    # ---- 前端构建：产出 custom-addon/frontend/dist（Go 运行时按 dist 路径 serve /data-mgmt/）----
    if (-not $SkipFrontend) {
        if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) {
            Write-Error "未找到 pnpm。请先安装（corepack enable 或 npm i -g pnpm）再构建前端，或用 -SkipFrontend 跳过。"
        }
        Write-Host "==> 构建前端 SPA (pnpm build)" -ForegroundColor Cyan
        Push-Location $frontendDir
        try { pnpm build; if ($LASTEXITCODE -ne 0) { throw "前端构建失败 (exit $LASTEXITCODE)" } }
        finally { Pop-Location }
    } else {
        Write-Host "==> 跳过前端构建 (-SkipFrontend)" -ForegroundColor DarkGray
    }

    # ---- Go 编译 ----
    Write-Host "==> 编译数据管理版 exe" -ForegroundColor Cyan
    Write-Host "    go build -o $exeOut ./cmd/server"
    go build -o $exeOut ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "编译失败 (exit $LASTEXITCODE)" }

    Write-Host ""
    Write-Host "编译完成：" -ForegroundColor Green
    Get-Item $exeOut | ForEach-Object {
        Write-Host ("  {0}  ({1:N0} bytes, 修改于 {2})" -f $_.FullName, $_.Length, $_.LastWriteTime)
    }
    Write-Host ""
    Write-Host "数据管理面板地址: http://127.0.0.1:<端口>/data-mgmt/ （端口见 config.yaml）" -ForegroundColor Cyan
}
finally {
    Pop-Location
}
