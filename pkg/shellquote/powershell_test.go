package shellquote

// powershell_test.go PowerShell 嵌入工具的守卫。
//
// 语义保证分两层验：
//   1) 字符串层：QuotePowerShellSingle 的转义形状、PowerShellEncodedCommand 的
//      UTF-16LE+base64 往返与载荷字符集（[A-Za-z0-9+/=]，对 cmd/Go exec/PS 无特殊字符）；
//   2) 真 PS 层（Windows 本地实跑、Linux CI 自动 skip）：经生产同路径
//      （cmd /C）执行编码命令，敌意值必须按字面量往返、不得逃逸执行；
//      并用"未转义载荷确实能执行 MARKER"做校准，证明中和用例是承重的。

import (
	"encoding/base64"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestQuotePowerShellSingleShape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "''"},
		{"plain", "'plain'"},
		{"a'b", "'a''b'"},
		{"'; Write-Output x; '", "'''; Write-Output x; '''"},
		{`a"b`, `'a"b'`},
		{"%PATH%", "'%PATH%'"},
		{"a!b!", "'a!b!'"},
		{"$(1+1)", "'$(1+1)'"},
		{"`whoami`", "'`whoami`'"},
		{"line1\nline2", "'line1\nline2'"},
		{"中文值", "'中文值'"},
	}
	for _, c := range cases {
		if got := QuotePowerShellSingle(c.in); got != c.want {
			t.Errorf("QuotePowerShellSingle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPowerShellEncodedCommandRoundTrip(t *testing.T) {
	script := `Write-Output 'a''b'; $x = "q"; 中文; Get-Service | Select-Object Name`
	cmd := PowerShellEncodedCommand(script)
	const prefix = "powershell -EncodedCommand "
	if !strings.HasPrefix(cmd, prefix) {
		t.Fatalf("前缀错误: %s", cmd)
	}
	b64 := strings.TrimPrefix(cmd, prefix)
	if !regexp.MustCompile(`^[A-Za-z0-9+/=]+$`).MatchString(b64) {
		t.Fatalf("载荷含非 base64 字符（可能被 cmd/PS 重解析）: %q", b64)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	if got := string(utf16.Decode(u)); got != script {
		t.Fatalf("往返不等于原脚本:\n got=%q\nwant=%q", got, script)
	}
}

// psRun 经生产同路径（Go exec → cmd /C）执行编码命令，返回输出文本。
func psRun(t *testing.T, script string) string {
	t.Helper()
	out, _ := exec.Command("cmd", "/C", PowerShellEncodedCommand(script)+" 2>nul").CombinedOutput()
	return string(out)
}

func psMarkerRan(out string) bool {
	return strings.Contains(out, "\r\nMARKER") || strings.HasPrefix(out, "MARKER") || strings.Contains(out, "\nMARKER")
}

// TestPowerShellUnescapedBreakoutCalibration 校准：未转义的敌意载荷**必须**能
// 逃逸执行 MARKER——否则下面的中和用例是空过（证明不了任何事）。
func TestPowerShellUnescapedBreakoutCalibration(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell.exe 仅 Windows 可用（Linux CI 自动跳过）")
	}
	if !psMarkerRan(psRun(t, `Write-Output 'x'; Write-Output MARKER; ''`)) {
		t.Fatal("校准失败：未转义载荷未能在真 PS 下逃逸执行")
	}
}

// TestPowerShellValuesStayLiteralUnderRealPS 中和：经 QuotePowerShellSingle 的
// 敌意值在真 PS 下必须按字面量往返（含 $()、反引号、%VAR%、!、引号、结尾反斜杠）。
func TestPowerShellValuesStayLiteralUnderRealPS(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell.exe 仅 Windows 可用（Linux CI 自动跳过）")
	}
	samples := []string{
		`x'; Write-Output MARKER; '`,
		`a"b`,
		`%PATH%`,
		`!delayed!`,
		`$(1+1)`,
		"`whoami`",
		`tail\`,
		`p@ss%w!"ord`,
	}
	for _, v := range samples {
		out := psRun(t, `Write-Output `+QuotePowerShellSingle(v))
		if psMarkerRan(out) {
			t.Errorf("值 %q 在真 PS 下逃逸执行: %s", v, out)
		}
		if !strings.Contains(out, v) {
			t.Errorf("值 %q 未按字面量输出: %q", v, out)
		}
	}
}
