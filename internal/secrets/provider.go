// Package secrets 提供统一的密钥管理抽象层。
//
// 支持 3 种密钥来源：
//   - EnvProvider：从环境变量读取（适合 K8s Secret 注入）
//   - FileProvider：从 JSON 文件读取（适合本地开发/CI 流水线）
//   - VaultProvider：从 HashiCorp Vault KV v2 引擎读取（适合生产环境）
//
// 通过 ChainProvider 可按优先级依次尝试多个 provider。
// ResolveSecret 辅助函数支持 ${provider:key} 引用语法，向后兼容明文配置。
//
// 为后续告警通道密钥外置提供基础。
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// ErrSecretNotFound 密钥不存在时返回的哨兵错误。
// 调用方通过 errors.Is(err, ErrSecretNotFound) 判定"未找到"与"读取失败"。
var ErrSecretNotFound = errors.New("secret not found")

// SecretProvider 密钥提供者抽象。按 key 获取密钥值。
// key 格式：扁平字符串（如 "notify/dingtalk/webhook_url"），由实现解释其含义。
type SecretProvider interface {
	// Get 按 key 获取密钥值。密钥不存在时返回 ("", ErrSecretNotFound)。
	Get(key string) (string, error)
	// Name 返回提供者名称（如 "env"、"file"、"vault"），用于日志和诊断。
	Name() string
}

// ============================================================================
// EnvProvider — 从环境变量读取密钥
// ============================================================================

// EnvProvider 从环境变量读取密钥。key 直接作为环境变量名。
// 可选 prefix（如 "OPSMESH_"）会拼接到 key 前，便于按命名空间隔离。
type EnvProvider struct {
	prefix string // 可选前缀（如 "OPSMESH_"），空=无前缀
}

// NewEnvProvider 构造 EnvProvider。prefix 为环境变量名前缀（空=无前缀）。
func NewEnvProvider(prefix string) *EnvProvider {
	return &EnvProvider{prefix: prefix}
}

// Get 实现 SecretProvider。从环境变量 prefix+key 读取。
// 环境变量未设置时返回 ("", ErrSecretNotFound)。
func (p *EnvProvider) Get(key string) (string, error) {
	v, ok := os.LookupEnv(p.prefix + key)
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}

// Name 返回 "env"。
func (p *EnvProvider) Name() string {
	return "env"
}

// ============================================================================
// FileProvider — 从 JSON 文件读取密钥
// ============================================================================

// FileProvider 从 JSON 文件读取密钥。JSON 结构：{"key1":"value1","key2":"value2"}。
// 支持嵌套：key 用 "/" 分隔，如 "notify/dingtalk/webhook_url" 对应
// {"notify":{"dingtalk":{"webhook_url":"..."}}}
//
// 文件在 NewFileProvider 时一次性加载到内存，后续 Get 不再访问磁盘。
// 如需热重载，可重新调用 NewFileProvider 替换实例。
type FileProvider struct {
	path string
	data map[string]interface{} // 解析后的嵌套 map
	mu   sync.RWMutex
}

// NewFileProvider 读取并解析 JSON 文件，构造 FileProvider。
// 文件不存在或解析失败时返回错误。
func NewFileProvider(path string) (*FileProvider, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取密钥文件 %q 失败: %w", path, err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("解析密钥文件 %q 失败: %w", path, err)
	}
	return &FileProvider{path: path, data: data}, nil
}

// Get 实现 SecretProvider。按 "/" 分隔遍历嵌套 map。
// 任意层级不存在或最终值非 string 时返回 ("", ErrSecretNotFound)。
func (p *FileProvider) Get(key string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cur := p.data
	parts := strings.Split(key, "/")
	for i, part := range parts {
		v, ok := cur[part]
		if !ok {
			return "", ErrSecretNotFound
		}
		// 最后一段必须为 string（密钥值）。
		if i == len(parts)-1 {
			s, ok := v.(string)
			if !ok {
				return "", ErrSecretNotFound
			}
			return s, nil
		}
		// 中间段必须为 map（继续下钻）。
		next, ok := v.(map[string]interface{})
		if !ok {
			return "", ErrSecretNotFound
		}
		cur = next
	}
	// 理论不可达（key 至少有一段）；保守返回 NotFound。
	return "", ErrSecretNotFound
}

// Name 返回 "file"。
func (p *FileProvider) Name() string {
	return "file"
}

// ============================================================================
// ChainProvider — 链式查找（按优先级依次尝试多个 provider）
// ============================================================================

// ChainProvider 链式密钥提供者：按顺序依次尝试多个 provider，
// 第一个返回非 ErrSecretNotFound 的结果胜出。
// 全部 provider 都返回 ErrSecretNotFound 时才返回 ErrSecretNotFound。
// 任一 provider 返回其他错误（如 Vault 不可达）则立即返回该错误（不继续尝试后续 provider）。
type ChainProvider struct {
	providers []SecretProvider
}

// NewChainProvider 构造 ChainProvider。providers 顺序即优先级顺序。
func NewChainProvider(providers ...SecretProvider) *ChainProvider {
	return &ChainProvider{providers: providers}
}

// Get 实现 SecretProvider。依次尝试 providers，第一个非 NotFound 的结果胜出。
func (c *ChainProvider) Get(key string) (string, error) {
	for _, p := range c.providers {
		v, err := p.Get(key)
		if err == nil {
			return v, nil
		}
		// 非 NotFound 错误立即返回（如 Vault 连接失败）。
		if !errors.Is(err, ErrSecretNotFound) {
			return "", err
		}
		// NotFound 继续尝试下一个 provider。
	}
	return "", ErrSecretNotFound
}

// Name 返回 "chain"。
func (c *ChainProvider) Name() string {
	return "chain"
}

// ============================================================================
// ResolveSecret — 解析密钥引用（向后兼容明文配置）
// ============================================================================

// ResolveSecret 解析密钥引用。如果 value 以 "${" 开头且以 "}" 结尾，从 provider 解析；
// 否则 value 本身就是明文密钥，直接返回（向后兼容）。
//
// 引用格式：
//   - ${provider:key}（如 ${vault:notify/dingtalk#secret}）：指定 provider 名称解析。
//     目前仅识别 "vault"/"env"/"file" 等名称用于诊断；实际解析仍由传入的 provider 完成，
//     即调用方应保证传入的 provider 与引用中的 provider 名称一致。
//   - ${key}（如 ${notify/dingtalk/webhook_url}）：用传入的 provider 解析（默认 provider）。
//
// provider 为 nil 或 value 不是引用格式时，直接返回明文 value。
func ResolveSecret(value string, provider SecretProvider) (string, error) {
	// 非引用格式：明文直接返回。
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return value, nil
	}
	// provider 为 nil 时无法解析引用，保守返回原文（避免误把引用当密钥）。
	if provider == nil {
		return value, nil
	}

	// 剥离 "${" 与 "}"。
	inner := value[2 : len(value)-1]

	// 解析可选的 "provider:" 前缀。存在时仅用于诊断/校验，实际解析仍走传入的 provider。
	// 这样设计的好处：调用方构造 ChainProvider 时无需关心引用具体指向哪个子 provider，
	// 由 ChainProvider 按优先级自动选择。
	key := inner
	if idx := strings.IndexByte(inner, ':'); idx >= 0 {
		// 仅当首段不含 "/" 时才视为 provider 名（避免把 "notify/dingtalk#webhook_url" 误判为含 provider 前缀）。
		head := inner[:idx]
		if !strings.ContainsRune(head, '/') && !strings.ContainsRune(head, '#') {
			key = inner[idx+1:]
		}
	}

	v, err := provider.Get(key)
	if err != nil {
		return "", fmt.Errorf("解析密钥引用 %q 失败: %w", value, err)
	}
	return v, nil
}
