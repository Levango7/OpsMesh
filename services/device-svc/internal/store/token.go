package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// tokenMeta 是 install token 的元数据（一次性、限时，消费后标记 consumed）。
type tokenMeta struct {
	deviceID  string
	tenantID  string
	expiresAt time.Time
	consumed  bool
}

// ProvisionStore 是自动纳管 install token 的签发与消费接口。
//
// 语义（与 controlplane store.Provision/ConsumeToken 1:1 等价）：
//   - IssueToken 签发 HMAC 一次性、限时 token（payload 明文含设备/租户/过期/随机串）；
//     库存键为 token 的 SHA-256 摘要（明文不落库），防 DB 只读泄露。
//   - ConsumeToken 先验 HMAC 签名（防 DB 写权限伪造），再按摘要查找，
//     最后检查一次性+过期；通过后置 consumed=true（一次性）。
type ProvisionStore interface {
	// IssueToken 签发一个一次性 install token（ttl 为有效期）。deviceID 为空时返回错误。
	IssueToken(deviceID, tenantID string, ttl time.Duration) (token string, err error)
	// ConsumeToken 校验并消费 token：限时、未用过才返回设备与租户并置 consumed；否则 ok=false。
	ConsumeToken(token string) (deviceID, tenantID string, ok bool)
}

// === token 内部实现（与 controlplane internal/store/memory.go 1:1 等价） ===

// hashToken 对完整 token 取 SHA-256 摘要（hex）。
// 安全：库存/内存只存摘要，不存明文 token——DB 只读账号/备份泄露不等于活体 token 泄露。
func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// verifyTokenMAC 校验 token 的 HMAC 签名（F8 真正落地）：从 token 中提取 payload + 签名，
// 用 secret 重算 HMAC-SHA256 并与签名部分比较。防 DB 写权限伪造 token。
func verifyTokenMAC(secret, token string) bool {
	if secret == "" || token == "" {
		return false
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	sigHex, payload := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sigHex))
}

// mustRandHex 生成 n 字节的随机 hex 字符串（密钥用）。crypto/rand 不可用时 panic。
func mustRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("[store] crypto/rand 不可用，无法生成安全密钥: %v", err))
	}
	return hex.EncodeToString(b)
}

// randHex 生成 n 字节随机 hex 字符串（nonce 用）。crypto/rand 不可用时 panic。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("[store] crypto/rand 不可用: %v", err))
	}
	return hex.EncodeToString(b)
}

// issueTokenLocked 签发一个一次性 install token（HMAC(deviceID|tenantID|expiry|nonce)），
// 调用方须持 m.mu（写锁）。token 格式：hex(hmac) + "." + payload，payload 明文含设备/租户/过期/随机串。
// 库存键为 token 的 SHA-256 摘要（明文不落库）。
// deviceID/tenantID 含 | 字符时返回错误（F15 解析歧义防护）。
func (m *MemoryStore) issueTokenLocked(deviceID, tenantID string, ttl time.Duration) (string, error) {
	if deviceID == "" {
		return "", errors.New("deviceID required")
	}
	if strings.Contains(deviceID, "|") || strings.Contains(tenantID, "|") {
		return "", fmt.Errorf("deviceID 或 tenantID 含非法字符 |")
	}
	if len(m.secret) == 0 {
		m.secret = []byte(mustRandHex(32)) // 兜底，正常构造时已置随机密钥
	}
	nonce := randHex(16)
	expiresAt := time.Now().Add(ttl)
	payload := strings.Join([]string{tenantID, deviceID, strconv.FormatInt(expiresAt.Unix(), 10), nonce}, "|")
	mac := hmac.New(sha256.New, []byte(m.secret))
	mac.Write([]byte(payload))
	tok := hex.EncodeToString(mac.Sum(nil)) + "." + payload
	m.tokens[hashToken(tok)] = &tokenMeta{deviceID: deviceID, tenantID: tenantID, expiresAt: expiresAt}
	return tok, nil
}

// SetSecret 注入 HMAC 密钥（建议构造时从 config.ProvisionSecret 读取；空则 issueTokenLocked 自动随机生成）。
func (m *MemoryStore) SetSecret(secret string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secret = []byte(secret)
}

// === ProvisionStore 接口实现 ===

// IssueToken 生成并登记一个一次性 install token（HMAC(deviceID|tenantID|expiry|nonce)，ttl 为有效期）。
func (m *MemoryStore) IssueToken(deviceID, tenantID string, ttl time.Duration) (token string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if deviceID == "" {
		return "", errors.New("deviceID required")
	}
	return m.issueTokenLocked(deviceID, tenantID, ttl)
}

// ConsumeToken 校验并消费 token：限时、未用过才返回设备与租户并置 consumed；否则返回 ok=false。
// 安全（F8）：先验 HMAC 签名（防 DB 写权限伪造），再按摘要查找，最后检查一次性+过期。
func (m *MemoryStore) ConsumeToken(token string) (deviceID, tenantID string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !verifyTokenMAC(string(m.secret), token) {
		return "", "", false
	}
	tm, exists := m.tokens[hashToken(token)]
	if !exists {
		return "", "", false
	}
	if tm.consumed {
		return "", "", false
	}
	if time.Now().After(tm.expiresAt) {
		return "", "", false
	}
	tm.consumed = true
	return tm.deviceID, tm.tenantID, true
}
