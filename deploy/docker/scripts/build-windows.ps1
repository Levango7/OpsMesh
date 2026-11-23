#Requires -Version 5.1
[CmdletBinding()]
param(
    [switch]$SkipFrontend
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string[]]$Arguments
    )
    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "命令失败（exit=$LASTEXITCODE）：$FilePath $($Arguments -join ' ')"
    }
}

$root = (Resolve-Path (Join-Path $PSScriptRoot '../../..')).Path
$embedDir = Join-Path $root 'internal/controlplane/embed/enterprise'
$exePath = Join-Path $root 'opsmesh.exe'

Push-Location $root
try {
    if (-not $SkipFrontend) {
        $npm = Get-Command 'npm.cmd' -ErrorAction Stop
        $webDir = Join-Path $root 'web/enterprise'
        Push-Location $webDir
        try {
            if (-not (Test-Path -LiteralPath (Join-Path $webDir 'node_modules') -PathType Container)) {
                Invoke-Native $npm.Source @('ci', '--no-audit', '--no-fund')
            }
            Invoke-Native $npm.Source @('run', 'build')
        }
        finally {
            Pop-Location
        }

        $distDir = Join-Path $webDir 'dist'
        $distIndex = Join-Path $distDir 'index.html'
        if (-not (Test-Path -LiteralPath $distIndex -PathType Leaf)) {
            throw "企业版构建产物缺失：$distIndex"
        }
        New-Item -ItemType Directory -Path $embedDir -Force | Out-Null
        Get-ChildItem -LiteralPath $embedDir -Force |
            Where-Object { $_.Name -ne 'placeholder.html' -and $_.Name -ne '.gitignore' } |
            Remove-Item -Recurse -Force
        Get-ChildItem -LiteralPath $distDir -Force |
            Copy-Item -Destination $embedDir -Recurse -Force
    }

    # 禁止用占位页或空 embed 凑出 Windows 交付二进制。
    $embeddedIndex = Join-Path $embedDir 'index.html'
    if (-not (Test-Path -LiteralPath $embeddedIndex -PathType Leaf)) {
        throw "企业版尚未装配：$embeddedIndex（请勿在全新源码树上使用 -SkipFrontend）"
    }
    $indexText = [System.IO.File]::ReadAllText($embeddedIndex)
    if ($indexText.Contains('OPSMESH_ENTERPRISE_BUNDLE_PLACEHOLDER') -or $indexText.Contains('未内置')) {
        throw '企业版 embed 仍是占位页，拒绝构建发布二进制'
    }

    $version = $env:OPSMESH_VERSION
    $commit = $env:OPSMESH_COMMIT
    $buildDate = $env:OPSMESH_BUILD_DATE
    $git = Get-Command 'git' -ErrorAction SilentlyContinue
    if ($null -ne $git) {
        if ([string]::IsNullOrWhiteSpace($version)) {
            $version = [string](Invoke-Native $git.Source @('describe', '--tags', '--always', '--dirty'))
        }
        if ([string]::IsNullOrWhiteSpace($commit)) {
            $commit = [string](Invoke-Native $git.Source @('rev-parse', '--short', 'HEAD'))
        }
        if ([string]::IsNullOrWhiteSpace($buildDate)) {
            $buildDate = [string](Invoke-Native $git.Source @('log', '-1', '--format=%cI'))
        }
    }
    if ([string]::IsNullOrWhiteSpace($version)) { $version = 'dev' }
    if ([string]::IsNullOrWhiteSpace($commit)) { $commit = 'dev' }
    if ([string]::IsNullOrWhiteSpace($buildDate)) { $buildDate = 'unknown' }

    $go = Get-Command 'go' -ErrorAction Stop
    $ldflags = "-s -w -X github.com/Levango7/OpsMesh/internal/version.Version=$version -X github.com/Levango7/OpsMesh/internal/version.Commit=$commit -X github.com/Levango7/OpsMesh/internal/version.Date=$buildDate"
    Invoke-Native $go.Source @('build', '-trimpath', "-ldflags=$ldflags", '-o', $exePath, './cmd/opsmesh')
    Invoke-Native $exePath @('--version')
}
finally {
    Pop-Location
}
