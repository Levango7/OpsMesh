// kms.go — KMSProvider 实现：通过 HTTP API 调用 KMS（密钥管理服务）解密密文。
//
// 不依赖任何外部 KMS SDK，仅用标准库 net/http + encoding/json，通过 REST API 与 KMS 交互。
// 生产环境推荐从环境变量 OPSMESH_KMS_TOKEN 注入 token，避免在命令行/配置文件中暴露凭据。
//
// 与 VaultProvider 的定位互补：
//   - VaultProvider：从 Vault KV 引擎"读取"已存储的明文密钥（托管存储）。
//   - KMSProvider：将配置中的 base64 密文"解密"为明文（信封加密 / Envelope Encryption），
//     密文可安全入库/入仓，仅在使用时经 KMS 解密。

package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// KMSProvider 通过 HTTP API 调用 KMS 服务解密密文。
//
// key 含义：base64 编码的密文（ciphertext），由调用方（如告警通道配置）直接存储。
// Get(key) 将该密文 POST 到 {endpoint}/decrypt，由 KMS 用主密钥 keyID 解密后返回明文。
//
// 适用场景：信封加密——配置/数据库中存储密文，运行期经 KMS 解密，避免明文落盘。
type KMSProvider struct {
	endpoint   string       // KMS API 根地址（如 https://kms.example.com/api/v1）
	keyID      string       // 主密钥 ID（CMK / Customer Master Key）
	token      string       // Bearer 认证 token（访问 KMS API 的凭据）
	httpClient *http.Client // 复用 HTTP 连接池
}

// kmsDecryptRequest KMS 解密请求体。
type kmsDecryptRequest struct {
	KeyID      string `json:"key_id"`     // 主密钥 ID
	Ciphertext string `json:"ciphertext"` // base64 编码的密文
}

// kmsDecryptResponse KMS 解密响应体。
type kmsDecryptResponse struct {
	Plaintext string `json:"plaintext"` // 解密后的明文
}

// NewKMSProvider 构造 KMSProvider。
//   - endpoint：KMS API 地址（如 "https://kms.example.com/api/v1"）
//   - keyID：主密钥 ID（CMK），用于解密
//   - token：Bearer 认证 token（推荐从环境变量 OPSMESH_KMS_TOKEN 注入）
//
// endpoint 或 keyID 为空时返回错误（避免静默退化）；token 允许为空（部分内网 KMS 不鉴权）。
// HTTP 客户端带 10s 超时（与 VaultProvider 一致），避免 KMS 不可达时阻塞调用方。
func NewKMSProvider(endpoint, keyID, token string) (*KMSProvider, error) {
	if endpoint == "" {
		return nil, errors.New("kms endpoint 为空")
	}
	if keyID == "" {
		return nil, errors.New("kms key_id 为空")
	}

	return &KMSProvider{
		endpoint: endpoint,
		keyID:    keyID,
		token:    token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// Get 实现 SecretProvider。
//
// key 为 base64 编码的密文。流程：
//  1. 构造请求体 {"key_id":"...","ciphertext":"<key>"}，POST 到 {endpoint}/decrypt。
//  2. 携带 Authorization: Bearer <token>（token 非空时）。
//  3. KMS 返回 200 + {"plaintext":"..."} 时返回 plaintext。
//  4. HTTP 404 → ErrSecretNotFound（密文/主密钥不存在）。
//  5. 其他 HTTP 错误 → fmt.Errorf 包装（含状态码与响应体摘要，便于诊断）。
//
// 上下文带 10s 超时（与 httpClient.Timeout 双保险，避免连接建立后读阻塞）。
func (k *KMSProvider) Get(key string) (string, error) {
	// 构造请求体。
	reqBody := kmsDecryptRequest{
		KeyID:      k.keyID,
		Ciphertext: key,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("kms 构造请求体失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.endpoint+"/decrypt", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("kms 构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Bearer token 认证（token 非空时设置；内网无鉴权 KMS 可留空）。
	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}

	resp, err := k.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("kms 调用 %q 失败: %w", k.endpoint+"/decrypt", err)
	}
	defer resp.Body.Close()

	// 读取响应体（限制 1MB，避免异常大响应耗尽内存）。
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("kms 读取响应体失败: %w", err)
	}

	// HTTP 404 → ErrSecretNotFound（密文或主密钥不存在）。
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrSecretNotFound
	}

	// 其他非 2xx → 包装错误（含状态码与响应体摘要，便于诊断）。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 响应体摘要截断到 256 字节，避免超长错误信息。
		snippet := string(raw)
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return "", fmt.Errorf("kms 解密失败: HTTP %d, 响应: %s", resp.StatusCode, snippet)
	}

	// 解析响应体 {"plaintext":"..."}。
	var respBody kmsDecryptResponse
	if err := json.Unmarshal(raw, &respBody); err != nil {
		return "", fmt.Errorf("kms 解析响应体失败: %w (原始: %s)", err, string(raw))
	}
	if respBody.Plaintext == "" {
		return "", ErrSecretNotFound
	}
	return respBody.Plaintext, nil
}

// Name 返回 "kms"。
func (k *KMSProvider) Name() string {
	return "kms"
}
