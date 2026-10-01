# run-mysql-init.ps1
# Executes init-mysql.sql (建库 + 授权) against the nexus-ci-mysql container.
# 注意：建表不在这里——控制面启动时跑 internal/store/migrations，服务启动时跑自己的
# schema.sql / initSchema。引导脚本里建表会让同名表静默让位（详见 init-mysql.sql 头部）。

$ContainerName = "nexus-ci-mysql"
$RootPassword = "nexus_ci"
$SqlFile = "$PSScriptRoot\init-mysql.sql"

if (-not (Test-Path -LiteralPath $SqlFile)) {
    Write-Error "SQL file not found: $SqlFile"
    exit 1
}

Write-Host "Checking container '$ContainerName' is running..." -ForegroundColor Cyan
$container = docker ps --filter "name=$ContainerName" --filter "status=running" --format "{{.Names}}"
if ($container -ne $ContainerName) {
    Write-Error "Container '$ContainerName' is not running. Start it first with docker compose."
    exit 1
}

Write-Host "Executing init-mysql.sql against $ContainerName..." -ForegroundColor Cyan
Get-Content -LiteralPath $SqlFile -Raw | docker exec -i $ContainerName mysql -uroot -p"$RootPassword" --default-character-set=utf8mb4

if ($LASTEXITCODE -eq 0) {
    Write-Host "Database initialized successfully." -ForegroundColor Green
}
else {
    Write-Error "MySQL execution failed with exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}
