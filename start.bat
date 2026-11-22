@echo off
rem OpsMesh Windows 本地构建与启动脚本（控制面）
rem 从脚本所在目录运行，不再依赖任何开发机绝对路径。
setlocal
cd /d "%~dp0"

rem 密钥与公开注册开关只从环境变量读取；未设置密钥时由二进制生成随机密钥。
if "%OPSMESH_JWT_SECRET%"=="" (
  echo [提示] 未设置 OPSMESH_JWT_SECRET，将自动生成随机密钥（重启后旧 token 失效）；生产务必设置强密钥。
)
if "%OPSMESH_ALLOW_PUBLIC_REGISTER%"=="" set "OPSMESH_ALLOW_PUBLIC_REGISTER=false"

rem 默认先构建企业版前端并注入版本元信息；OPSMESH_SKIP_BUILD=1 仅用于明确复用现有 exe。
if not "%OPSMESH_SKIP_BUILD%"=="1" (
  powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0deploy\docker\scripts\build-windows.ps1"
  if errorlevel 1 (
    echo [错误] Windows 构建失败，未启动旧二进制。
    exit /b 1
  )
)

if not exist "%~dp0opsmesh.exe" (
  echo [错误] 未找到 opsmesh.exe；请去掉 OPSMESH_SKIP_BUILD 后重新执行构建。
  exit /b 1
)

start /B "" "%~dp0opsmesh.exe" --mode=controlplane --store=memory --demo --allow-public-register=%OPSMESH_ALLOW_PUBLIC_REGISTER% --http-port=8080 --grpc-port=9090 --metrics-port=9091
