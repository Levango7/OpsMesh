// k8s_cluster.go 实现 Phase 3 K8s 集群管理 HTTP handler。
//
// API 端点：
//   - GET    /api/v1/k8s/clusters        列出所有集群（kubeconfig 脱敏为 ***）
//   - POST   /api/v1/k8s/clusters        添加集群 {name, server, kubeconfig}
//   - DELETE /api/v1/k8s/clusters/{id}   删除集群（返回 204）
//   - POST   /api/v1/k8s/clusters/{id}/test 测试连接 {status, message}
//
// 资源管理 API（详见 k8s_manage.go）：
//   - /api/v1/k8s/clusters/{id}/namespaces | pods | deployments | services | configmaps | secrets | nodes
//
// 设计要点：
//   - kubeconfig 为敏感内容，GET 列表/详情时脱敏为 ***（不返回原内容）；
//   - 添加集群时同步建立 client-go 连接（ClusterManager.AddCluster），连接失败仍保存配置但标记 offline；
//   - 测试连接不持久化，仅临时构造客户端验证连通性；
//   - 鉴权：需 user:read/user:write/user:delete 权限（与用户管理 API 一致）；
//   - 错误响应统一 {"error": "message"} 格式，HTTP 状态码 400/404/500。
package controlplane

import (
	"crypto/aes"
	"crypto/cipher"
	cryptoRand "crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"opsmesh/internal/logx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// k8sTenantFromRequest 提取请求归属租户（K8s 租户隔离）。
// 优先取网关注入的 X-Tenant-ID；缺头时：
//   - requireAuth=true：返回空串（由调用方 handler 拒绝 401，防绕过网关伪造租户）；
//   - requireAuth=false：归一为 default（与 store 层 SaveK8sCluster 空租户归一一致，保持 demo 兼容）。
//
// 修复：原实现缺头静默归一 default，绕过租户闸门（requireAuth 下缺头应 401 而非 default）。
func (s *Server) k8sTenantFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return "", false
	}
	return actx.TenantID, true
}

// encryptKubeconfig 用 AES-256-GCM 加密 kubeconfig 明文，返回 base64(nonce+ciphertext)。
// 安全语义：DB 泄露时加密后的 kubeconfig 不可直接还原，需同时拿到加密密钥才能解密。
//
// 行为：
//   - 空串透传（空 kubeconfig 不加密）；
//   - encryptionKey 未配置（非生产模式）：明文透传（保持 demo 兼容，NewServer 已告警）；
//   - encryptionKey 已配置：AES-GCM 加密，base64 编码返回。
func (s *Server) encryptKubeconfig(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if len(s.encryptionKey) == 0 {
		return plaintext, nil // 未配置加密密钥：明文透传（非生产/demo 兼容）
	}
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("kubeconfig 加密失败（AES 初始化）: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("kubeconfig 加密失败（GCM 初始化）: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := cryptoRand.Read(nonce); err != nil {
		return "", fmt.Errorf("kubeconfig 加密失败（nonce 生成）: %w", err)
	}
	// Seal 把 nonce 作为 dst 前缀追加，结果 = nonce + ciphertext + gcmTag。
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptKubeconfig 用 AES-256-GCM 解密 base64(nonce+ciphertext)，返回 kubeconfig 明文。
// 用于从 store 读取加密 kubeconfig 后还原为明文（传给 ClusterManager.AddCluster/TestCluster）。
//
// 行为：
//   - 空串透传（空 kubeconfig 不解密）；
//   - encryptionKey 未配置（非生产模式）：明文透传（store 中即为明文）；
//   - encryptionKey 已配置：base64 解码 → AES-GCM 解密 → 返回明文。
func (s *Server) decryptKubeconfig(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	if len(s.encryptionKey) == 0 {
		return encrypted, nil // 未配置加密密钥：明文透传
	}
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("kubeconfig 解密失败（base64 解码）: %w", err)
	}
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("kubeconfig 解密失败（AES 初始化）: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("kubeconfig 解密失败（GCM 初始化）: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("kubeconfig 解密失败：密文长度 %d < nonce 长度 %d", len(data), nonceSize)
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("kubeconfig 解密失败（GCM 验签）: %w", err)
	}
	return string(plaintext), nil
}

// k8sClusterKubeconfigMasked 是 kubeconfig 脱敏后的占位符。
// GET 列表/详情时用此值替换原 kubeconfig，避免敏感凭据泄露到前端。
const k8sClusterKubeconfigMasked = "***"

// maskK8sCluster 返回脱敏后的集群配置副本（Kubeconfig 替换为 ***）。
// 用于 GET 列表/详情 API 响应，避免敏感凭据泄露。
func maskK8sCluster(c *store.K8sCluster) *store.K8sCluster {
	if c == nil {
		return nil
	}
	cp := *c
	if cp.Kubeconfig != "" {
		cp.Kubeconfig = k8sClusterKubeconfigMasked
	}
	return &cp
}

// maskK8sClusters 批量脱敏集群配置列表。
func maskK8sClusters(cs []*store.K8sCluster) []*store.K8sCluster {
	out := make([]*store.K8sCluster, 0, len(cs))
	for _, c := range cs {
		out = append(out, maskK8sCluster(c))
	}
	return out
}

// handleK8sClusters 统一处理 /api/v1/k8s/clusters：
//   - GET：列出所有集群（需 user:read 权限，kubeconfig 脱敏）
//   - POST：添加集群（需 user:write 权限）
func (s *Server) handleK8sClusters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListK8sClusters(w, r)
	case http.MethodPost:
		s.handleCreateK8sCluster(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListK8sClusters 处理 GET /api/v1/k8s/clusters：列出所有集群（kubeconfig 脱敏）。
func (s *Server) handleListK8sClusters(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	// 租户兜底：requireAuth 下缺租户头 → 401（防绕过网关伪造租户）。
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	// 租户隔离：仅返回当前租户的集群。
	clusters := s.store.ListK8sClusters(tenant)
	writeJSON(w, http.StatusOK, map[string]interface{}{"clusters": maskK8sClusters(clusters)})
}

// handleCreateK8sCluster 处理 POST /api/v1/k8s/clusters：添加集群。
// 请求体：{name, server, kubeconfig}；name 与 kubeconfig 必填。
// 行为：
//   - 校验 name/kubeconfig 非空；
//   - 保存集群配置到 store（Status 初始为 unknown）；
//   - 尝试建立 client-go 连接（ClusterManager.AddCluster），成功标记 online，失败标记 offline；
//   - 返回创建的集群（含 ID，kubeconfig 脱敏）。
func (s *Server) handleCreateK8sCluster(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "k8s:write")
	if !ok {
		return
	}
	// 租户兜底：requireAuth 下缺租户头 → 401。
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body struct {
		Name       string `json:"name"`
		Server     string `json:"server"`
		Kubeconfig string `json:"kubeconfig"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" || body.Kubeconfig == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and kubeconfig are required"})
		return
	}
	// ：kubeconfig 存入 store 前做 AES-GCM 加密，DB 泄露时不直接暴露集群凭据。
	encrypted, err := s.encryptKubeconfig(body.Kubeconfig)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "kubeconfig encryption failed"})
		return
	}
	c := &store.K8sCluster{
		Name:       body.Name,
		TenantID:   tenant, // 租户归属
		Server:     body.Server,
		Kubeconfig: encrypted, // 加密后存 store
		Status:     "unknown",
	}
	// 持久化失败直接返回 500，不再假装保存成功。
	if err := s.store.SaveK8sCluster(c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save cluster failed"})
		return
	}
	// 尝试建立 client-go 连接：成功标记 online，失败标记 offline（仍保存配置，用户可后续 test 重试）。
	// AddCluster 需要明文 kubeconfig 解析 REST config，用 body.Kubeconfig（明文）而非加密值。
	if s.clusterMgr != nil {
		if err := s.clusterMgr.AddCluster(c.ID, body.Kubeconfig); err != nil {
			c.Status = "offline"
		} else {
			c.Status = "online"
		}
		// 状态回写：c.Kubeconfig 已是加密值，直接存 store。
		if err := s.store.SaveK8sCluster(c); err != nil {
			logx.Error(r.Context(), "K8s 集群状态回写失败", err, "clusterID", c.ID)
		}
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: c.TenantID, UserID: caller.ID, Action: "k8s_cluster_create", Target: c.ID, Detail: sanitizeAuditDetail("name=" + c.Name),
	})
	writeJSON(w, http.StatusCreated, maskK8sCluster(c))
}

// handleK8sClusterRouting 分派 /api/v1/k8s/clusters/{id} 子路径：
//   - DELETE /api/v1/k8s/clusters/{id}：删除集群（需 k8s:delete 权限，返回 204）
//   - POST   /api/v1/k8s/clusters/{id}/test：测试连接（需 k8s:read 权限）
//   - GET    /api/v1/k8s/clusters/{id}/namespaces|pods|deployments|services|configmaps|secrets|nodes[...]：
//     K8s 资源管理（详见 k8s_manage.go）。
//
// 路由结构：/{id}                      → 集群本身（DELETE）
//
//	/{id}/test                  → 测试连接
//	/{id}/{resource}[/{sub...}] → K8s 资源管理（handleK8sResourceRouting）
func (s *Server) handleK8sClusterRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/k8s/clusters/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cluster id required"})
		return
	}
	// 按 / 切分：[id] / [id, resource] / [id, resource, sub...]。
	// SplitN 3 段：保证 sub 保留剩余所有 /（如 pods/ns/name/logs）。
	parts := strings.SplitN(rest, "/", 3)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cluster id required"})
		return
	}
	// 仅 /{id}：集群本身管理（DELETE）。
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodDelete:
			s.handleDeleteK8sCluster(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	// /{id}/{resource} 或 /{id}/{resource}/{sub}。
	resource := parts[1]
	sub := ""
	if len(parts) == 3 {
		sub = parts[2]
	}
	// 集群级动作（与具体 K8s 资源无关）。
	if resource == "test" {
		s.handleTestK8sCluster(w, r, id)
		return
	}
	// K8s 资源管理路由分发到 k8s_manage.go。
	s.handleK8sResourceRouting(w, r, id, resource, sub)
}

// handleDeleteK8sCluster 处理 DELETE /api/v1/k8s/clusters/{id}：删除集群（返回 204）。
// 同时从 ClusterManager 移除连接（若存在）。
func (s *Server) handleDeleteK8sCluster(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "k8s:delete")
	if !ok {
		return
	}
	// 租户兜底：requireAuth 下缺租户头 → 401。
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	existing := s.store.GetK8sCluster(id)
	// 租户隔离：集群不存在或归属其他租户时按 not found 拒绝（不泄露存在性）。
	if existing == nil || existing.TenantID != tenant {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not found"})
		return
	}
	if !s.store.DeleteK8sCluster(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not found"})
		return
	}
	// 同步移除 ClusterManager 中的连接（若存在）。
	if s.clusterMgr != nil {
		s.clusterMgr.RemoveCluster(id)
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: existing.TenantID, UserID: caller.ID, Action: "k8s_cluster_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name),
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleTestK8sCluster 处理 POST /api/v1/k8s/clusters/{id}/test：测试集群连接。
// 返回 {status: "online"/"offline", message: "..."}。
// 行为：
//   - 取集群配置（不存在返回 404）；
//   - 用 ClusterManager.TestCluster 临时构造客户端测试连通性（不持久化连接）；
//   - 成功 → 更新 store 中 Status 为 online，返回 online；
//   - 失败 → 更新 store 中 Status 为 offline，返回 offline + 错误信息。
func (s *Server) handleTestK8sCluster(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	// 租户兜底：requireAuth 下缺租户头 → 401。
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	existing := s.store.GetK8sCluster(id)
	// 租户隔离：集群不存在或归属其他租户时按 not found 拒绝。
	if existing == nil || existing.TenantID != tenant {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not found"})
		return
	}
	// ClusterManager 未初始化时（理论上不会，NewServer 必构造）返回 500。
	if s.clusterMgr == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cluster manager not initialized"})
		return
	}
	// ：store 中 kubeconfig 为加密存储，TestCluster 需明文解析 REST config，先解密。
	plain, err := s.decryptKubeconfig(existing.Kubeconfig)
	if err != nil {
		logx.Error(r.Context(), "K8s 集群 kubeconfig 解密失败", err, "clusterID", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "kubeconfig decryption failed"})
		return
	}
	err = s.clusterMgr.TestCluster(id, plain)
	if err != nil {
		existing.Status = "offline"
		_ = s.store.SaveK8sCluster(existing) // existing.Kubeconfig 仍为加密值，直接回写
		// 原始错误可能泄漏 API Server 地址等内部信息，仅记日志，前端给通用文案。
		logx.Error(r.Context(), "K8s 集群测试连接失败", err, "clusterID", id)
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "offline",
			"message": "连接失败（详见控制面日志）",
		})
		return
	}
	existing.Status = "online"
	// 状态回写失败不阻断测试结论，仅记日志。
	if err := s.store.SaveK8sCluster(existing); err != nil {
		logx.Error(r.Context(), "K8s 集群状态回写失败", err, "clusterID", id)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "online",
		"message": "连接正常",
	})
}
