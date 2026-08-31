# 数据备份脚本：把运行数据（data-records.sqlite + config.yaml）备份到项目根 backup/
# 用法: powershell -ExecutionPolicy Bypass -File custom-addon\scripts\backup-data.ps1
# 位置: custom-addon/scripts/  ->  自动定位项目根
$ErrorActionPreference = "Stop"

$scriptsDir   = Split-Path -Parent $MyInvocation.MyCommand.Path   # custom-addon/scripts
$customAddon  = Split-Path -Parent $scriptsDir                    # custom-addon
$projectRoot  = Split-Path -Parent $customAddon                   # 项目根

$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$backupDir = Join-Path $projectRoot "backup"
New-Item -ItemType Directory -Force $backupDir | Out-Null

$sqlite = Join-Path $projectRoot "data\data-records.sqlite"
$config = Join-Path $projectRoot "config.yaml"

if (Test-Path $sqlite) {
    $dest = Join-Path $backupDir "data-records-$stamp.sqlite"
    Copy-Item $sqlite $dest
    Write-Host "已备份: $dest" -ForegroundColor Green
} else {
    Write-Warning "未找到 $sqlite，跳过数据库备份。"
}

if (Test-Path $config) {
    $dest = Join-Path $backupDir "config-$stamp.yaml"
    Copy-Item $config $dest
    Write-Host "已备份: $dest" -ForegroundColor Green
} else {
    Write-Warning "未找到 $config，跳过配置备份。"
}
