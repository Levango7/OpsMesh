package model

import "testing"

// TestRequireSecretReference 钉住「secrets 只存引用」的形状契约（TD-88 方向裁定）。
func TestRequireSecretReference(t *testing.T) {
	ok := []string{
		"${vault:notify/dingtalk#secret}",
		"${env:OPSMESH_ALERT_TOKEN}",
		"${file:/run/secrets/pass}",
		"${a}",
	}
	for _, v := range ok {
		if err := RequireSecretReference(v); err != nil {
			t.Errorf("引用 %q 应被接受, got %v", v, err)
		}
	}
	bad := []string{
		"plain-password",
		"",       // 空值也是明文语义（未配置）——拒绝会迫使调用方显式删键而不是写空
		"${",     // 半截引用
		"a${b}c", // 必须整串包裹
		"https://oapi.dingtalk.com/robot/send?access_token=abc",
	}
	for _, v := range bad {
		if err := RequireSecretReference(v); err == nil {
			t.Errorf("非引用值 %q 应被拒绝（TD-88 只存引用）", v)
		}
	}
}
