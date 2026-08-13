// vault.go — VaultProvider 实现：从 HashiCorp Vault KV v2 引擎读取密钥。
//
// 依赖 github.com/hashicorp/vault/api 客户端。生产环境推荐配合 Vault Agent 注入 token，
// 避免在配置文件中硬编码 token。

package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	vault "github.com/hashicorp/vault/api"
)

// VaultProvider 从 HashiCorp Vault KV v2 引擎读取密钥。
// path 为 KV 挂载路径（如 "secret"），key 格式为 "path/to/secret#field"：
//   - "path/to/secret" 为 Vault 中的 secret 路径
//   - "field" 为该 secret 中的字段名
//
// 示例：key="notify/dingtalk#webhook_url" 表示从 mount=secret 引擎下
// 路径 "notify/dingtalk" 的 secret 中取 "webhook_url" 字段。
type VaultProvider struct {
	client *vault.Client
	mount  string // KV 引擎挂载路径（默认 "secret"）
}

// NewVaultProvider 构造 VaultProvider。
//   - addr：Vault API 地址（如 "https://vault:8200"）
//   - token：Vault 访问令牌（推荐从环境变量 OPSMESH_VAULT_TOKEN 注入）
//   - mount：KV v2 引擎挂载路径（空则默认 "secret"）
//
// addr 或 token 为空时返回错误（避免静默退化）。
func NewVaultProvider(addr, token, mount string) (*VaultProvider, error) {
	if addr == "" {
		return nil, errors.New("vault addr 为空")
	}
	if token == "" {
		return nil, errors.New("vault token 为空")
	}
	if mount == "" {
		mount = "secret"
	}

	cfg := vault.DefaultConfig()
	cfg.Address = addr
	client, err := vault.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("构造 vault client 失败: %w", err)
	}
	client.SetToken(token)

	return &VaultProvider{client: client, mount: mount}, nil
}

// Get 实现 SecretProvider。
//
// key 格式："path/to/secret#field"。
//   - 按 "#" 分割为 secretPath 与 field（field 缺省时返回错误）
//   - 调用 client.KVv2(mount).Get(ctx, secretPath) 读取 secret
//   - 从 secret.Data 中取 field 值，转为 string 返回
//   - secret 不存在或 field 不存在时返回 ErrSecretNotFound
//
// 上下文带 10s 超时，避免 Vault 不可达时阻塞调用方。
func (v *VaultProvider) Get(key string) (string, error) {
	// 解析 "path#field"。
	idx := strings.LastIndexByte(key, '#')
	if idx < 0 {
		return "", fmt.Errorf("vault key %q 缺少 #field 后缀（格式应为 path/to/secret#field）", key)
	}
	secretPath := key[:idx]
	field := key[idx+1:]
	if secretPath == "" || field == "" {
		return "", fmt.Errorf("vault key %q 解析后 secretPath 或 field 为空", key)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secret, err := v.client.KVv2(v.mount).Get(ctx, secretPath)
	if err != nil {
		// Vault API 返回 404 时识别为 NotFound。
		// vault/api 的 KVv2.Get 在 404 时返回两种错误形式：
		//   1. *vault.ResponseError（StatusCode == 404）
		//   2. fmt.Errorf("secret not found: at %s", path)（KVv2 内部包装）
		var respErr *vault.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == 404 {
			return "", ErrSecretNotFound
		}
		if strings.Contains(err.Error(), "secret not found") {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("vault 读取 %q 失败: %w", secretPath, err)
	}
	if secret == nil || secret.Data == nil {
		return "", ErrSecretNotFound
	}

	raw, ok := secret.Data[field]
	if !ok {
		return "", ErrSecretNotFound
	}

	// 字段值可能为 string 或其他类型（数字/布尔等），统一转 string。
	switch s := raw.(type) {
	case string:
		return s, nil
	default:
		return fmt.Sprintf("%v", s), nil
	}
}

// Name 返回 "vault"。
func (v *VaultProvider) Name() string {
	return "vault"
}
