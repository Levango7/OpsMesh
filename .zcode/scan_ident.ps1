$ErrorActionPreference = 'SilentlyContinue'
Write-Output '=== task+数字 无空格形式（可能是标识符 task1）==='
$files = Get-ChildItem -Recurse -File -Include *.go | Where-Object { $_.FullName -notmatch '\\\.git\\|\.pb\.go$' }
foreach ($f in $files) {
    $m = Select-String -Path $f.FullName -Pattern 'task\d' -CaseSensitive
    foreach ($x in $m) {
        '{0}:{1}: {2}' -f $f.Name, $x.LineNumber, $x.Line.Trim().Substring(0, [Math]::Min(90, $x.Line.Trim().Length))
    }
}
Write-Output ''
Write-Output '=== 裸 P0/P1/P2 作为代码 token（非注释非字符串）==='
foreach ($f in $files) {
    $lines = Get-Content $f.FullName
    for ($i = 0; $i -lt $lines.Count; $i++) {
        $t = $lines[$i].Trim()
        if ($t -cmatch '^//' -or $t -cmatch '^\*') { continue }
        $code = $t -creplace '//.*$', ''
        $code = $code -creplace '"[^"]*"', '""'
        if ($code -cmatch '\bP[012]\b') {
            '{0}:{1}: {2}' -f $f.Name, ($i + 1), $code.Substring(0, [Math]::Min(90, $code.Length))
        }
    }
}
Write-Output '=== done ==='
