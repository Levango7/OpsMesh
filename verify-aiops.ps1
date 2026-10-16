# AIOps 真实 LLM 验证脚本
# 使用前请先设置环境变量：
#   $env:OPSMESH_AIOPS_API_KEY="你的 Kilo Gateway API Key"
#   $env:OPSMESH_AIOPS_BASE_URL="https://api.kilo.ai/v1"   # 可选，默认此值
#   $env:OPSMESH_AIOPS_MODEL="qvac/qwen3.5-0.8b"           # 可选，免费模型

$ErrorActionPreference = "Stop"

$apiKey = $env:OPSMESH_AIOPS_API_KEY
$baseURL = $env:OPSMESH_AIOPS_BASE_URL
if (-not $baseURL) { $baseURL = "https://api.kilo.ai/v1" }
$model = $env:OPSMESH_AIOPS_MODEL
if (-not $model) { $model = "qvac/qwen3.5-0.8b" }

if (-not $apiKey) {
    Write-Host "ERROR: 请先设置 OPSMESH_AIOPS_API_KEY 环境变量" -ForegroundColor Red
    Write-Host "  例如：$env:OPSMESH_AIOPS_API_KEY = 'sk-...'" -ForegroundColor Gray
    Write-Host "  可用免费模型（来自 kilo gateway 目录）：" -ForegroundColor Gray
    Write-Host "    qvac/qwen3.5-0.8b" -ForegroundColor Gray
    Write-Host "    qvac/gpt-oss-20b" -ForegroundColor Gray
    Write-Host "    qvac/gpt-oss-120b" -ForegroundColor Gray
    Write-Host "    nemotron-3-ultra-550b-a55b-free" -ForegroundColor Gray
    Write-Host "    nemotron-3.5-lightning-free" -ForegroundColor Gray
    exit 1
}

Write-Host "=== AIOps 真实 LLM 验证 ===" -ForegroundColor Cyan
Write-Host "Model: $model" -ForegroundColor Gray
Write-Host "BaseURL: $baseURL" -ForegroundColor Gray
Write-Host ""

# 测试 1: LLM 直接对话
Write-Host "--- 测试 1: LLM 直接对话 ---" -ForegroundColor Yellow
$body = @{
    model = $model
    messages = @(
        @{ role = "user"; content = "你是谁？请用一句话回答。" }
    )
    temperature = 0.3
    max_tokens = 1024
    stream = $false
} | ConvertTo-Json

try {
    $resp = Invoke-WebRequest -Uri "$baseURL/chat/completions" -Method POST -ContentType "application/json" -Headers @{ "Authorization" = "Bearer $apiKey" } -Body $body -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
    $content = $resp.Content | ConvertFrom-Json
    $reply = $content.choices[0].message.content
    Write-Host "LLM 回复: $reply" -ForegroundColor Green
    if ($content.usage) {
        Write-Host "Token 用量: prompt=$($content.usage.prompt_tokens) completion=$($content.usage.completion_tokens) total=$($content.usage.total_tokens)" -ForegroundColor Gray
    }
} catch {
    Write-Host "LLM 对话失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        Write-Host "响应体: $($reader.ReadToEnd())" -ForegroundColor Red
    }
    exit 1
}

Write-Host ""

# 测试 2: 控制面 AIOps 根因分析 API（需控制面已启动并监听 19680）
Write-Host "--- 测试 2: 控制面 AIOps 根因分析 API ---" -ForegroundColor Yellow
$cpURL = "http://127.0.0.1:19680"  # 控制面端口，可根据实际调整
$alertBody = @{
    alert = @{
        alertID   = "al-001"
        deviceID  = "dev-web-01"
        severity  = "critical"
        metric    = "cpu_usage"
        message   = "CPU 使用率持续超过 95%，持续 5 分钟"
        createdAt = (Get-Date).ToString("o")
    }
    metrics = @{
        cpu = @{ usage = 96.5; cores = 8; model = "Intel Xeon" }
        memory = @{ usage = 78.0; total = 16384; used = 12780 }
        disks = @(
            @{ mount = "C:"; usage = 85.0 }
            @{ mount = "D:"; usage = 45.0 }
        )
        network = @(
            @{ name = "eth0"; status = "up"; ip = "10.0.0.10" }
        )
        processCount = 245
        uptime = 172800
    }
    logs = @(
        @{ timestamp = (Get-Date).AddMinutes(-5).ToString("o"); level = "ERROR"; source = "nginx"; message = "connection refused upstream" }
        @{ timestamp = (Get-Date).AddMinutes(-3).ToString("o"); level = "WARN"; source = "system"; message = "high cpu usage detected" }
    )
} | ConvertTo-Json

try {
    $resp = Invoke-WebRequest -Uri "$cpURL/api/v1/aiops/analyze" -Method POST -ContentType "application/json" -Headers @{ "X-Tenant-ID" = "default" } -Body $alertBody -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
    $result = $resp.Content | ConvertFrom-Json
    Write-Host "根因类别: $($result.category)" -ForegroundColor Green
    Write-Host "根因组件: $($result.component)" -ForegroundColor Green
    Write-Host "根因描述: $($result.description)" -ForegroundColor Green
    Write-Host "信心度: $($result.confidence)" -ForegroundColor Green
    Write-Host "证据: $($result.evidence -join ', ')" -ForegroundColor Green
} catch {
    Write-Host "根因分析 API 调用失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    Write-Host "  可能原因：控制面未启动、AIOps 未启用（--aiops-enabled）、或端口不匹配" -ForegroundColor Gray
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        Write-Host "响应体: $($reader.ReadToEnd())" -ForegroundColor Red
    }
}

Write-Host ""

# 测试 3: 控制面 AIOps 智能修复 API
Write-Host "--- 测试 3: 控制面 AIOps 智能修复 API ---" -ForegroundColor Yellow
$remediateBody = @{
    rootCause = @{
        category     = "软件"
        component    = "nginx"
        description  = "nginx 进程异常退出"
        confidence   = 0.9
        evidence     = @("进程不存在")
    }
    constraints = @{
        maxSteps       = 5
        riskTolerance  = "low"
        requireApproval = $false
        timeoutSec     = 30
        autoExecute    = $false
    }
} | ConvertTo-Json

try {
    $resp = Invoke-WebRequest -Uri "$cpURL/api/v1/aiops/remediate" -Method POST -ContentType "application/json" -Headers @{ "X-Tenant-ID" = "default" } -Body $remediateBody -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
    $plan = $resp.Content | ConvertFrom-Json
    Write-Host "修复标题: $($plan.title)" -ForegroundColor Green
    Write-Host "步骤数: $($plan.steps.Count)" -ForegroundColor Green
    foreach ($step in $plan.steps) {
        Write-Host "  步骤 $($step.order): $($step.title) -> $($step.action) [$($step.target)]" -ForegroundColor Gray
    }
    Write-Host "安全级别: $($plan.safety.riskLevel) | 需要确认: $($plan.safety.requiresConfirm)" -ForegroundColor Gray
} catch {
    Write-Host "修复 API 调用失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        Write-Host "响应体: $($reader.ReadToEnd())" -ForegroundColor Red
    }
}

Write-Host ""

# 测试 4: 控制面 AIOps 自然语言任务 API
Write-Host "--- 测试 4: 控制面 AIOps 自然语言任务 API ---" -ForegroundColor Yellow
$nlBody = @{
    input = "重启设备 dev-web-01 的 nginx 服务"
    devices = @("dev-web-01")
} | ConvertTo-Json

try {
    $resp = Invoke-WebRequest -Uri "$cpURL/api/v1/aiops/remediate-nl" -Method POST -ContentType "application/json" -Headers @{ "X-Tenant-ID" = "default" } -Body $nlBody -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
    $plan = $resp.Content | ConvertFrom-Json
    Write-Host "操作标题: $($plan.title)" -ForegroundColor Green
    Write-Host "命令数: $($plan.commands.Count)" -ForegroundColor Green
    foreach ($cmd in $plan.commands) {
        Write-Host "  命令: $($cmd.command) [agent=$($cmd.agentID) timeout=$($cmd.timeout)s priority=$($cmd.priority)]" -ForegroundColor Gray
    }
} catch {
    Write-Host "自然语言任务 API 调用失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        Write-Host "响应体: $($reader.ReadToEnd())" -ForegroundColor Red
    }
}

Write-Host ""

# 测试 5: 控制面 AIOps 自动执行修复 API（下发任务到 agent）
Write-Host "--- 测试 5: 控制面 AIOps 自动执行修复 API ---" -ForegroundColor Yellow
$executeBody = @{
    plan = @{
        title = "重启 nginx 服务"
        steps = @(
            @{ order = 1; title = "重启服务"; action = "restart"; target = "dev-web-01"; command = "systemctl restart nginx"; verifyCmd = "systemctl status nginx" }
        )
        commands = @(
            @{ agentID = "dev-web-01"; command = "systemctl restart nginx"; type = "shell"; timeout = 30; priority = 5 }
        )
        dagNodes = @(
            @{ id = "n1"; name = "重启 nginx"; type = "shell"; command = "systemctl restart nginx"; dependsOn = @(); retryCount = 3; retryDelay = 5 }
        )
        safety = @{ requiresConfirm = $false; rollbackCmd = ""; riskLevel = "low"; notes = @("注意备份配置") }
    }
    autoExecute = $true
} | ConvertTo-Json

try {
    $resp = Invoke-WebRequest -Uri "$cpURL/api/v1/aiops/execute" -Method POST -ContentType "application/json" -Headers @{ "X-Tenant-ID" = "default" } -Body $executeBody -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
    $result = $resp.Content | ConvertFrom-Json
    Write-Host "下发总数: $($result.total) | 成功: $($result.dispatched)" -ForegroundColor Green
    foreach ($r in $result.results) {
        Write-Host "  命令 $($r.CommandID): status=$($r.Status) taskID=$($r.TaskID) agent=$($r.AgentID)" -ForegroundColor Gray
        if ($r.Reason) { Write-Host "    原因: $($r.Reason)" -ForegroundColor Gray }
    }
} catch {
    Write-Host "执行修复 API 调用失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        Write-Host "响应体: $($reader.ReadToEnd())" -ForegroundColor Red
    }
}

Write-Host ""

# 测试 6: 控制面 AIOps 状态查询
Write-Host "--- 测试 6: 控制面 AIOps 状态查询 ---" -ForegroundColor Yellow
try {
    $resp = Invoke-WebRequest -Uri "$cpURL/api/v1/aiops/status" -Method GET -Headers @{ "X-Tenant-ID" = "default" } -UseBasicParsing -TimeoutSec 10 -ErrorAction Stop
    $status = $resp.Content | ConvertFrom-Json
    Write-Host "启用: $($status.enabled) | 提供商: $($status.provider) | 模型: $($status.model)" -ForegroundColor Green
    Write-Host "统计: 分析=$($status.stats.analyzesTotal) 修复=$($status.stats.remediations) 自然语言=$($status.stats.nlParses) 执行=$($status.stats.executedRemediations)" -ForegroundColor Gray
} catch {
    Write-Host "状态查询失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
}

Write-Host ""
Write-Host "=== 验证完成 ===" -ForegroundColor Cyan
