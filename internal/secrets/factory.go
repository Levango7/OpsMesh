// factory.go — 根据配置构造 SecretProvider 的工厂函数。
//
// 支持的组合：
//   - "env" → EnvProvider（前缀 OPSMESH_）
//   - "file" → FileProvider（从 cfg.SecretFile 加载）
//   - "vault" → VaultProvider（连接 cfg.VaultAddr）
//   - "chain:env,file" → ChainProvider（依次尝试 env 和 file）
//   - "chain:env,vault" → ChainProvider（env 优先，vault 兜底）

package secrets

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"opsmesh/internal/config"
)

// FromConfig 根据 config 构造 SecretProvider。
// cfg.SecretProvider 为空时返回 nil（不启用密钥外置）。
//
// 支持的组合：
//   - "env" → EnvProvider（前缀 OPSMESH_）
//   - "file" → FileProvider（从 cfg.SecretFile 加载）
//   - "vault" → VaultProvider（连接 cfg.VaultAddr）
//   - "chain:env,file" → ChainProvider（依次尝试 env 和 file）
//
// 任一组合下子 provider 构造失败时返回错误（fail-fast，避免运行期诡异失败）。
func FromConfig(cfg *config.Config) (SecretProvider, error) {
	if cfg == nil || cfg.SecretProvider == "" {
		return nil, nil
	}

	spec := cfg.SecretProvider

	// chain:provider1,provider2,... 形式。
	if strings.HasPrefix(spec, "chain:") {
		return buildChain(spec[len("chain:"):], cfg)
	}

	// 单一 provider。
	switch spec {
	case "env":
		return NewEnvProvider("OPSMESH_"), nil
	case "file":
		if cfg.SecretFile == "" {
			return nil, errors.New("--secret-provider=file 但 --secret-file 为空")
		}
		return NewFileProvider(cfg.SecretFile)
	case "vault":
		return buildVault(cfg)
	default:
		return nil, fmt.Errorf("非法 --secret-provider=%q（应为 env | file | vault | chain:...）", spec)
	}
}

// buildChain 构造 ChainProvider。names 为逗号分隔的子 provider 名称列表。
func buildChain(names string, cfg *config.Config) (SecretProvider, error) {
	parts := strings.Split(names, ",")
	if len(parts) == 0 {
		return nil, errors.New("chain: 后须指定至少一个 provider（如 chain:env,file）")
	}

	providers := make([]SecretProvider, 0, len(parts))
	for _, name := range parts {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		p, err := buildSingle(name, cfg)
		if err != nil {
			return nil, fmt.Errorf("chain 子 provider %q 构造失败: %w", name, err)
		}
		providers = append(providers, p)
	}
	if len(providers) == 0 {
		return nil, errors.New("chain: 解析后无有效 provider")
	}
	return NewChainProvider(providers...), nil
}

// buildSingle 按名称构造单个 SecretProvider。
func buildSingle(name string, cfg *config.Config) (SecretProvider, error) {
	switch name {
	case "env":
		return NewEnvProvider("OPSMESH_"), nil
	case "file":
		if cfg.SecretFile == "" {
			return nil, errors.New("--secret-file 为空（file provider 需要 JSON 密钥文件路径）")
		}
		return NewFileProvider(cfg.SecretFile)
	case "vault":
		return buildVault(cfg)
	default:
		return nil, fmt.Errorf("未知 provider 名称 %q（应为 env | file | vault）", name)
	}
}

// buildVault 从 config 构造 VaultProvider。
// token 优先取 cfg.VaultToken，为空时回退环境变量 OPSMESH_VAULT_TOKEN（更安全）。
func buildVault(cfg *config.Config) (SecretProvider, error) {
	if cfg.VaultAddr == "" {
		return nil, errors.New("--vault-addr 为空（vault provider 需要 Vault API 地址）")
	}
	token := cfg.VaultToken
	if token == "" {
		// 回退到环境变量（避免在命令行/配置文件中暴露 token）。
		if v, ok := os.LookupEnv("OPSMESH_VAULT_TOKEN"); ok && v != "" {
			token = v
		}
	}
	if token == "" {
		return nil, errors.New("vault token 为空（请配置 --vault-token 或环境变量 OPSMESH_VAULT_TOKEN）")
	}
	return NewVaultProvider(cfg.VaultAddr, token, cfg.VaultMount)
}
