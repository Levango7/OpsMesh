$ErrorActionPreference = 'SilentlyContinue'
Write-Output '=== docs 中 P0/P1/P2 上下文样本（查故障等级等合法用法）==='
$docs = Get-ChildItem docs -File -Filter *.md
foreach ($f in $docs) {
    $m = Select-String -Path $f.FullName -Pattern 'P[012]' -CaseSensitive
    foreach ($x in $m) {
        if ($x.Line -match '故障|等级|级别|severity|告警级别|事件') {
            '{0}:{1}: {2}' -f $f.Name, $x.LineNumber, $x.Line.Trim().Substring(0, [Math]::Min(100, $x.Line.Trim().Length))
        }
    }
}
Write-Output ''
Write-Output '=== 大写裸 A/B/E/H+数字 后接中文 的样本（验证非审计用法）==='
$files = Get-ChildItem -Recurse -File -Include *.go,*.md,*.js,*.vue,*.yaml | Where-Object { $_.FullName -notmatch 'node_modules|\\\.git\\|\\dist\\' }
$n = 0
foreach ($f in $files) {
    $m = Select-String -Path $f.FullName -Pattern '\b(?:A|B|E|H)\d{1,2}\b(?=\s*[\u4e00-\u9fff：:）)])' -CaseSensitive
    foreach ($x in $m) {
        $n++
        if ($n % 40 -eq 1) {
            '{0}:{1}: {2}' -f $f.Name, $x.LineNumber, $x.Line.Trim().Substring(0, [Math]::Min(100, $x.Line.Trim().Length))
        }
    }
}
Write-Output ('total bare-audit-CN matches: ' + $n)
