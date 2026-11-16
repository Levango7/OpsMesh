#Requires -Version 5.1
<#
.SYNOPSIS
    OpsMesh Dockerfile digest-pin verifier (TD-89 residual).

.DESCRIPTION
    Verifies every FROM line in Dockerfile* files (repo root, deploy/, operator/)
    contains an @sha256:<64-hex> digest pin. scratch and ARG-based FROM ($VAR)
    are skipped with a warning. Exits 1 when any unpinned FROM is found.

.PARAMETER FixHint
    Print manual pin / Renovate instructions for unpinned images.

.PARAMETER Report
    Print only "file:line" lines for unpinned FROMs (for inventory).

.PARAMETER Help
    Show usage.
#>
[CmdletBinding()]
param(
    [switch]$FixHint,
    [switch]$Report,
    [switch]$Help
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Show-Usage {
    @'
Usage: scripts/security/verify-digest-pins.ps1 [-FixHint] [-Report]

Verifies every FROM line in Dockerfile* files (repo root, deploy/, operator/)
contains an @sha256:<64-hex> digest pin. scratch and ARG-based FROM ($VAR)
are skipped with a warning.

Flags:
  -FixHint   Print manual pin / Renovate instructions for unpinned images.
  -Report    Print only "file:line" lines for unpinned FROMs (for inventory).

Exit codes: 0 all pinned, 1 any unpinned.
'@
}

if ($Help) {
    Show-Usage
    exit 0
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$root = (Resolve-Path (Join-Path $scriptDir '..\..')).Path
Set-Location $root

$files = @()
$rootItems = Get-ChildItem -LiteralPath $root -File -Filter 'Dockerfile*' -ErrorAction SilentlyContinue
foreach ($item in $rootItems) { $files += $item.FullName }
if (Test-Path -LiteralPath (Join-Path $root 'deploy')) {
    $deployItems = Get-ChildItem -LiteralPath (Join-Path $root 'deploy') -Recurse -File -Filter 'Dockerfile*' -ErrorAction SilentlyContinue
    foreach ($item in $deployItems) { $files += $item.FullName }
}
if (Test-Path -LiteralPath (Join-Path $root 'operator')) {
    $opItems = Get-ChildItem -LiteralPath (Join-Path $root 'operator') -Recurse -File -Filter 'Dockerfile*' -ErrorAction SilentlyContinue
    foreach ($item in $opItems) { $files += $item.FullName }
}
$files = @($files | Sort-Object -Unique)

if ($files.Count -eq 0) {
    [Console]::Error.WriteLine('error: no Dockerfile* found.')
    exit 2
}

$totalUnpinned = 0
$unpinnedImages = New-Object System.Collections.Generic.List[string]

foreach ($full in $files) {
    $rel = $full.Substring($root.Length + 1)
    $rawLines = Get-Content -LiteralPath $full
    $fileFail = $false
    $fileUnpinnedLines = New-Object System.Collections.Generic.List[string]
    $n = 0
    foreach ($raw in $rawLines) {
        $n++
        $trimmed = $raw.TrimStart()
        if ($trimmed -notmatch '^(?i)FROM(\s|$)') { continue }
        $first = ($trimmed -split '\s+', 2)[0]
        $rest = if ($trimmed.Length -gt $first.Length) { $trimmed.Substring($first.Length).TrimStart() } else { '' }
        $tokens = @($rest -split '\s+' | Where-Object { $_ -ne '' })
        $image = ''
        $skipNext = $false
        foreach ($tok in $tokens) {
            if ($skipNext) { $skipNext = $false; continue }
            if ($tok -match '^--platform=') { continue }
            if ($tok -eq '--platform') { $skipNext = $true; continue }
            if ($tok -match '^(?i)AS$') { break }
            $image = $tok
            break
        }
        if (-not $image) { continue }

        $low = $image.ToLowerInvariant()
        if ($low -eq 'scratch') {
            [Console]::Error.WriteLine("WARN ${rel}:${n}: scratch base skipped")
            continue
        }
        if ($image.StartsWith('$')) {
            [Console]::Error.WriteLine("WARN ${rel}:${n}: ARG-based FROM skipped ($image)")
            continue
        }

        if ($image -notmatch '@sha256:[0-9a-fA-F]{64}$') {
            $fileFail = $true
            $totalUnpinned++
            if (-not $unpinnedImages.Contains($image)) { $unpinnedImages.Add($image) }
            $fileUnpinnedLines.Add("  line ${n}: $raw")
            if ($Report) { Write-Output "${rel}:${n}" }
        }
    }

    if (-not $Report) {
        if (-not $fileFail) {
            Write-Output "PASS  $rel"
        }
        else {
            Write-Output "FAIL  $rel"
            foreach ($l in $fileUnpinnedLines) { Write-Output $l }
        }
    }
}

if (-not $Report) {
    Write-Output ''
    Write-Output "Summary: $totalUnpinned unpinned FROM line(s) across $($files.Count) file(s)"
}

if ($FixHint) {
    Write-Output ''
    Write-Output '--fix-hint--'
    Write-Output 'Manual pin: append @sha256:<64-hex> to each unpinned image reference.'
    Write-Output '  crane digest <image>'
    Write-Output '  # or: docker pull <image> && docker inspect --format "{{index .RepoDigests 0}}" <image>'
    foreach ($img in $unpinnedImages) { Write-Output "  crane digest $img" }
    Write-Output 'Renovate: pinDigests is already enabled; run npx renovate (or wait for automated PRs).'
}

if ($totalUnpinned -gt 0) { exit 1 }
exit 0
