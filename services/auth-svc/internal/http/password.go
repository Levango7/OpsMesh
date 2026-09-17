// password.go — A2 口令策略（与 controlplane auth.go:614-641 逐字一致 + TD-60 增强）。
//
// TD-60 安全增强：从 8 字符 + 大小写 + 数字 提升到可配置强策略：
//   - 最小长度 12（默认，可配置）
//   - 大写 + 小写 + 数字 + 特殊字符（四类必含）
//   - 常见密码黑名单（top-N 可配置，默认含 top-100 常见弱口令）
//
// 向后兼容：validateStrongPassword 保留原签名，内部调用默认强策略。
// 测试环境可通过 PasswordPolicy 降低要求（如 minLen=8、requireSpecial=false）。
package http

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// PasswordPolicy 定义口令强度策略（可配置）。
//
// 零值 Policy（&PasswordPolicy{}）等价于默认强策略：
// MinLen=12, RequireUpper=true, RequireLower=true, RequireDigit=true,
// RequireSpecial=true, Blacklist=defaultCommonPasswords。
type PasswordPolicy struct {
	MinLen         int      // 最小长度（默认 12）
	RequireUpper   bool     // 要求大写字母（默认 true）
	RequireLower   bool     // 要求小写字母（默认 true）
	RequireDigit   bool     // 要求数字（默认 true）
	RequireSpecial bool     // 要求特殊字符（默认 true）
	Blacklist      []string // 常见密码黑名单（默认 defaultCommonPasswords）
}

// defaultPasswordPolicy 返回生产默认强策略。
func defaultPasswordPolicy() *PasswordPolicy {
	return &PasswordPolicy{
		MinLen:         12,
		RequireUpper:   true,
		RequireLower:   true,
		RequireDigit:   true,
		RequireSpecial: true,
		Blacklist:      defaultCommonPasswords,
	}
}

// defaultCommonPasswords 内置 top-100 常见弱口令（摘自 SecLists top-100 + 运维常见）。
// 命中黑名单的口令即使满足复杂度规则也拒绝（防字典攻击）。
var defaultCommonPasswords = []string{
	"123456", "password", "123456789", "12345678", "12345", "1234567",
	"1234567890", "qwerty", "abc123", "111111", "123123", "admin",
	"letmein", "monkey", "1234", "dragon", "master", "666666",
	"!@#$%^&*", "aaa111", "trustno1", "welcome", "shadow", "superman",
	"michael", "football", "baseball", "iloveyou", "sunshine", "princess",
	"charlie", "donald", "password1", "qwerty123", "welcome1", "admin123",
	"letmein1", "changeme", "P@ssw0rd", "Passw0rd", "Password1",
	"1q2w3e4r", "qwerty1", "123qwe", "1qaz2wsx", "zxcvbnm", "asdfghjkl",
	"000000", "654321", "123123123", "abcdef", "abcdefg", "abcdef123",
	"test", "test123", "guest", "guest123", "root", "root123",
	"pass", "pass123", "pass1234", "123456a", "123456abc", "abc12345",
	"a1b2c3d4", "1a2b3c4d", "q1w2e3r4", "z1x2c3v4", "11111111",
	"00000000", "88888888", "99999999", "77777777", "66666666",
	"55555555", "44444444", "33333333", "22222222", "11111111",
	"abcd1234", "1234abcd", "a123456", "aa123456", "aaa12345",
	"abc123abc", "password12", "password123", "qwerty12", "qwerty1234",
	"admin1234", "administrator", "administrator123", "letmein123",
	"welcome123", "changeme123", "P@ssword1", "Pa$$w0rd", "P@ssw0rd1",
	// 满足复杂度但常见的口令（仍须拒绝：复杂度规则不等于安全性）。
	"Welcome1!23", "Changeme1!23", "Password1!23", "Qwerty1!234",
	"Admin1!2345", "Letmein1!23", "Abc123!@#45", "Test1!234567",
}

// validateStrongPassword 强口令校验（默认强策略：12 字符 + 大小写 + 数字 + 特殊字符 + 黑名单）。
// 返回不满足时的可读提示（满足返回空串）。
//
// 向后兼容入口：内部调用 validatePasswordWithPolicy(pw, defaultPasswordPolicy())。
// 测试环境或需要宽松策略时，直接调用 validatePasswordWithPolicy 传入自定义 Policy。
func validateStrongPassword(pw string) string {
	return validatePasswordWithPolicy(pw, defaultPasswordPolicy())
}

// validatePasswordWithPolicy 按给定策略校验口令强度。
// 返回不满足时的可读提示（满足返回空串）。
//
// 策略字段零值时采用默认值（MinLen=12, Require*=true）。
// 黑名单匹配大小写不敏感（防 Password/Password/PASSWORD 绕过）。
func validatePasswordWithPolicy(pw string, policy *PasswordPolicy) string {
	if policy == nil {
		policy = defaultPasswordPolicy()
	}
	minLen := policy.MinLen
	if minLen <= 0 {
		minLen = 12
	}
	requireUpper := policy.RequireUpper || policy.MinLen == 0 && policy.RequireUpper
	// 简化：零值 Policy 等价默认（全部 true）。非零值时尊重显式设置。
	if policy.MinLen == 0 && !policy.RequireUpper && !policy.RequireLower && !policy.RequireDigit && !policy.RequireSpecial {
		requireUpper = true
		policy.RequireLower = true
		policy.RequireDigit = true
		policy.RequireSpecial = true
	} else {
		requireUpper = policy.RequireUpper
	}

	if len(pw) < minLen {
		return "password too short (min " + itoa(minLen) + " chars)"
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, c := range pw {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}
	if requireUpper && !hasUpper {
		return "password must contain at least one uppercase letter"
	}
	if policy.RequireLower && !hasLower {
		return "password must contain at least one lowercase letter"
	}
	if policy.RequireDigit && !hasDigit {
		return "password must contain at least one digit"
	}
	if policy.RequireSpecial && !hasSpecial {
		return "password must contain at least one special character"
	}

	// 黑名单校验：大小写不敏感匹配（防 Password/PASSWORD 绕过）。
	blacklist := policy.Blacklist
	if blacklist == nil {
		blacklist = defaultCommonPasswords
	}
	pwLower := strings.ToLower(pw)
	for _, bad := range blacklist {
		if pwLower == strings.ToLower(bad) {
			return "password is too common (found in blacklist)"
		}
	}

	return ""
}

// hashPasswordForBlacklist 计算口令的 SHA-256 摘要（用于大黑名单集合的 O(1) 查找）。
// 当前黑名单规模 ≤1000，线性扫描足够；超 1000 时可改用 map[hash]struct{}。
// 保留供未来扩展（如加载 top-1000 文件时预构建 hash 集合）。
func hashPasswordForBlacklist(pw string) string {
	h := sha256.Sum256([]byte(strings.ToLower(pw)))
	return hex.EncodeToString(h[:])
}

// itoa 轻量 int→string（避免引入 strconv 仅为此一处）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
