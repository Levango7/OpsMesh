// password.go — A2 口令策略（与 controlplane auth.go:614-641 逐字一致）。
package http

// validateStrongPassword 强口令校验：≥8 字符 + 大写 + 小写 + 数字。
// 返回不满足时的可读提示（满足返回空串）。与 controlplane 同规则集。
func validateStrongPassword(pw string) string {
	const minLen = 8
	if len(pw) < minLen {
		return "password too short (min 8 chars)"
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range pw {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	if !hasUpper {
		return "password must contain at least one uppercase letter"
	}
	if !hasLower {
		return "password must contain at least one lowercase letter"
	}
	if !hasDigit {
		return "password must contain at least one digit"
	}
	return ""
}
