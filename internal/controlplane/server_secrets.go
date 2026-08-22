// server_secrets.go — 密钥管理 HTTP handler。
//
// 暴露 3 个端点供前端密钥管理页面调用：
//   - GET  /api/v1/secrets/status → 当前 provider 类型、是否启用、Vault 地址、Mount 路径、密钥文件路径
//   - POST /api/v1/secrets/test   → 测试 provider 连接（返回 ok/fail + 延迟）
//   - GET  /api/v1/secrets/keys   → 密钥 key 列表（仅名称 + 来源 provider，不返回值）
//
// 安全约束：
//   - status 不返回 Vault token（避免前端泄露）。
//   - keys 仅返回 key 名称与来源 provider，不返回密钥值。
//   - test 端点对 Vault 地址做 SSRF 校验（复用 validateURLSSRF），拒绝私网/环回地址。
//
// 后端依赖 internal/secrets 包：NewVaultProvider 构造 Vault client；
// 本 handler 不在 Server 结构中新增字段，每次请求时按 cfg 现场构造（轻量），
// 保持 server.go 改动最小化。
package controlplane

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"opsmesh/internal/secrets"
)

// secretsStatusResponse /api/v1/secrets/status 响应。
// 不包含 Vault token（安全考虑）。
type secretsStatusResponse struct {
	Provider string `json:"provider"` // provider 类型：env/file/vault/chain:...
	Enabled  bool   `json:"enabled"`  // 是否启用（cfg.SecretProvider 非空）
	Addr     string `json:"addr"`     // Vault API 地址（仅 vault/chain:*vault 时非空）
	Mount    string `json:"mount"`    // Vault KV v2 挂载路径
	File     string `json:"file"`     // 密钥文件路径（仅 file/chain:*file 时非空）
}

// handleSecretsStatus 处理 GET /api/v1/secrets/status：返回当前 provider 配置概览。
// 不返回 token，避免前端泄露。
func (s *Server) handleSecretsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireProd(w, r, "secrets:read"); !ok {
		return
	}
	cfg := s.cfg
	resp := secretsStatusResponse{
		Provider: cfg.SecretProvider,
		Enabled:  cfg.SecretProvider != "",
		Addr:     cfg.VaultAddr,
		Mount:    cfg.VaultMount,
		File:     cfg.SecretFile,
	}
	writeJSON(w, http.StatusOK, resp)
}

// secretsTestRequest /api/v1/secrets/test 请求体。
type secretsTestRequest struct {
	Addr  string `json:"addr"`  // Vault API 地址（必填）
	Token string `json:"token"` // Vault 访问令牌（可选，为空时尝试环境变量）
	Mount string `json:"mount"` // KV v2 挂载路径（可选，默认 "secret"）
}

// secretsTestResponse /api/v1/secrets/test 响应。
type secretsTestResponse struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	Error     string `json:"error,omitempty"`
}

// handleSecretsTest 处理 POST /api/v1/secrets/test：测试 Vault provider 连接。
// 用临时构造的 VaultProvider 发起一次轻量探测（List KV v2 mount 根路径），
// 返回 ok/fail + 延迟。SSRF 校验拒绝私网/环回地址。
func (s *Server) handleSecretsTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireProd(w, r, "secrets:write"); !ok {
		return
	}
	var req secretsTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Addr == "" {
		jsonError(w, http.StatusBadRequest, "addr is required")
		return
	}
	// SSRF 校验：拒绝私网/环回/元数据地址，避免控制面被用作 SSRF 跳板。
	if err := validateURLSSRF(req.Addr); err != nil {
		writeJSON(w, http.StatusOK, secretsTestResponse{
			OK:    false,
			Error: "SSRF blocked: " + err.Error(),
		})
		return
	}
	// token 为空时回退环境变量（与 buildVault 行为一致）。
	token := req.Token
	if token == "" {
		if v, ok := os.LookupEnv("OPSMESH_VAULT_TOKEN"); ok && v != "" {
			token = v
		}
	}
	if token == "" {
		writeJSON(w, http.StatusOK, secretsTestResponse{
			OK:    false,
			Error: "vault token is empty (set token or env OPSMESH_VAULT_TOKEN)",
		})
		return
	}
	mount := req.Mount
	if mount == "" {
		mount = "secret"
	}

	start := time.Now()
	// 构造临时 VaultProvider 并发起一次轻量探测：尝试读取 mount 下 sys/internal/ui/mounts/<mount>。
	// 这里复用 secrets.NewVaultProvider 构造 client，再用其 Get 方法探测一个已知路径。
	// 探测路径选择 "sys/internal/ui/mounts/" + mount（Vault 系统路径，token 有效时返回 200/403，无效时 401）。
	// 简化处理：构造成功即视为连接 OK（Vault client 构造时不发起网络请求），
	// 真正的连通性探测由后续 Get 调用触发；此处用一次 Get 触发网络 IO。
	provider, err := secrets.NewVaultProvider(req.Addr, token, mount)
	if err != nil {
		writeJSON(w, http.StatusOK, secretsTestResponse{
			OK:        false,
			LatencyMs: time.Since(start).Milliseconds(),
			Error:     "construct vault provider: " + err.Error(),
		})
		return
	}
	// 探测：用一次 Get 触发网络 IO。key 用 "<mount>#_" 形式；
	// 即使返回 NotFound 也说明 Vault 可达且 token 通过认证（404 而非 401/403）。
	// 任何错误都视为连接失败，错误信息透传给前端（不含敏感信息）。
	_, getErr := provider.Get("opsmesh/secrets-test#probe")
	latency := time.Since(start).Milliseconds()
	if getErr == nil {
		writeJSON(w, http.StatusOK, secretsTestResponse{
			OK:        true,
			LatencyMs: latency,
		})
		return
	}
	// 区分 NotFound（可达）与其他错误（不可达/认证失败）。
	errStr := getErr.Error()
	if strings.Contains(errStr, "secret not found") || isVaultNotFound(errStr) {
		writeJSON(w, http.StatusOK, secretsTestResponse{
			OK:        true,
			LatencyMs: latency,
		})
		return
	}
	writeJSON(w, http.StatusOK, secretsTestResponse{
		OK:        false,
		LatencyMs: latency,
		Error:     errStr,
	})
}

// isVaultNotFound 判断 Vault 错误是否为 404（密钥不存在但 Vault 可达）。
// Vault API 在 404 时返回 "secret not found" 或 *vault.ResponseError(StatusCode=404)，
// 这里用字符串匹配简化判断（避免引入 vault/api 依赖到 controlplane 包）。
func isVaultNotFound(errStr string) bool {
	return strings.Contains(errStr, "404") || strings.Contains(errStr, "not found")
}

// secretsKeyEntry /api/v1/secrets/keys 列表项。
// 仅包含 key 名称与来源 provider，不包含密钥值（安全考虑）。
type secretsKeyEntry struct {
	Key      string `json:"key"`
	Provider string `json:"provider"`
}

// handleSecretsKeys 处理 GET /api/v1/secrets/keys：返回密钥 key 列表。
// 仅返回 key 名称与来源 provider，不返回密钥值。
//
// 数据来源：
//   - env provider：扫描环境变量中带 OPSMESH_ 前缀的 key（值不返回）。
//   - file provider：从 cfg.SecretFile 加载 JSON 并扁平化 key 路径（值不返回）。
//   - vault provider：Vault 无法枚举所有 key（KV v2 不支持 list 全量），返回空列表 + 提示。
//   - chain: 多 provider 合并去重。
//
// 简化实现：根据 cfg.SecretProvider 类型选择对应的 key 枚举策略；
// vault/未配置时返回空列表（前端展示"暂无密钥"）。
func (s *Server) handleSecretsKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireProd(w, r, "secrets:read"); !ok {
		return
	}
	cfg := s.cfg
	if cfg.SecretProvider == "" {
		writeJSON(w, http.StatusOK, []secretsKeyEntry{})
		return
	}
	writeJSON(w, http.StatusOK, s.enumerateSecretKeys())
}

// enumerateSecretKeys 根据 s.cfg 枚举密钥 key 列表（仅名称 + provider，不含值）。
// chain: 多 provider 合并按 key 去重（优先级高者保留）。
func (s *Server) enumerateSecretKeys() []secretsKeyEntry {
	spec := s.cfg.SecretProvider
	// chain:provider1,provider2,... 形式
	if strings.HasPrefix(spec, "chain:") {
		var entries []secretsKeyEntry
		for _, name := range strings.Split(spec[len("chain:"):], ",") {
			entries = append(entries, s.keysFromSingleProvider(strings.TrimSpace(name))...)
		}
		return dedupSecretKeys(entries)
	}
	// 单一 provider
	return s.keysFromSingleProvider(spec)
}

// keysFromSingleProvider 按单个 provider 名称枚举其 key 列表。
func (s *Server) keysFromSingleProvider(name string) []secretsKeyEntry {
	switch name {
	case "env":
		return enumerateEnvKeys("OPSMESH_")
	case "file":
		return s.enumerateFileKeys()
	case "vault":
		// Vault KV v2 不支持全量 list，返回空（前端展示"暂无密钥"）。
		return nil
	}
	return nil
}

// enumerateEnvKeys 扫描环境变量中带 prefix 前缀的 key，返回 key 列表（去除前缀）。
func enumerateEnvKeys(prefix string) []secretsKeyEntry {
	var out []secretsKeyEntry
	for _, kv := range os.Environ() {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		k := kv[:idx]
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		out = append(out, secretsKeyEntry{
			Key:      k[len(prefix):],
			Provider: "env",
		})
	}
	return out
}

// enumerateFileKeys 从 cfg.SecretFile 加载 JSON 并扁平化 key 路径。
// JSON 结构：{"a":{"b":"v1"},"c":"v2"} → keys=["a/b","c"]。
// 文件不存在或解析失败时返回空。
func (s *Server) enumerateFileKeys() []secretsKeyEntry {
	path := s.cfg.SecretFile
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	var out []secretsKeyEntry
	flattenJSONKeys(data, "", &out, "file")
	return out
}

// flattenJSONKeys 递归扁平化 JSON map，收集所有 string 叶子节点的 key 路径。
// path 为当前路径前缀（如 "a/b"），provider 为来源 provider 名称。
func flattenJSONKeys(node map[string]interface{}, prefix string, out *[]secretsKeyEntry, provider string) {
	for k, v := range node {
		full := k
		if prefix != "" {
			full = prefix + "/" + k
		}
		switch child := v.(type) {
		case map[string]interface{}:
			flattenJSONKeys(child, full, out, provider)
		case string:
			*out = append(*out, secretsKeyEntry{Key: full, Provider: provider})
		default:
			// 非 string/obj 类型也作为 key 收集（数值/布尔等）
			*out = append(*out, secretsKeyEntry{Key: full, Provider: provider})
		}
	}
}

// dedupSecretKeys 按 key 去重，保留首次出现的 entry（chain 中优先级高的 provider 先出现）。
func dedupSecretKeys(entries []secretsKeyEntry) []secretsKeyEntry {
	seen := make(map[string]bool, len(entries))
	out := make([]secretsKeyEntry, 0, len(entries))
	for _, e := range entries {
		if seen[e.Key] {
			continue
		}
		seen[e.Key] = true
		out = append(out, e)
	}
	return out
}
