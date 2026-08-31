@echo off
rem 本文件为 GBK/ANSI 编码，请勿用 UTF-8 编辑器保存
title CLIProxyAPI 8317 数据管理版启动器
setlocal enabledelayedexpansion

set "EXE=%~dp0..\exe\cli-proxy-api-8317.exe"
set "WORKDIR=C:\Users\802165\CLIProxyAPI"
set "CONFIG=config.yaml"
set "PORT=8317"
set "CPA_PLUGIN_UI=1"
set "URL=http://localhost:%PORT%/management.html"

echo ============================================================
echo   CLIProxyAPI 8317  (数据管理剥离版 + Chrome 插件注入)
echo ============================================================
echo.

REM ---- 1. 检查剥离版 exe 是否存在 ----
if not exist "%EXE%" (
    echo [错误] 未找到剥离版 exe: %EXE%
    echo        请先编译: powershell -ExecutionPolicy Bypass -File %~dp0build.ps1
    echo.
    pause
    exit /b 1
)

REM ---- 2. 检查端口占用 ----
echo [检查] 端口 %PORT% 占用情况 ...
set "PID="
for /f "tokens=5" %%P in ('netstat -ano ^| findstr /R ":%PORT% .*LISTENING" ^| findstr /V "\["') do set "PID=%%P"

if not defined PID (
    echo [OK] 端口 %PORT% 空闲，可直接启动。
    goto START
)

echo [提示] 端口 %PORT% 已被 PID %PID% 占用:
tasklist /FI "PID eq %PID%" 2>nul | findstr /I "%PID%"
echo.
echo        这通常是已有一个 CLIProxyAPI 服务在运行。
echo        继续会先停止它（当前服务会中断）。
set /p CHOICE=是否停止该进程并重启服务？[Y/N]
if /i not "%CHOICE%"=="Y" (
    echo [退出] 未做任何操作。
    pause
    exit /b 0
)
taskkill /PID %PID% /F >nul 2>&1
if errorlevel 1 (
    echo [错误] 停止进程失败，请以管理员身份重新运行本脚本。
    pause
    exit /b 1
)
echo [完成] 已停止 PID %PID%，端口已释放。
timeout /t 1 >nul

:START
echo [启动] 工作目录: %WORKDIR%
echo        配置文件: %CONFIG%  (端口 8317, auth-dir 复用官方 token)
cd /d "%WORKDIR%"
start "CLIProxyAPI-8317" "%EXE%" --config %CONFIG%
echo.
echo [完成] 已在新窗口启动。请在浏览器打开:
echo        %URL%
echo        管理密钥: 30@webFE
timeout /t 3 >nul
exit /b 0
