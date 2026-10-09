package store

import (
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
)

// TestSecretCryptoRoundTrip 覆盖两后端共用的机密加解密原语（TD-65 补测发现的缺陷回归）。
//
// 背景：config-svc 的 MySQL 后端此前把 secret 原样落库（构造函数接了 encryptionKey 但从未使用），
// 而内存后端是加密的——同一份配置、两个后端、两种安全语义。本测试锁住原语本身的契约，
// 集成测试（mysql_integration_test.go）锁住「真的加密落库」。
func TestSecretCryptoRoundTrip(t *testing.T) {
	key := deriveKey("unit-test-key")

	enc, err := encryptSecret(key, "s3cr3t-值")
	if err != nil {
		t.Fatalf("encryptSecret: %v", err)
	}
	if !strings.HasPrefix(enc, secretCipherPrefix) {
		t.Fatalf("密文缺少版本前缀 %q: %q", secretCipherPrefix, enc)
	}
	if strings.Contains(enc, "s3cr3t") {
		t.Fatal("密文里出现了明文片段")
	}

	plain, legacy, err := decryptSecret(key, enc)
	if err != nil {
		t.Fatalf("decryptSecret: %v", err)
	}
	if legacy {
		t.Fatal("本进程刚加密的值不应被判为历史明文存量")
	}
	if plain != "s3cr3t-值" {
		t.Fatalf("解密 = %q, want 原文（含非 ASCII 字符）", plain)
	}

	// 随机 nonce：同一明文两次加密结果必须不同（否则等于确定性加密，泄漏「值是否相同」）。
	enc2, err := encryptSecret(key, "s3cr3t-值")
	if err != nil {
		t.Fatalf("encryptSecret(2): %v", err)
	}
	if enc == enc2 {
		t.Fatal("同一明文两次加密结果相同——nonce 不是随机的")
	}
}

// TestSecretCryptoLegacyAndFailurePaths 锁住两条判据的边界：
//  1. 无前缀的值 = 升级前的历史明文存量 ⇒ 原样返回并标记 legacy（升级不能打断老数据）；
//  2. 有前缀但解不开（密钥不匹配/被篡改）⇒ 必须报错，调用方据此硬失败，
//     绝不能把密文当明文交给调用方。
func TestSecretCryptoLegacyAndFailurePaths(t *testing.T) {
	key := deriveKey("unit-test-key")

	plain, legacy, err := decryptSecret(key, "legacy-plaintext")
	if err != nil {
		t.Fatalf("无前缀的值应原样放行，却报错: %v", err)
	}
	if !legacy || plain != "legacy-plaintext" {
		t.Fatalf("无前缀值返回 = (%q, legacy=%v), want (legacy-plaintext, legacy=true)", plain, legacy)
	}

	enc, err := encryptSecret(key, "value")
	if err != nil {
		t.Fatalf("encryptSecret: %v", err)
	}
	// 错密钥
	if _, _, err := decryptSecret(deriveKey("another-key"), enc); err == nil {
		t.Fatal("错密钥解密应报错，却成功了")
	}
	// 篡改密文体（翻掉 base64 尾字符）
	tampered := enc[:len(enc)-2] + "AB"
	if _, _, err := decryptSecret(key, tampered); err == nil {
		t.Fatal("被篡改的密文解密应报错（GCM 认证失败），却成功了")
	}
	// 前缀在但内容不是合法 base64
	if _, _, err := decryptSecret(key, secretCipherPrefix+"not-base64!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	// 前缀在但长度不足以容纳 nonce
	if _, _, err := decryptSecret(key, secretCipherPrefix+"AA=="); err == nil {
		t.Fatal("长度不足的密文应报错")
	}
}

// TestMemoryStoreSecretEncryptedAtRest 验证内存后端也走同一对原语：
// 写入后内部存储的值必须是带前缀的密文，而对外读取仍返回明文。
func TestMemoryStoreSecretEncryptedAtRest(t *testing.T) {
	s := NewMemoryStore("unit-test-key", 10)
	created := s.CreateSecret(&models.SecretEntry{TenantID: "t1", Key: "k1", Value: "plain", KeyType: "aes"})
	if created == nil {
		t.Fatal("CreateSecret 返回 nil")
	}
	stored := s.secrets[configKey("t1", "k1")]
	if stored == nil {
		t.Fatal("内部存储里没有该机密")
	}
	if !strings.HasPrefix(stored.Value, secretCipherPrefix) {
		t.Fatalf("内存后端存储值未加密（或格式与 MySQL 后端不一致）: %q", stored.Value)
	}
	got, ok := s.GetSecret("t1", "k1")
	if !ok || got.Value != "plain" {
		t.Fatalf("读回 = %+v, want 明文 plain", got)
	}
}
