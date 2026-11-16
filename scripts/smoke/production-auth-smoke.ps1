# production-auth-smoke.ps1 — verifies the production auth baseline end to end.
#
# What it proves:
#   1. --store=mysql (persistent) without --production/--require-auth fail-fasts
#      unless --allow-insecure-store is passed.
#   2. --production implies require-auth: /healthz stays public, but
#      GET /api/v1/devices is 401 for anonymous and bogus-bearer callers.
#   3. A NON-production instance (memory + --demo --allow-insecure-store)
#      returns 200 for anonymous GET /api/v1/devices — the difference is real.
#
# Both server processes are always stopped via finally. Exit 0 = all PASS.

$ErrorActionPreference = "Stop"

$Root = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$Tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("opsmesh-smoke-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $Tmp | Out-Null
$Bin = Join-Path $Tmp "opsmesh-smoke.exe"
$Cert = Join-Path $Root "deploy\docker\certs\tls.crt"
$Key = Join-Path $Root "deploy\docker\certs\tls.key"

$ProdProc = $null
$DevProc = $null
$Fails = 0

function Write-Pass([string]$Label) { Write-Host "PASS  $Label" }
function Write-Fail([string]$Label) { Write-Host "FAIL  $Label"; $script:Fails++ }
function Write-Info([string]$Message) { Write-Host "INFO  $Message" }

function Assert-Status([string]$Label, [int]$Want, [int]$Got) {
    if ($Want -eq $Got) { Write-Pass "$Label (HTTP $Got)" }
    else { Write-Fail "$Label (want HTTP $Want, got $Got)" }
}

function Get-FreePort {
    $listener = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try { $port = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port } finally { $listener.Stop() }
    return $port
}

function Get-UniquePort([int[]]$Used) {
    while ($true) {
        $p = Get-FreePort
        if ($Used -notcontains $p) { return $p }
    }
}

function Wait-Health([int]$Port, [System.Diagnostics.Process]$Proc, [int]$Seconds = 30) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    while ((Get-Date) -lt $deadline) {
        if ($Proc.HasExited) { return $false }
        try {
            $r = Invoke-WebRequest -Uri "http://127.0.0.1:$Port/healthz" -UseBasicParsing -TimeoutSec 2
            if ([int]$r.StatusCode -eq 200) { return $true }
        } catch { }
        Start-Sleep -Milliseconds 200
    }
    return $false
}

function Get-Status([string]$Url, [string]$Bearer = "") {
    try {
        $params = @{ Uri = $Url; UseBasicParsing = $true; TimeoutSec = 5; ErrorAction = "Stop" }
        if ($Bearer -ne "") { $params.Headers = @{ Authorization = "Bearer $Bearer" } }
        $r = Invoke-WebRequest @params
        return [int]$r.StatusCode
    } catch {
        $resp = $_.Exception.Response
        if ($null -ne $resp) { return [int]$resp.StatusCode }
        return 0
    }
}

function Show-Log([string]$Path) {
    if ((Test-Path $Path) -and ((Get-Item $Path).Length -gt 0)) {
        Write-Info "---- $(Split-Path $Path -Leaf) (tail) ----"
        Get-Content $Path -Tail 40 | ForEach-Object { Write-Host $_ }
    }
}

try {
    if (-not (Test-Path $Cert) -or -not (Test-Path $Key)) {
        Write-Fail "test TLS cert/key not found under deploy\docker\certs"
        exit 1
    }

    Write-Info "building controlplane binary"
    Push-Location $Root
    try {
        & go build -o $Bin ./cmd/opsmesh
    } finally {
        Pop-Location
    }
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path $Bin)) {
        Write-Fail "go build ./cmd/opsmesh (exit=$LASTEXITCODE)"
        exit 1
    }

    # --- assertion 1: fail-fast for persistent store without production/auth ---
    Write-Info "checking fail-fast: --store=mysql without --production"
    $ffOut = Join-Path $Tmp "ff.log"
    $ffErr = Join-Path $Tmp "ff.err.log"
    $used = @()
    $ffHttp = Get-UniquePort $used; $used += $ffHttp
    $ffGrpc = Get-UniquePort $used; $used += $ffGrpc
    $ffMetrics = Get-UniquePort $used
    $ffArgs = @(
        "--mode=controlplane", "--store=mysql",
        "--mysql-dsn=u:p@tcp(127.0.0.1:3306)/ops_device",
        "--http-port=$ffHttp", "--grpc-port=$ffGrpc", "--metrics-port=$ffMetrics",
        "--data-dir=$Tmp\ff-data"
    )
    $ffProc = Start-Process -FilePath $Bin -ArgumentList $ffArgs -Wait -PassThru -NoNewWindow `
        -RedirectStandardOutput $ffOut -RedirectStandardError $ffErr
    $ffMsg = ""
    foreach ($f in @($ffOut, $ffErr)) {
        if (Test-Path $f) { $ffMsg += (Get-Content $f -Raw) }
    }
    if ($ffProc.ExitCode -ne 0 -and $ffMsg -match "persistent store without --production") {
        Write-Pass "fail-fast for persistent store without --production (exit=$($ffProc.ExitCode))"
    } else {
        Write-Fail "fail-fast for persistent store without --production (exit=$($ffProc.ExitCode))"
        if ($ffMsg -ne "") { Write-Host $ffMsg }
    }

    # --- instance A: production baseline -------------------------------------
    $used = @()
    $p1 = Get-UniquePort $used; $used += $p1
    $g1 = Get-UniquePort $used; $used += $g1
    $m1 = Get-UniquePort $used
    $jwt = "smoke-jwt-secret-0123456789abcdef0123456789"
    $enc = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
    $prodArgs = @(
        "--mode=controlplane", "--store=memory", "--production",
        "--http-port=$p1", "--grpc-port=$g1", "--metrics-port=$m1",
        "--data-dir=$Tmp\prod-data",
        "--tls-cert=$Cert", "--tls-key=$Key", "--http-tls=off",
        "--jwt-secret=$jwt", "--encryption-key=$enc",
        "--admin-password=Sm0ke-Test!Passw0rd",
        "--log-level=info"
    )
    Write-Info "starting production instance on :$p1 (memory store, --production)"
    $ProdProc = Start-Process -FilePath $Bin -ArgumentList $prodArgs -PassThru -NoNewWindow `
        -RedirectStandardOutput (Join-Path $Tmp "prod.log") -RedirectStandardError (Join-Path $Tmp "prod.err.log")

    # --- instance B: non-production, explicit insecure opt-in ----------------
    $used = @()
    $p2 = Get-UniquePort $used; $used += $p2
    $g2 = Get-UniquePort $used; $used += $g2
    $m2 = Get-UniquePort $used
    $devArgs = @(
        "--mode=controlplane", "--store=memory", "--demo", "--allow-insecure-store",
        "--http-port=$p2", "--grpc-port=$g2", "--metrics-port=$m2",
        "--data-dir=$Tmp\dev-data",
        "--log-level=info"
    )
    Write-Info "starting non-production instance on :$p2 (memory + --demo --allow-insecure-store)"
    $DevProc = Start-Process -FilePath $Bin -ArgumentList $devArgs -PassThru -NoNewWindow `
        -RedirectStandardOutput (Join-Path $Tmp "dev.log") -RedirectStandardError (Join-Path $Tmp "dev.err.log")

    if (Wait-Health $p1 $ProdProc) { Write-Pass "production instance became healthy on :$p1" }
    else { Write-Fail "production instance did not become healthy on :$p1" }
    if (Wait-Health $p2 $DevProc) { Write-Pass "non-production instance became healthy on :$p2" }
    else { Write-Fail "non-production instance did not become healthy on :$p2" }

    if ($Fails -eq 0) {
        Assert-Status "prod GET /healthz (no credential)" 200 (Get-Status "http://127.0.0.1:$p1/healthz")
        Assert-Status "prod GET /api/v1/devices (anonymous)" 401 (Get-Status "http://127.0.0.1:$p1/api/v1/devices")
        Assert-Status "prod GET /api/v1/devices (bogus bearer)" 401 (Get-Status "http://127.0.0.1:$p1/api/v1/devices" "bogus-token")
        Assert-Status "dev GET /api/v1/devices (anonymous, non-production)" 200 (Get-Status "http://127.0.0.1:$p2/api/v1/devices")
    } else {
        Write-Info "skipping HTTP assertions because a server failed to start"
    }

    if ($Fails -ne 0) {
        Show-Log (Join-Path $Tmp "prod.log")
        Show-Log (Join-Path $Tmp "prod.err.log")
        Show-Log (Join-Path $Tmp "dev.log")
        Show-Log (Join-Path $Tmp "dev.err.log")
        Write-Host ""
        Write-Host "RESULT: FAIL ($Fails failure(s))"
        exit 1
    }

    Write-Host ""
    Write-Host "RESULT: PASS"
    exit 0
} finally {
    foreach ($p in @($ProdProc, $DevProc)) {
        if ($null -ne $p -and -not $p.HasExited) {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        }
    }
    if ($Fails -eq 0) {
        Remove-Item -LiteralPath $Tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}
