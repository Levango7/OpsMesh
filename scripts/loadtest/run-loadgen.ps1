# OpsMesh load baseline wrapper (PowerShell): builds the controlplane, runs it
# in demo/memory mode on a free port, executes scripts/loadtest/loadgen.go,
# appends one row to scripts/loadtest/BASELINE.md, and always stops the server.
$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = (Resolve-Path (Join-Path $ScriptDir "../..")).Path

$Vus = if ($env:VUS) { [int]$env:VUS } else { 50 }
$Duration = if ($env:DURATION) { $env:DURATION } else { "30s" }
$Tenant = if ($env:TENANT) { $env:TENANT } else { "default" }
$Timeout = if ($env:TIMEOUT) { $env:TIMEOUT } else { "5s" }
$Think = if ($env:THINK) { $env:THINK } else { "1s" }
$Token = if ($env:TOKEN) { $env:TOKEN } else { "" }
$BasePort = if ($env:PORT) { [int]$env:PORT } else { 18080 }

function Get-FreePort([int]$Start) {
    $p = $Start
    while ($true) {
        $listener = $null
        try {
            $listener = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, $p)
            $listener.Start()
            $listener.Stop()
            return $p
        } catch {
            if ($listener) { try { $listener.Stop() } catch { } }
            $p++
        }
    }
}

$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("opsmesh-loadgen-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TmpDir | Out-Null
$Proc = $null
$pushed = $false
$rc = 1

function Append-Text([string]$Path, [string]$Text) {
    [System.IO.File]::AppendAllText($Path, $Text, (New-Object System.Text.UTF8Encoding($false)))
}

try {
    Push-Location $RootDir
    $pushed = $true

    $HttpPort = Get-FreePort $BasePort
    $GrpcPort = Get-FreePort ($HttpPort + 1)
    $MetricsPort = Get-FreePort ($GrpcPort + 1)

    Write-Host "== OpsMesh loadgen baseline =="
    Write-Host "repo=$RootDir vus=$Vus duration=$Duration http_port=$HttpPort"

    Write-Host "== building controlplane =="
    $exe = Join-Path $TmpDir "controlplane.exe"
    & go build -o $exe ./cmd/opsmesh
    if ($LASTEXITCODE -ne 0) { throw "go build ./cmd/opsmesh failed (exit $LASTEXITCODE)" }

    Write-Host "== starting controlplane (--demo --store=memory) =="
    $logOut = Join-Path $TmpDir "controlplane.out.log"
    $logErr = Join-Path $TmpDir "controlplane.err.log"
    $Proc = Start-Process -FilePath $exe -ArgumentList @(
        "--mode=controlplane",
        "--demo",
        "--store=memory",
        "--task-svc-grpc-addr=",
        "--http-port=$HttpPort",
        "--grpc-port=$GrpcPort",
        "--metrics-port=$MetricsPort"
    ) -RedirectStandardOutput $logOut -RedirectStandardError $logErr -NoNewWindow -PassThru

    $ready = $false
    for ($i = 0; $i -lt 60; $i++) {
        if ($Proc.HasExited) {
            Get-Content $logOut -Tail 40 -ErrorAction SilentlyContinue | Write-Host
            Get-Content $logErr -Tail 40 -ErrorAction SilentlyContinue | Write-Host
            throw "controlplane exited before becoming ready (exit $($Proc.ExitCode))"
        }
        try {
            $resp = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/readyz" -TimeoutSec 1
            if ($resp.StatusCode -eq 200) { $ready = $true; break }
        } catch { }
        Start-Sleep -Milliseconds 500
    }
    if (-not $ready) {
        Get-Content $logOut -Tail 40 -ErrorAction SilentlyContinue | Write-Host
        Get-Content $logErr -Tail 40 -ErrorAction SilentlyContinue | Write-Host
        throw "controlplane did not become ready within 30s"
    }

    Write-Host "== running loadgen =="
    if (-not $Token) {
        try {
            $login = Invoke-WebRequest -UseBasicParsing -Method Post `
                -Uri "http://127.0.0.1:$HttpPort/api/v1/auth/login" `
                -ContentType "application/json" `
                -Body '{"username":"admin","password":"admin123"}' -TimeoutSec 5
            $loginObj = ConvertFrom-Json $login.Content
            $Token = $loginObj.token
            if (-not $Token -and $loginObj.changePasswordToken) {
                $changeBody = @{
                    oldPassword         = "admin123"
                    newPassword         = "Loadgen!Baseline2026"
                    changePasswordToken = $loginObj.changePasswordToken
                } | ConvertTo-Json -Compress
                $changed = Invoke-WebRequest -UseBasicParsing -Method Post `
                    -Uri "http://127.0.0.1:$HttpPort/api/v1/auth/change-password" `
                    -ContentType "application/json" -Body $changeBody -TimeoutSec 5
                $Token = (ConvertFrom-Json $changed.Content).token
            }
            if ($Token) { Write-Host "demo admin token acquired" }
        } catch { }
        if (-not $Token) { Write-Warning "demo login failed; protected endpoints may return 401" }
    }

    $lgArgs = @(
        "run", (Join-Path $ScriptDir "loadgen.go"),
        "-base", "http://127.0.0.1:$HttpPort",
        "-tenant", $Tenant,
        "-vus", "$Vus",
        "-duration", $Duration,
        "-timeout", $Timeout,
        "-think", $Think
    )
    if ($Token) { $lgArgs += @("-token", $Token) }

    $lines = New-Object System.Collections.Generic.List[string]
    & go @lgArgs 2>&1 | ForEach-Object {
        $line = [string]$_
        Write-Host $line
        $lines.Add($line)
    }
    $rc = $LASTEXITCODE

    $idx = $lines.IndexOf("## JSON summary")
    if ($idx -lt 0 -or $idx -ge ($lines.Count - 1)) { throw "loadgen produced no JSON summary (exit $rc)" }
    $jsonText = ($lines[($idx + 1)..($lines.Count - 1)] -join "`n")
    $summary = $jsonText | ConvertFrom-Json

    $dateUtc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $sha = (& git rev-parse --short HEAD 2>$null | Out-String).Trim()
        if ($LASTEXITCODE -ne 0 -or -not $sha) { $sha = "unknown" }
        $porcelain = (& git status --porcelain 2>$null | Out-String).Trim()
        if ($porcelain) { $sha = "$sha-dirty" }
    } catch {
        if (-not $sha) { $sha = "unknown" }
    } finally {
        $ErrorActionPreference = $prevEap
    }

    $verdict = if ($rc -eq 0) { "PASS" } else { "FAIL" }
    $errPct = [double]$summary.overall.error_rate * 100
    $row = "| $dateUtc | $sha | loadgen | $Vus | {0:F2} | {1:F2} | {2:F2} | {3:F2} | {4:F3}% | $verdict | demo memory :$HttpPort $Duration think=$Think (pwsh) |" -f `
        [double]$summary.overall.rps, `
        [double]$summary.overall.p50_ms, `
        [double]$summary.overall.p95_ms, `
        [double]$summary.overall.p99_ms, `
        $errPct

    $baseline = Join-Path $ScriptDir "BASELINE.md"
    if (-not (Test-Path $baseline)) {
        $header = @'
# OpsMesh 控制面负载基线

> 由 `scripts/loadtest/run-loadgen.sh` / `run-loadgen.ps1` 自动追加。
> 环境：单机 controlplane `--demo --store=memory`，压测进程与目标同机。
> 判据与 k6 基线一致：overall p99 < 300ms 且错误率 < 0.1%（2xx/3xx 视为成功）。

| 日期 (UTC) | commit | 场景 | VUs | RPS | p50 (ms) | p95 (ms) | p99 (ms) | 错误率 | 结论 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
'@
        [System.IO.File]::WriteAllText($baseline, $header + "`n", (New-Object System.Text.UTF8Encoding($false)))
    }
    Append-Text $baseline ($row + "`n")
    Write-Host ""
    Write-Host "appended: $row"
} catch {
    Write-Error $_
    $rc = 1
} finally {
    if ($Proc) {
        try {
            if (-not $Proc.HasExited) {
                Stop-Process -Id $Proc.Id -Force
                Wait-Process -Id $Proc.Id -Timeout 10 -ErrorAction SilentlyContinue
            }
        } catch { }
    }
    if ($pushed) { Pop-Location }
    Remove-Item -LiteralPath $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}

exit $rc
