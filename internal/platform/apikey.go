// apikey.go 实现 API Key 管理引擎（平台化）。
//
// API Key 用于程序化访问控制面 API（替代用户名/密码登录），
// 适用于 CI/CD 系统、自动化脚本、第三方集成等场景。
//
// 设计要点：
//   - Key 格式：om_ + 32 位随机 hex（共 35 字符），前缀 om_ 便于识别；
//   - 存储：仅存 SHA-256 hash，明文 key 仅在创建时返回一次（不可再次获取）；
//   - 校验：ValidateKey 用 SHA-256(明文) 比对已存 hash；
//   - Scopes：细粒度权限控制（如 ["device:read","task:write"]）；
//   - RateLimitPerSec：每秒限流（0=不限）；
//   - ExpiresAt：过期时间（零值=永不过期）；
//   - Enabled：禁用后立即失效。
package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"opsmesh/internal/store"
)

// 复用 store 包 APIKey 数据模型。
type APIKey = store.APIKey

// APIKeyManager API Key 管理引擎。
type APIKeyManager struct {
	store store.APIKeyStore
}

// NewAPIKeyManager 构造 API Key 管理引擎。
func NewAPIKeyManager(s store.APIKeyStore) *APIKeyManager {
	return &APIKeyManager{store: s}
}

// GenerateAPIKey 生成新的 API Key 明文与 hash。
// 返回 (prefix, hash)：
//   - prefix：明文 key（"om_" + 32 位随机 hex，共 35 字符），仅在生成时返回，调用方须妥善保存；
//   - hash：SHA-256(prefix) 的 hex 编码，用于持久化存储与校验。
//
// 熵源失败时返回错误（crypto/rand 不可用）。
func GenerateAPIKey() (prefix string, hash string, err error) {
	b := make([]byte, 16) // 16 字节 = 32 位 hex
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate api key: crypto/rand failed: %w", err)
	}
	prefix = "om_" + hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(prefix))
	hash = hex.EncodeToString(sum[:])
	return prefix, hash, nil
}

// hashAPIKey 计算明文 key 的 SHA-256 hex 摘要（用于 ValidateKey 比对已存 hash）。
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// ValidateKey 校验明文 API Key 是否有效。
// 校验流程：
//   - 解析 key 格式（om_ 前缀 + 32 位 hex）；
//   - 计算 SHA-256(key)，与 store 中已存 hash 比对；
//   - 校验 Enabled=true 且未过期。
//
// 返回匹配的 APIKey（不含明文 key）；无效返回错误。
func (m *APIKeyManager) ValidateKey(key string) (*APIKey, error) {
	if key == "" {
		return nil, errors.New("api key is empty")
	}
	if !strings.HasPrefix(key, "om_") {
		return nil, errors.New("invalid api key format: missing om_ prefix")
	}
	if len(key) != 35 { // om_ (3) + 32 hex
		return nil, fmt.Errorf("invalid api key length: %d (want 35)", len(key))
	}
	hash := hashAPIKey(key)
	// 遍历所有租户的 API Key 比对 hash（MVP 线性扫描；生产可建 hash→APIKey 索引）。
	// 由于 APIKeyStore 按 tenantID 隔离，这里需要遍历全部租户。
	// 为避免 platform 包依赖 TenantStore，这里通过 ListAPIKeys("") 走全租户路径
	// （MemoryStore 实现中 tenantID="" 返回全部）。
	//
	// L2 时序攻击加固：用 crypto/subtle.ConstantTimeCompare 替代 == 比较 hash。
	// k.Key 字段存的是 SHA-256 hex hash（非明文），与本次计算的 hash 常时比较，
	// 避免攻击者通过响应时间差异逐字节猜测 hash。
	keys := m.store.ListAPIKeys("")
	for _, k := range keys {
		if k == nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(k.Key), []byte(hash)) == 1 {
			if !k.Enabled {
				return nil, errors.New("api key is disabled")
			}
			if !k.ExpiresAt.IsZero() && time.Now().After(k.ExpiresAt) {
				return nil, errors.New("api key has expired")
			}
			return k, nil
		}
	}
	return nil, errors.New("api key not found")
}

// HasScope 检查 API Key 是否拥有指定 scope。
// scope 格式为 "resource:action"（如 "device:read"）。
// 空 scopes 表示拥有全部权限（向后兼容）。
func (m *APIKeyManager) HasScope(apiKey *APIKey, scope string) bool {
	if apiKey == nil {
		return false
	}
	if len(apiKey.Scopes) == 0 {
		// 空 scopes = 全权限（向后兼容，避免老 API Key 失效）。
		return true
	}
	for _, s := range apiKey.Scopes {
		if s == scope {
			return true
		}
		// 通配符匹配：device:* 匹配 device:read/device:write 等。
		if strings.HasSuffix(s, ":*") {
			prefix := strings.TrimSuffix(s, "*")
			if strings.HasPrefix(scope, prefix) {
				return true
			}
		}
	}
	return false
}
