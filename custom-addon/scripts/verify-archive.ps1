# 归档校验脚本：对比源码树补丁文件与 custom-addon/modified-files/ 是否一致，防止双份拷贝漂移
# 用法: powershell -ExecutionPolicy Bypass -File custom-addon\scripts\verify-archive.ps1
# 位置: custom-addon/scripts/  ->  modified-files 在上上级（custom-addon/），源码树在上三级（项目根）
# 说明: 现行补丁面 = 2 个被修改的官方文件 + 1 个新增测试文件（api_tools_quota_test.go）。
#       注入形态（updater.go/updater_test.go）已于 2026-09 退役回官方版，不再属于补丁面。
#       custom-addon/backend 与 custom-addon/frontend 是单一来源（直接编译/构建），
#       归档目录不保存它们的副本，只检查关键文件存在。
$ErrorActionPreference = "Stop"

$scriptsDir   = Split-Path -Parent $MyInvocation.MyCommand.Path   # custom-addon/scripts
$customAddon  = Split-Path -Parent $scriptsDir                    # custom-addon
$projectRoot  = Split-Path -Parent $customAddon                   # 项目根

$pairs = @(
  'internal/api/handlers/management/api_tools.go',
  'internal/api/handlers/management/api_tools_quota_test.go',
  'internal/api/server_management.go'
)

$singleSource = @(
  'custom-addon/backend/data_records.go',
  'custom-addon/backend/data_records_test.go',
  'custom-addon/frontend/src/routes/index.tsx',
  'custom-addon/frontend/src/routes/batches.tsx',
  'custom-addon/frontend/src/lib/queries.ts',
  'custom-addon/frontend/dist/index.html'
)

$allOk = $true
foreach ($rel in $pairs) {
  $src = Join-Path $projectRoot $rel
  $arc = Join-Path $customAddon "modified-files\$rel"
  $a = Get-FileHash $src -ErrorAction SilentlyContinue
  $b = Get-FileHash $arc -ErrorAction SilentlyContinue
  if (-not $a -or -not $b -or $a.Hash -ne $b.Hash) {
    Write-Host "漂移: $rel" -ForegroundColor Yellow
    $allOk = $false
  } else {
    Write-Host "一致: $rel" -ForegroundColor Green
  }
}

foreach ($rel in $singleSource) {
  $src = Join-Path $projectRoot $rel
  if (-not (Test-Path $src)) {
    Write-Host "缺失单一来源文件: $rel" -ForegroundColor Yellow
    $allOk = $false
  } else {
    Write-Host "存在: $rel（单一来源，归档不存副本）" -ForegroundColor Green
  }
}

if ($allOk) {
  Write-Host ""
  Write-Host "全部一致，归档与源码树同步。" -ForegroundColor Green
  exit 0
} else {
  Write-Host ""
  Write-Host "存在漂移，请同步 modified-files/ 与源码树。" -ForegroundColor Yellow
  exit 1
}
