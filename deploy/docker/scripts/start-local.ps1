# OpsMesh Local Deployment Script
# 一键启动所有服务并验证

$ErrorActionPreference = "SilentlyContinue"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  OpsMesh Local Deployment" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# 1. 检查 MySQL
Write-Host "`n[1/5] 检查 MySQL..." -ForegroundColor Yellow
$mysqlRunning = docker ps --filter "name=nexus-ci-mysql" --format "{{.Names}}"
if (-not $mysqlRunning) {
    Write-Host "  启动 MySQL 容器..." -ForegroundColor Gray
    docker run -d --name opsmesh-mysql-test `
        -e MYSQL_ROOT_PASSWORD=opsmesh_root `
        -e MYSQL_DATABASE=opsmesh `
        -e MYSQL_USER=opsmesh `
        -e MYSQL_PASSWORD=opsmesh_pass `
        -p 3307:3306 `
        swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/library/mysql:8.0 `
        --default-authentication-plugin=mysql_native_password
    Start-Sleep -Seconds 30
} else {
    Write-Host "  MySQL 已运行" -ForegroundColor Green
}

# 2. 创建数据库表
Write-Host "`n[2/5] 初始化数据库..." -ForegroundColor Yellow
docker exec nexus-ci-mysql mysql -uroot -pnexus_ci opsmesh -e "
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) PRIMARY KEY,
    username VARCHAR(128) UNIQUE NOT NULL,
    email VARCHAR(256) NOT NULL,
    password_hash VARCHAR(256) NOT NULL,
    status VARCHAR(32) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    must_change_password TINYINT(1) DEFAULT 0
);" 2>$null
Write-Host "  数据库就绪" -ForegroundColor Green

# 3. 创建 admin 用户
Write-Host "`n[3/5] 创建 admin 用户..." -ForegroundColor Yellow
$bcryptHash = '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy'
docker exec nexus-ci-mysql mysql -uroot -pnexus_ci opsmesh -e "INSERT IGNORE INTO users (id, username, email, password_hash, status) VALUES ('usr-admin', 'admin', 'admin@opsmesh.io', '$bcryptHash', 'active');" 2>$null
Write-Host "  admin 用户已创建" -ForegroundColor Green

# 4. 启动服务
Write-Host "`n[4/5] 启动微服务..." -ForegroundColor Yellow

# 停止旧进程
Get-Process -Name "opsmesh","auth-svc","device-svc","task-svc","alert-svc" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 2

$env:DB_DSN='opsmesh:opsmesh_pass@tcp(localhost:3306)/opsmesh?parseTime=true'
$env:JWT_SECRET='opsmesh_jwt_secret_2024'

# Controlplane (MemoryStore 快速测试)
Set-Location "F:\Nexus\OpsMesh"
$env:STORE='memory'
$env:DEMO='true'
Start-Process -FilePath ".\opsmesh.exe" -WindowStyle Normal
Start-Sleep -Seconds 3

# Auth-svc
Set-Location "F:\Nexus\OpsMesh\services\auth-svc"
$env:AUTH_SVC_HTTP_PORT=8100
$env:AUTH_SVC_JWT_SECRET='opsmesh_jwt_secret_2024'
Start-Process -FilePath ".\auth-svc.exe" -WindowStyle Normal
Start-Sleep -Seconds 2

# Device-svc
Set-Location "F:\Nexus\OpsMesh\services\device-svc"
$env:DEVICE_SVC_HTTP_PORT=8101
Start-Process -FilePath ".\device-svc.exe" -WindowStyle Normal
Start-Sleep -Seconds 2

# Task-svc
Set-Location "F:\Nexus\OpsMesh\services\task-svc"
$env:TASK_SVC_HTTP_PORT=8102
Start-Process -FilePath ".\task-svc.exe" -WindowStyle Normal
Start-Sleep -Seconds 2

# Alert-svc
Set-Location "F:\Nexus\OpsMesh\services\alert-svc"
$env:ALERT_SVC_HTTP_PORT=8103
Start-Process -FilePath ".\alert-svc.exe" -WindowStyle Normal
Start-Sleep -Seconds 3

# 5. 验证
Write-Host "`n[5/5] 验证服务..." -ForegroundColor Yellow
$services = @(
    @{Name="Controlplane"; Port=8080; Path="/healthz"},
    @{Name="Auth-svc"; Port=8100; Path="/health"},
    @{Name="Device-svc"; Port=8101; Path="/health"},
    @{Name="Task-svc"; Port=8102; Path="/health"},
    @{Name="Alert-svc"; Port=8103; Path="/health"}
)

foreach ($svc in $services) {
    try {
        $resp = Invoke-WebRequest -Uri "http://localhost:$($svc.Port)$($svc.Path)" -UseBasicParsing -TimeoutSec 5
        Write-Host "  ✅ $($svc.Name) (:$($svc.Port)) - $($resp.Content)" -ForegroundColor Green
    } catch {
        Write-Host "  ❌ $($svc.Name) (:$($svc.Port)) - 无法连接" -ForegroundColor Red
    }
}

Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "  部署完成!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "`n访问地址:" -ForegroundColor White
Write-Host "  Controlplane: http://localhost:8080" -ForegroundColor Gray
Write-Host "  Auth-svc:     http://localhost:8100" -ForegroundColor Gray
Write-Host "  Device-svc:   http://localhost:8101" -ForegroundColor Gray
Write-Host "  Task-svc:     http://localhost:8102" -ForegroundColor Gray
Write-Host "  Alert-svc:    http://localhost:8103" -ForegroundColor Gray
Write-Host "`n测试命令:" -ForegroundColor White
Write-Host "  # 注册用户" -ForegroundColor Gray
Write-Host '  curl -X POST http://localhost:8080/api/v1/auth/register -H "Content-Type: application/json" -d "{\"username\":\"admin\",\"password\":\"Admin123456\",\"email\":\"admin@opsmesh.io\"}"' -ForegroundColor Gray
Write-Host "  # 登录" -ForegroundColor Gray
Write-Host '  curl -X POST http://localhost:8080/api/v1/auth/login -H "Content-Type: application/json" -d "{\"username\":\"admin\",\"password\":\"Admin123456\"}"' -ForegroundColor Gray
