// powershell.go PowerShell 脚本的安全构造与嵌入工具。
//
// 背景（2026-11-06 本机对生产路径的实测，取证与探针见
// docs/opsmesh-authz-audit-2026-11-04.md §3.6）：Windows agent 的脚本由
// `cmd /C` 执行（internal/agent/agent.go 的 shellCommand），且 Go 的 exec
// 参数转义会改写脚本里的引号——`powershell -Command "..."` 形式在这种路径下
// 内层引号会退化成 PS 字符串字面量（脚本被回显、根本不执行）；cmd 层本身
// 也没有引号内转义语法（`\"` 里的 `"` 照样闭合引号、`%VAR%` 引号内仍展开）。
//
// 唯一同时满足三层的嵌入方式是 `powershell -EncodedCommand`（UTF-16LE +
// base64）：载荷字符集仅 [A-Za-z0-9+/=]，对 Go exec / cmd / PowerShell 三层
// 都无特殊字符可被重解析；脚本内部的值则一律用 QuotePowerShellSingle
// 转义单引号。registry 等 cmd 原生语境仍用 ValidateCmdQuoted（拒绝式校验）。
package shellquote

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// QuotePowerShellSingle 把值转成 PowerShell 单引号字面量（' → ”，外层补 '...'）。
//
// PS 单引号串内除 ' 之外一切字符都是字面量：$、反引号、换行、双引号、% 都不展开。
// 因此"值 → 单引号字面量"是 PS 层唯一需要的处理；cmd/传输层的安全由
// PowerShellEncodedCommand 的 base64 载荷保证。
func QuotePowerShellSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// PowerShellEncodedCommand 把 PS 脚本包成 `powershell -EncodedCommand <base64>`。
//
// script 按 PowerShell 对该参数的规定编码：UTF-16LE（小端）后 base64。
// 返回值是完整的命令前缀（不含 ` 2>nul`、`|| echo Failed` 一类 cmd 尾巴，
// 那些由调用方按语境拼接）。
func PowerShellEncodedCommand(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return "powershell -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}
