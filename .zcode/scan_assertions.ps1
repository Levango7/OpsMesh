$ErrorActionPreference = 'SilentlyContinue'
Write-Output '=== 测试中断言含标记的字符串字面量 ==='
$tests = Get-ChildItem -Recurse -File -Include *_test.go | Where-Object { $_.FullName -notmatch 'node_modules' }
foreach ($f in $tests) {
    $m = Select-String -Path $f.FullName -Pattern 'P0-\d|P1-\d|P2-\d|task \d' -CaseSensitive |
        Where-Object { $_.Line -notmatch '^\s*//' -and $_.Line -match '"' }
    foreach ($x in $m) {
        '{0}:{1}: {2}' -f $f.Name, $x.LineNumber, $x.Line.Trim().Substring(0, [Math]::Min(110, $x.Line.Trim().Length))
    }
}
Write-Output ''
Write-Output '=== 含标记的文件名 ==='
git ls-files | Select-String -Pattern '(?i)(task\d|_p[012]|p0[0-9]|audit|security_p)' | Select-Object -First 20
Write-Output ''
Write-Output '=== 前端测试断言检查 ==='
$feTests = Get-ChildItem web\enterprise -Recurse -File -Include *.spec.js,*.test.js | Where-Object { $_.FullName -notmatch 'node_modules' }
foreach ($f in $feTests) {
    $m = Select-String -Path $f.FullName -Pattern 'P0-\d|P1-\d|P2-\d|task \d' -CaseSensitive |
        Where-Object { $_.Line -notmatch '^\s*//' }
    foreach ($x in $m) {
        '{0}:{1}: {2}' -f $f.Name, $x.LineNumber, $x.Line.Trim().Substring(0, [Math]::Min(110, $x.Line.Trim().Length))
    }
}
