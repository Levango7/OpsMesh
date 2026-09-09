package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// newMemStore 创建一个带有固定 HMAC 密钥的 MemoryStore（测试用，避免随机密钥导致不可复现）。
func newMemStore() *MemoryStore {
	m := NewMemoryStore()
	m.SetSecret("test-provision-secret-fixed-for-testing")
	return m
}

// TestIssueToken_GeneratesValidToken 验证 IssueToken 签发格式正确（HMAC.payload）且可 Consume。
func TestIssueToken_GeneratesValidToken(t *testing.T) {
	m := newMemStore()
	tok, err := m.IssueToken("dev-127.0.0.1", "tenant-a", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken 不应报错: %v", err)
	}
	if !strings.Contains(tok, ".") {
		t.Fatalf("token 应包含 '.' 分隔符，got %q", tok)
	}
	// Consume 应成功
	deviceID, tenantID, ok := m.ConsumeToken(tok)
	if !ok {
		t.Fatalf("ConsumeToken 应成功，got ok=false")
	}
	if deviceID != "dev-127.0.0.1" {
		t.Fatalf("deviceID 应为 dev-127.0.0.1，got %s", deviceID)
	}
	if tenantID != "tenant-a" {
		t.Fatalf("tenantID 应为 tenant-a，got %s", tenantID)
	}
	// 一次性：再次 Consume 应失败
	_, _, ok2 := m.ConsumeToken(tok)
	if ok2 {
		t.Fatal("一次性 token 二次 Consume 应失败")
	}
}

// TestIssueToken_EmptyDeviceID 验证 deviceID 为空时返回错误。
func TestIssueToken_EmptyDeviceID(t *testing.T) {
	m := newMemStore()
	_, err := m.IssueToken("", "tenant-a", 15*time.Minute)
	if err == nil {
		t.Fatal("deviceID 为空应返回错误")
	}
	if !strings.Contains(err.Error(), "deviceID required") {
		t.Fatalf("错误信息应提及 deviceID required，got %v", err)
	}
}

// TestIssueToken_PipeRejection 验证 deviceID/tenantID 含 | 字符时被拒绝（F15 解析歧义防护）。
func TestIssueToken_PipeRejection(t *testing.T) {
	m := newMemStore()
	_, err := m.IssueToken("dev|evil", "tenant-a", 15*time.Minute)
	if err == nil {
		t.Fatal("deviceID 含 | 应被拒绝")
	}
	if !strings.Contains(err.Error(), "|") {
		t.Fatalf("错误信息应提及 | 字符，got %v", err)
	}
	// tenantID 含 | 也应拒绝
	_, err = m.IssueToken("dev-1", "tenant|a", 15*time.Minute)
	if err == nil {
		t.Fatal("tenantID 含 | 应被拒绝")
	}
}

// TestIssueToken_ExpiresAfterTTL 验证 token 过期后 Consume 返回 ok=false。
func TestIssueToken_ExpiresAfterTTL(t *testing.T) {
	m := newMemStore()
	tok, err := m.IssueToken("dev-1", "tenant-a", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("IssueToken 不应报错: %v", err)
	}
	// 等待过期
	time.Sleep(5 * time.Millisecond)
	_, _, ok := m.ConsumeToken(tok)
	if ok {
		t.Fatal("过期 token 应 Consume 失败")
	}
}

// TestConsumeToken_InvalidMAC 验证伪造 token（错误 HMAC 签名）被拒绝。
func TestConsumeToken_InvalidMAC(t *testing.T) {
	m := newMemStore()
	// 构造一个看起来合法但签名错误的 token
	badTok := "invalidsig.payload-with-no-dot-split-malformed"
	_, _, ok := m.ConsumeToken(badTok)
	if ok {
		t.Fatal("伪造 token 应 Consume 失败")
	}
}

// TestConsumeToken_UnknownToken 验证未签发的 token 被拒绝。
func TestConsumeToken_UnknownToken(t *testing.T) {
	m := newMemStore()
	_, _, ok := m.ConsumeToken("deadbeef.payload")
	if ok {
		t.Fatal("未知 token 应 Consume 失败")
	}
}

// TestConsumeToken_AlreadyConsumed 验证已消费的 token 二次消费被拒绝（一次性语义）。
func TestConsumeToken_AlreadyConsumed(t *testing.T) {
	m := newMemStore()
	tok, err := m.IssueToken("dev-1", "tenant-a", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken 不应报错: %v", err)
	}
	_, _, ok1 := m.ConsumeToken(tok)
	if !ok1 {
		t.Fatal("首次 Consume 应成功")
	}
	_, _, ok2 := m.ConsumeToken(tok)
	if ok2 {
		t.Fatal("已消费的 token 二次 Consume 应失败（一次性语义）")
	}
}

// TestConsumeToken_TamperedPayload 验证 payload 被篡改后 HMAC 校验失败。
func TestConsumeToken_TamperedPayload(t *testing.T) {
	m := newMemStore()
	tok, err := m.IssueToken("dev-1", "tenant-a", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken 不应报错: %v", err)
	}
	// 篡改 payload 部分（保留签名前缀，修改设备 ID）
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		t.Fatal("token 格式异常")
	}
	tampered := parts[0] + ".dev-X|tenant-a|1234|nonce"
	_, _, ok := m.ConsumeToken(tampered)
	if ok {
		t.Fatal("篡改 payload 的 token 应 Consume 失败（HMAC 校验）")
	}
}

// TestProvisionStore_IssueToken_NilSecretFallback 验证未注入密钥时 issueTokenLocked 自动随机生成（不 panic）。
func TestProvisionStore_IssueToken_NilSecretFallback(t *testing.T) {
	m := NewMemoryStore() // 未 SetSecret，secret 为空
	tok, err := m.IssueToken("dev-1", "tenant-a", 15*time.Minute)
	if err != nil {
		t.Fatalf("空密钥兜底随机生成不应报错: %v", err)
	}
	if tok == "" {
		t.Fatal("应签发非空 token")
	}
	//  Consume 应成功（密钥在 issueTokenLocked 内部已固化）
	_, _, ok := m.ConsumeToken(tok)
	if !ok {
		t.Fatal("空密钥兜底签发的 token 应可消费")
	}
}

// TestProvisionStore_MultipleTokensIndependent 验证多个 token 独立消费（一个消费不影响其他）。
func TestProvisionStore_MultipleTokensIndependent(t *testing.T) {
	m := newMemStore()
	tok1, err := m.IssueToken("dev-1", "tenant-a", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken tok1 不应报错: %v", err)
	}
	tok2, err := m.IssueToken("dev-2", "tenant-a", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken tok2 不应报错: %v", err)
	}
	// 消费 tok1
	_, _, ok1 := m.ConsumeToken(tok1)
	if !ok1 {
		t.Fatal("tok1 应消费成功")
	}
	// tok2 仍应可消费
	_, _, ok2 := m.ConsumeToken(tok2)
	if !ok2 {
		t.Fatal("tok2 应仍可消费（独立性）")
	}
	// tok1 不可再消费
	_, _, ok3 := m.ConsumeToken(tok1)
	if ok3 {
		t.Fatal("tok1 已消费，不应再消费")
	}
}

// TestConsumeToken_ErrorCases 验证 ConsumeToken 在各类异常输入下的鲁棒性（不 panic）。
func TestConsumeToken_ErrorCases(t *testing.T) {
	m := newMemStore()
	for _, tc := range []string{"", "invalid", ".", "prefix.", ".suffix", "a.b.c"} {
		_, _, _ = m.ConsumeToken(tc) // 不应 panic
	}
}

// TestProvisionStore_IssueToken_ErrorVsOK 验证 IssueToken 的错误与成功分支都能正确返回。
func TestProvisionStore_IssueToken_ErrorVsOK(t *testing.T) {
	m := newMemStore()
	_, err := m.IssueToken("", "tenant-a", 15*time.Minute)
	if err == nil {
		t.Fatal("空 deviceID 应返回错误而非 nil")
	}
	if !errors.Is(err, errors.New("deviceID required")) && !strings.Contains(err.Error(), "deviceID required") {
		t.Fatalf("错误应提及 deviceID required，got %v", err)
	}
	// 正常签发
	tok, err := m.IssueToken("dev-1", "tenant-a", 15*time.Minute)
	if err != nil {
		t.Fatalf("正常签发不应报错，got %v", err)
	}
	if tok == "" {
		t.Fatal("正常签发应返回非空 token")
	}
}
