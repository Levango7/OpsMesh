// apikey_test.go 测试 API Key 管理引擎（platform.APIKeyManager）。
package platform

import (
	"testing"
	"time"

	"opsmesh/internal/store"
)

// newTestAPIKeyManager 构造测试用 APIKeyManager。
func newTestAPIKeyManager() *APIKeyManager {
	return NewAPIKeyManager(store.NewMemoryStore())
}

// TestGenerateAPIKey_Format 验证生成的 key 格式正确（om_ + 32 hex = 35 字符）。
func TestGenerateAPIKey_Format(t *testing.T) {
	prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}
	if len(prefix) != 35 {
		t.Fatalf("prefix length=%d, want 35", len(prefix))
	}
	if prefix[:3] != "om_" {
		t.Fatalf("prefix=%q, want om_ prefix", prefix[:3])
	}
	if len(hash) != 64 { // SHA-256 hex = 64 字符
		t.Fatalf("hash length=%d, want 64", len(hash))
	}
}

// TestGenerateAPIKey_Unique 验证两次生成产生不同的 key。
func TestGenerateAPIKey_Unique(t *testing.T) {
	p1, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey 1 failed: %v", err)
	}
	p2, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey 2 failed: %v", err)
	}
	if p1 == p2 {
		t.Fatal("two generated keys should be different")
	}
}

// TestValidateKey_Valid 验证有效 key 校验通过。
func TestValidateKey_Valid(t *testing.T) {
	m := newTestAPIKeyManager()
	plainKey, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}
	m.store.CreateAPIKey("default", &store.APIKey{
		Name:    "test-key",
		Key:     hash,
		Enabled: true,
	})
	k, err := m.ValidateKey(plainKey)
	if err != nil {
		t.Fatalf("ValidateKey failed: %v", err)
	}
	if k.Name != "test-key" {
		t.Fatalf("Name=%q, want test-key", k.Name)
	}
}

// TestValidateKey_Empty 验证空 key 返回错误。
func TestValidateKey_Empty(t *testing.T) {
	m := newTestAPIKeyManager()
	if _, err := m.ValidateKey(""); err == nil {
		t.Fatal("ValidateKey(\"\") should return error")
	}
}

// TestValidateKey_BadPrefix 验证错误前缀返回错误。
func TestValidateKey_BadPrefix(t *testing.T) {
	m := newTestAPIKeyManager()
	if _, err := m.ValidateKey("bad_prefix_1234567890abcdef1234567890abcd"); err == nil {
		t.Fatal("ValidateKey with bad prefix should return error")
	}
}

// TestValidateKey_BadLength 验证错误长度返回错误。
func TestValidateKey_BadLength(t *testing.T) {
	m := newTestAPIKeyManager()
	if _, err := m.ValidateKey("om_short"); err == nil {
		t.Fatal("ValidateKey with bad length should return error")
	}
}

// TestValidateKey_Disabled 验证禁用 key 返回错误。
func TestValidateKey_Disabled(t *testing.T) {
	m := newTestAPIKeyManager()
	plainKey, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}
	m.store.CreateAPIKey("default", &store.APIKey{
		Name:    "disabled-key",
		Key:     hash,
		Enabled: false,
	})
	if _, err := m.ValidateKey(plainKey); err == nil {
		t.Fatal("ValidateKey for disabled key should return error")
	}
}

// TestValidateKey_Expired 验证过期 key 返回错误。
func TestValidateKey_Expired(t *testing.T) {
	m := newTestAPIKeyManager()
	plainKey, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}
	m.store.CreateAPIKey("default", &store.APIKey{
		Name:      "expired-key",
		Key:       hash,
		Enabled:   true,
		ExpiresAt: time.Now().Add(-1 * time.Hour), // 1 小时前过期
	})
	if _, err := m.ValidateKey(plainKey); err == nil {
		t.Fatal("ValidateKey for expired key should return error")
	}
}

// TestValidateKey_NotFound 验证不存在的 key 返回错误。
func TestValidateKey_NotFound(t *testing.T) {
	m := newTestAPIKeyManager()
	plainKey, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}
	if _, err := m.ValidateKey(plainKey); err == nil {
		t.Fatal("ValidateKey for nonexistent key should return error")
	}
}

// TestHasScope_ExactMatch 验证精确匹配 scope。
func TestHasScope_ExactMatch(t *testing.T) {
	m := newTestAPIKeyManager()
	k := &store.APIKey{Scopes: []string{"device:read", "task:write"}}
	if !m.HasScope(k, "device:read") {
		t.Fatal("HasScope should return true for exact match")
	}
	if m.HasScope(k, "device:write") {
		t.Fatal("HasScope should return false for non-existent scope")
	}
}

// TestHasScope_EmptyScopes 验证空 scopes 表示全权限。
func TestHasScope_EmptyScopes(t *testing.T) {
	m := newTestAPIKeyManager()
	k := &store.APIKey{Scopes: nil}
	if !m.HasScope(k, "anything:read") {
		t.Fatal("HasScope with empty scopes should return true (all permissions)")
	}
}

// TestHasScope_Wildcard 验证通配符匹配。
func TestHasScope_Wildcard(t *testing.T) {
	m := newTestAPIKeyManager()
	k := &store.APIKey{Scopes: []string{"device:*"}}
	if !m.HasScope(k, "device:read") {
		t.Fatal("HasScope should match wildcard device:* for device:read")
	}
	if !m.HasScope(k, "device:write") {
		t.Fatal("HasScope should match wildcard device:* for device:write")
	}
}

// TestHasScope_NilKey 验证 nil key 返回 false。
func TestHasScope_NilKey(t *testing.T) {
	m := newTestAPIKeyManager()
	if m.HasScope(nil, "device:read") {
		t.Fatal("HasScope(nil, ...) should return false")
	}
}
