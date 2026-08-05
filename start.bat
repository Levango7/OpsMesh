@echo off
rem OpsMesh 本地启动脚本（控制面）
rem 安全说明（task 89）：JWT 密钥与公开注册开关均从环境变量读取，避免硬编码泄露。
cd /d F:\Nexus\OpsMesh

rem JWT 密钥：优先环境变量 OPSMESH_JWT_SECRET；未设置时用内置 demo 兜底值并告警。
if "%OPSMESH_JWT_SECRET%"=="" (
  set "OPSMESH_JWT_SECRET=opsmesh-demo-jwt-secret-2026"
  echo [警告] 未设置 OPSMESH_JWT_SECRET，使用内置 demo 密钥，仅限本地演示；生产务必设置强密钥。
)

rem 公开注册免审批：默认关闭（安全基线）；如需本地演示免审批可设 OPSMESH_ALLOW_PUBLIC_REGISTER=true。
if "%OPSMESH_ALLOW_PUBLIC_REGISTER%"=="" set "OPSMESH_ALLOW_PUBLIC_REGISTER=false"

start /B "" "F:\Nexus\OpsMesh\opsmesh.exe" --mode=controlplane --store=memory --demo --allow-public-register=%OPSMESH_ALLOW_PUBLIC_REGISTER% --jwt-secret=%OPSMESH_JWT_SECRET% --http-port=8080 --grpc-port=9090
