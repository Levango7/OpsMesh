# OpsMesh 负载基线包装器（load 场景）
# 用法：$env:BASE_URL="..."; $env:TOKEN="..."; pwsh scripts/loadtest/run-baseline.ps1
$ErrorActionPreference = "Stop"

if (-not (Get-Command k6 -ErrorAction SilentlyContinue)) {
    Write-Error "k6 未安装。安装方式见 https://grafana.com/docs/k6/latest/set-up/install-k6/"
    exit 127
}

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $env:BASE_URL) { $env:BASE_URL = "http://127.0.0.1:8080" }
if (-not $env:TENANT)   { $env:TENANT = "default" }
if (-not $env:TOKEN)    { Write-Warning "TOKEN 为空，仅压 /healthz 与 /readyz（业务读面跳过）" }

Write-Host "== OpsMesh 负载基线 =="
Write-Host "BASE_URL=$($env:BASE_URL)  TENANT=$($env:TENANT)  SCENARIO=load"

k6 run `
    -e SCENARIO=load `
    -e BASE_URL=$env:BASE_URL `
    -e TENANT=$env:TENANT `
    -e TOKEN=$env:TOKEN `
    (Join-Path $ScriptDir "k6-baseline.js")
