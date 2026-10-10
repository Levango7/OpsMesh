// secret_crypto_test.go 机密加密的**格式兼容 + 语义**回归（TD-88 前置：原语下沉 pkg/secretcrypto）。
//
// 本文件保留一份**旧实现的逐字副本**（legacyEncrypt/legacyDecrypt，仅测试用），用于把
// 「下沉后格式与旧实现逐字节一致」变成常驻断言——这是规格 §8 列为「不可跳过」的回滚保险：
// 格式一旦漂移，config-svc 已落库的密文就读不出来了，而那种事故只在**升级后的真库**上暴露。
// 副本刻意留在 *_test.go 里（实现守卫 no_dup_impl_test.go 只扫非测试文件），
// 既保住证据、又不构成「第二份实现」。
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/pkg/secretcrypto"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
)

// ---- 旧实现副本（下沉前 store.go 的 encryptSecret/decryptSecret，逐字保留） ----

const legacyPrefix = "enc:v1:"

func legacyEncrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return legacyPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

func legacyDecrypt(key []byte, stored string) (string, bool, error) {
	raw, ok := strings.CutPrefix(stored, legacyPrefix)
	if !ok {
		return stored, true, nil
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", false, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", false, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false, err
	}
	if len(data) < gcm.NonceSize() {
		return "", false, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", false, err
	}
	return string(out), false, nil
}

// TestCrossCompatWithLegacyImplementation 规格 §8 的「不可跳过」保险：
// 旧实现产出的密文必须能被新包解开，新包产出的密文必须能被旧实现解开（双向逐字节兼容）。
func TestCrossCompatWithLegacyImplementation(t *testing.T) {
	key := deriveKey("cross-compat-key")
	const plain = "s3cr3t-跨实现验证"

	// 方向 A：旧加密 → 新解密
	oldEnc, err := legacyEncrypt(key, plain)
	if err != nil {
		t.Fatalf("legacyEncrypt: %v", err)
	}
	if !secretcrypto.HasPrefix(oldEnc) {
		t.Fatalf("旧实现产出的密文前缀与新包不一致: %q", oldEnc)
	}
	got, legacy, err := secretcrypto.Decrypt(key, oldEnc)
	if err != nil || legacy || got != plain {
		t.Fatalf("新包解旧密文 = (%q legacy=%v err=%v), want (%q false nil)", got, legacy, err, plain)
	}

	// 方向 B：新加密 → 旧解密
	newEnc, err := secretcrypto.Encrypt(key, plain)
	if err != nil {
		t.Fatalf("secretcrypto.Encrypt: %v", err)
	}
	got2, legacy2, err := legacyDecrypt(key, newEnc)
	if err != nil || legacy2 || got2 != plain {
		t.Fatalf("旧实现解新密文 = (%q legacy=%v err=%v), want (%q false nil)", got2, legacy2, err, plain)
	}
}

// TestSecretCryptoRoundTripAndPrefix 往返 + 前缀 + 随机 nonce（共享原语语义）。
func TestSecretCryptoRoundTripAndPrefix(t *testing.T) {
	key := deriveKey("unit-test-key")

	enc, err := secretcrypto.Encrypt(key, "s3cr3t-值")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !secretcrypto.HasPrefix(enc) {
		t.Fatalf("密文缺少版本前缀 %q: %q", secretcrypto.Prefix, enc)
	}
	if strings.Contains(enc, "s3cr3t") {
		t.Fatal("密文里出现了明文片段")
	}
	plain, legacy, err := secretcrypto.Decrypt(key, enc)
	if err != nil || legacy || plain != "s3cr3t-值" {
		t.Fatalf("解密 = (%q legacy=%v err=%v)", plain, legacy, err)
	}
	enc2, err := secretcrypto.Encrypt(key, "s3cr3t-值")
	if err != nil {
		t.Fatalf("Encrypt(2): %v", err)
	}
	if enc == enc2 {
		t.Fatal("同一明文两次加密结果相同——nonce 不是随机的")
	}
}

// TestSecretCryptoLegacyAndFailurePaths 两条判据的边界：历史明文放行 / 解不开硬失败。
func TestSecretCryptoLegacyAndFailurePaths(t *testing.T) {
	key := deriveKey("unit-test-key")

	plain, legacy, err := secretcrypto.Decrypt(key, "legacy-plaintext")
	if err != nil || !legacy || plain != "legacy-plaintext" {
		t.Fatalf("无前缀值返回 = (%q, legacy=%v, err=%v), want 原样放行且 legacy", plain, legacy, err)
	}
	enc, err := secretcrypto.Encrypt(key, "value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, _, err := secretcrypto.Decrypt(deriveKey("another-key"), enc); err == nil {
		t.Fatal("错密钥解密应报错，却成功了")
	}
	tampered := enc[:len(enc)-2] + "AB"
	if _, _, err := secretcrypto.Decrypt(key, tampered); err == nil {
		t.Fatal("被篡改的密文解密应报错（GCM 认证失败），却成功了")
	}
	if _, _, err := secretcrypto.Decrypt(key, secretcrypto.Prefix+"not-base64!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	if _, _, err := secretcrypto.Decrypt(key, secretcrypto.Prefix+"AA=="); err == nil {
		t.Fatal("长度不足的密文应报错")
	}
}

// TestMemoryStoreSecretEncryptedAtRest 内存后端也走共享原语：存储值是带前缀密文，对外读回明文。
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
	if !secretcrypto.HasPrefix(stored.Value) {
		t.Fatalf("内存后端存储值未加密（或格式与共享原语不一致）: %q", stored.Value)
	}
	got, ok := s.GetSecret("t1", "k1")
	if !ok || got.Value != "plain" {
		t.Fatalf("读回 = %+v, want 明文 plain", got)
	}
}
