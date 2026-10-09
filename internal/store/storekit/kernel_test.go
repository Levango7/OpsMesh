// kernel_test.go 内核函数行为回归（自 internal/store/store_extra_test.go 迁出，TD-61 批次 3-sql）。
package storekit

import (
	"strings"
	"testing"
)

// TestBcryptHash_Default 验证 BcryptHash 默认能正常哈希。
func TestBcryptHash_Default(t *testing.T) {
	hash, err := BcryptHash("password")
	if err != nil {
		t.Fatalf("BcryptHash 失败: %v", err)
	}
	if hash == "" || hash == "password" {
		t.Fatal("BcryptHash 应返回非空哈希")
	}
	// 相同密码哈希不同（bcrypt 含盐）
	hash2, _ := BcryptHash("password")
	if hash == hash2 {
		t.Fatal("bcrypt 哈希应含盐，两次结果不应相同")
	}
}

// TestRandHex_Default 验证 RandHex 返回非空十六进制串。
func TestRandHex_Default(t *testing.T) {
	s := RandHex(16)
	if s == "" {
		t.Fatal("RandHex 应返回非空串")
	}
	// 长度 = 2*n（每字节 2 个 hex 字符）
	if len(s) != 32 {
		t.Fatalf("RandHex(16) len = %d, want 32", len(s))
	}
	// 不同调用结果不同（极大概率）
	a, b := RandHex(16), RandHex(16)
	if a == b {
		t.Fatal("两次 RandHex 不应相同")
	}
}

// TestMustRandHex_Default 验证 MustRandHex 返回非空十六进制串。
func TestMustRandHex_Default(t *testing.T) {
	s := MustRandHex(32)
	if s == "" {
		t.Fatal("MustRandHex 应返回非空串")
	}
	if len(s) != 64 {
		t.Fatalf("MustRandHex(32) len = %d, want 64", len(s))
	}
}

// TestRandAlertRuleID_Default 验证 RandAlertRuleID 返回带前缀的 ID。
func TestRandAlertRuleID_Default(t *testing.T) {
	id := RandAlertRuleID()
	if !strings.HasPrefix(id, "alert-rule-") {
		t.Fatalf("RandAlertRuleID = %q, want prefix alert-rule-", id)
	}
}

// TestHashToken_Deterministic_Extra 验证 HashToken 确定性。
func TestHashToken_Deterministic_Extra(t *testing.T) {
	a := HashToken("token-x")
	b := HashToken("token-x")
	if a != b {
		t.Fatal("HashToken 应确定性")
	}
	if HashToken("a") == HashToken("b") {
		t.Fatal("HashToken 不同输入应不同输出")
	}
}

// TestVerifyTokenMAC_EdgeCases 验证 VerifyTokenMAC 边界。
func TestVerifyTokenMAC_EdgeCases(t *testing.T) {
	if VerifyTokenMAC("", "x") {
		t.Fatal("空 secret 应返回 false")
	}
	if VerifyTokenMAC("s", "") {
		t.Fatal("空 token 应返回 false")
	}
	if VerifyTokenMAC("s", "no-dot") {
		t.Fatal("无分隔符应返回 false")
	}
	if VerifyTokenMAC("s", ".payload") {
		t.Fatal("空签名应返回 false")
	}
	if VerifyTokenMAC("s", "sig.") {
		t.Fatal("空 payload 应返回 false")
	}
}
