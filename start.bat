@echo off
rem OpsMesh 本地启动脚本（控制面）
rem 安全说明（修复）：JWT 密钥与公开注册开关均从环境变量读取，避免硬编码泄露。
rem 不再内置 demo 兜底密钥：未设置 OPSMESH_JWT_SECRET 时二进制自动生成随机密钥（重启后会话失效）。
cd /d F:\Nexus\OpsMesh

if "%OPSMESH_JWT_SECRET%"=="" (
  echo [提示] 未设置 OPSMESH_JWT_SECRET，将自动生成随机密钥（重启后旧 token 失效）；生产务必设置强密钥：set OPSMESH_JWT_SECRET=^<openssl rand -hex 32^>
)

rem 公开注册免审批：默认关闭（安全基线）；如需本地演示免审批可设 OPSMESH_ALLOW_PUBLIC_REGISTER=true。
if "%OPSMESH_ALLOW_PUBLIC_REGISTER%"=="" set "OPSMESH_ALLOW_PUBLIC_REGISTER=false"

rem --jwt-secret 不再显式传递：config 直接读取 OPSMESH_JWT_SECRET 环境变量，未设置则随机生成。
start /B "" "F:\Nexus\OpsMesh\opsmesh.exe" --mode=controlplane --store=memory --demo --allow-public-register=%OPSMESH_ALLOW_PUBLIC_REGISTER% --http-port=8080 --grpc-port=9090
