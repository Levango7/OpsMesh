# run-mysql-init.ps1
# Executes the unified init-mysql.sql against the nexus-ci-mysql container

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
