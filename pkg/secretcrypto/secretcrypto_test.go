package secretcrypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, keyLen)
	for i := range k {
		k[i] = byte(i * 7)
	}
	return k
}

// TestEncryptDecryptRoundTrip 往返一致 + 输出形态（带前缀、非明文、随机 nonce）。
func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)

	enc, err := Encrypt(key, "kubeconfig-内容: apiVersion: v1")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !HasPrefix(enc) {
		t.Fatalf("密文缺少前缀 %q: %q", Prefix, enc)
	}
	if strings.Contains(enc, "apiVersion") {
		t.Fatal("密文里出现了明文片段")
	}
	pt, legacy, err := Decrypt(key, enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if legacy {
		t.Fatal("本进程刚加密的值不应被判为 legacy 明文存量")
	}
	if pt != "kubeconfig-内容: apiVersion: v1" {
		t.Fatalf("解密 = %q, want 原文（含中文）", pt)
	}
	// 随机 nonce：同一明文两次加密结果必须不同。
	enc2, err := Encrypt(key, "kubeconfig-内容: apiVersion: v1")
	if err != nil {
		t.Fatalf("Encrypt(2): %v", err)
	}
	if enc == enc2 {
		t.Fatal("同一明文两次加密结果相同——nonce 不是随机的")
	}
}

// TestEncryptEmptyPassthroughAndEmptyStored 空值语义：空明文不加密；空值解密原样返回。
func TestEncryptEmptyPassthroughAndEmptyStored(t *testing.T) {
	key := testKey(t)
	enc, err := Encrypt(key, "")
	if err != nil || enc != "" {
		t.Fatalf("空明文应透传空串, got %q err=%v", enc, err)
	}
	pt, legacy, err := Decrypt(key, "")
	if err != nil || pt != "" || legacy {
		t.Fatalf("空值解密应返回空串且非 legacy, got %q legacy=%v err=%v", pt, legacy, err)
	}
}

// TestDecryptUnprefixedIsLegacyPlaintext 无前缀 ⇒ legacy 明文放行（升级前存量）。
func TestDecryptUnprefixedIsLegacyPlaintext(t *testing.T) {
	key := testKey(t)
	pt, legacy, err := Decrypt(key, "plain-secret-value")
	if err != nil {
		t.Fatalf("无前缀应放行, got err=%v", err)
	}
	if !legacy || pt != "plain-secret-value" {
		t.Fatalf("无前缀返回 = (%q, legacy=%v), want (原样, true)", pt, legacy)
	}
}

// TestDecryptHardFailsOnBrokenCiphertext 带前缀但解不开 ⇒ 必须硬失败（不得回退成明文）。
// 这是本包的核心红线：密钥不匹配/数据损坏与「历史明文」是两回事。
func TestDecryptHardFailsOnBrokenCiphertext(t *testing.T) {
	key := testKey(t)
	enc, err := Encrypt(key, "value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// 错密钥
	if _, _, err := Decrypt(testKey2(t), enc); err == nil {
		t.Fatal("错密钥解密应报错，却成功了（红线破了）")
	}
	// 篡改密文一个字符（尾字符换掉，破坏 GCM tag）
	tampered := enc[:len(enc)-2] + "AB"
	if _, _, err := Decrypt(key, tampered); err == nil {
		t.Fatal("被篡改的密文解密应报错（GCM 验签），却成功了")
	}
	// 前缀在但不是合法 base64
	if _, _, err := Decrypt(key, Prefix+"not-base64!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	// 前缀在但长度不足以容纳 nonce
	if _, _, err := Decrypt(key, Prefix+"AA=="); err == nil {
		t.Fatal("长度不足的密文应报错")
	}
}

// TestDecryptLegacyUnprefixed 历史「无前缀密文」专用解密：能解 ⇒ ok；不能解 ⇒ 不误吞。
func TestDecryptLegacyUnprefixed(t *testing.T) {
	key := testKey(t)

	// 造一份「旧实现」形态的密文：base64(nonce||ct)，不带前缀。
	enc, err := Encrypt(key, "legacy-ciphertext-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	legacyCipher := strings.TrimPrefix(enc, Prefix)
	pt, ok := DecryptLegacyUnprefixed(key, legacyCipher)
	if !ok || pt != "legacy-ciphertext-value" {
		t.Fatalf("旧形态密文应能解出, got (%q, ok=%v)", pt, ok)
	}

	// 真 legacy 明文（非 base64 或解不开）⇒ ok=false，不得误吞。
	if _, ok := DecryptLegacyUnprefixed(key, "just-plain-text"); ok {
		t.Fatal("明文被误判为旧密文（会把它当密文解密失败后丢掉）")
	}
	if _, ok := DecryptLegacyUnprefixed(key, "aGVsbG8="); ok { // base64("hello") 但不是有效 GCM 密文
		t.Fatal("短 base64 被误判为旧密文")
	}
	// 带前缀的值不属于本函数职责
	if _, ok := DecryptLegacyUnprefixed(key, enc); ok {
		t.Fatal("带前缀的值不应由 DecryptLegacyUnprefixed 处理")
	}
	// 空值
	if _, ok := DecryptLegacyUnprefixed(key, ""); ok {
		t.Fatal("空值不应判为旧密文")
	}
}

// TestWrongKeyLength 非 32 字节密钥 ⇒ 报错（不静默降级）。
func TestWrongKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33} {
		k := make([]byte, n)
		if _, err := Encrypt(k, "x"); err == nil {
			t.Errorf("密钥长度 %d：Encrypt 应报错", n)
		}
		if _, _, err := Decrypt(k, Prefix+base64.StdEncoding.EncodeToString(make([]byte, 40))); err == nil {
			t.Errorf("密钥长度 %d：Decrypt 应报错", n)
		}
	}
}

func testKey2(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, keyLen)
	for i := range k {
		k[i] = byte(255 - i*3)
	}
	return k
}
