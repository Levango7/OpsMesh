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
	"net/http"
	"strings"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

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
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	clusters := s.store.ListK8sClusters()
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
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
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
	c := &store.K8sCluster{
		Name:       body.Name,
		Server:     body.Server,
		Kubeconfig: body.Kubeconfig,
		Status:     "unknown",
	}
	s.store.SaveK8sCluster(c)
	// 尝试建立 client-go 连接：成功标记 online，失败标记 offline（仍保存配置，用户可后续 test 重试）。
	if s.clusterMgr != nil {
		if err := s.clusterMgr.AddCluster(c.ID, body.Kubeconfig); err != nil {
			c.Status = "offline"
		} else {
			c.Status = "online"
		}
		s.store.SaveK8sCluster(c)
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "k8s_cluster_create", Target: c.ID, Detail: "name=" + c.Name,
	})
	writeJSON(w, http.StatusCreated, maskK8sCluster(c))
}

// handleK8sClusterRouting 分派 /api/v1/k8s/clusters/{id} 子路径：
//   - DELETE /api/v1/k8s/clusters/{id}：删除集群（需 user:delete 权限，返回 204）
//   - POST   /api/v1/k8s/clusters/{id}/test：测试连接（需 user:read 权限）
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
	caller, ok := s.requirePermission(w, r, "user:delete")
	if !ok {
		return
	}
	existing := s.store.GetK8sCluster(id)
	if existing == nil {
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
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "k8s_cluster_delete", Target: id, Detail: "name=" + existing.Name,
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
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	existing := s.store.GetK8sCluster(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not found"})
		return
	}
	// ClusterManager 未初始化时（理论上不会，NewServer 必构造）返回 500。
	if s.clusterMgr == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cluster manager not initialized"})
		return
	}
	err := s.clusterMgr.TestCluster(id, existing.Kubeconfig)
	if err != nil {
		existing.Status = "offline"
		s.store.SaveK8sCluster(existing)
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "offline",
			"message": err.Error(),
		})
		return
	}
	existing.Status = "online"
	s.store.SaveK8sCluster(existing)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "online",
		"message": "连接正常",
	})
}
