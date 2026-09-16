// k8s_manage.go 实现 Phase 3 K8s 资源管理 HTTP handler。
//
// 在 k8s_cluster.go 集群管理（增删查 + 测试连接）之上，本文件实现具体 K8s 资源的
// 只读/写操作，全部基于 client-go Clientset，无需 kubectl 二进制依赖。
//
// API 端点（{id} 为集群 ID）：
//   - GET    /api/v1/k8s/clusters/{id}/namespaces                       列出 namespace
//   - GET    /api/v1/k8s/clusters/{id}/pods?namespace={ns}              列出 pod
//   - GET    /api/v1/k8s/clusters/{id}/pods/{ns}/{name}/logs            获取 pod 日志
//   - DELETE /api/v1/k8s/clusters/{id}/pods/{ns}/{name}                 删除 pod
//   - GET    /api/v1/k8s/clusters/{id}/deployments?namespace={ns}       列出 deployment
//   - POST   /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/scale    扩缩容 {replicas}
//   - POST   /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/restart  滚动重启
//   - POST   /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/rollback 回滚到上一 revision
//   - GET    /api/v1/k8s/clusters/{id}/services?namespace={ns}          列出 service
//   - GET    /api/v1/k8s/clusters/{id}/configmaps?namespace={ns}        列出 configmap
//   - GET    /api/v1/k8s/clusters/{id}/secrets?namespace={ns}           列出 secret（仅 key 名）
//   - GET    /api/v1/k8s/clusters/{id}/nodes                            列出 node
//
// 设计要点：
//   - 路由分派由 handleK8sResourceRouting 统一入口，按 resource 段分发到具体 handler；
//   - 集群连接通过 ClusterManager.GetClient 获取，未连接返回 404；
//   - 鉴权：读操作需 user:read，写操作需 user:write，删除需 user:delete（与集群管理一致）；
//   - 所有 K8s 调用都加 30s 超时（r.Context() 派生），避免阻塞 API 请求；
//   - 错误响应统一 {"error": "message"} 格式，HTTP 状态码 400/404/500；
//   - Secret 列表仅返回 key 名，不返回 value（避免敏感内容泄露）；
//   - 写操作（delete pod / scale / restart）记录审计事件。
//
// 拆分说明：Pod 操作位于 k8s_pods.go，Deployment/Service/ConfigMap/Secret 操作位于
// k8s_deployments.go，集群级操作（dashboard/metrics/health/nodes）位于 k8s_cluster_ops.go，
// 本文件保留路由入口（handleK8sResourceRouting/routePods/routeDeployments/routeNodes）
// 与辅助函数（formatAge/round2）。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/k8s"
)

// k8sAPITimeout 单次 K8s API 调用的超时时间。
// 30s 覆盖绝大多数 list/get/patch 操作；超大集群的 list 可能需要调大或改用分页。
const k8sAPITimeout = 30 * time.Second

// handleK8sResourceRouting 分派 /api/v1/k8s/clusters/{id}/{resource}[/{sub}] 资源管理子路径。
//
// 入参：
//   - clusterID：集群 ID（已由 handleK8sClusterRouting 解析）；
//   - resource：资源类型（namespaces / pods / deployments / services / configmaps / secrets / nodes）；
//   - sub：资源子路径（如 pods 的 "{ns}/{name}/logs"），可为空。
//
// 行为：
//   - 取集群连接（未连接返回 404）；
//   - 按 resource 分发到具体 handler；
//   - 未知 resource 返回 404。
func (s *Server) handleK8sResourceRouting(w http.ResponseWriter, r *http.Request, clusterID, resource, sub string) {
	if s.clusterMgr == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "cluster manager not initialized"})
		return
	}
	// 租户兜底：requireAuth 下缺租户头 → 401（防绕过网关伪造租户）。
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	// 租户隔离：校验集群归属当前租户，防跨租户操作集群资源（不泄露存在性）。
	if c := s.store.GetK8sCluster(clusterID); c == nil || c.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not found"})
		return
	}
	client, err := s.clusterMgr.GetClient(clusterID)
	if err != nil {
		writeSanitizedError(r.Context(), w, http.StatusNotFound, "k8s.clusterNotConnected", "cluster not connected", err)
		return
	}
	switch resource {
	case "namespaces":
		s.handleListNamespaces(w, r, client)
	case "pods":
		s.routePods(w, r, client, sub)
	case "deployments":
		s.routeDeployments(w, r, client, sub)
	case "services":
		s.handleListServices(w, r, client)
	case "configmaps":
		s.handleListConfigMaps(w, r, client)
	case "secrets":
		s.handleListSecrets(w, r, client)
	case "nodes":
		s.routeNodes(w, r, client, sub)
	case "dashboard":
		s.handleClusterDashboard(w, r, client)
	case "health":
		s.handleClusterHealth(w, r, client)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown resource: " + resource})
	}
}

// routePods 分发 pod 子路径：
//   - ""                                              → GET   handleListPods
//   - "{ns}/{name}"                                   → DELETE handleDeletePod
//   - "{ns}/{name}/logs"                              → GET   handlePodLogs
func (s *Server) routePods(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	if sub == "" {
		if r.Method != http.MethodGet {
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleListPods(w, r, client)
		return
	}
	parts := strings.SplitN(sub, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "pod namespace and name required"})
		return
	}
	ns, name := parts[0], parts[1]
	// /logs 子路径：获取 pod 日志。
	if len(parts) == 3 && parts[2] == "logs" {
		if r.Method != http.MethodGet {
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handlePodLogs(w, r, client, ns, name)
		return
	}
	// {ns}/{name} 主路径：当前仅支持 DELETE。
	if len(parts) == 2 {
		if r.Method != http.MethodDelete {
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleDeletePod(w, r, client, ns, name)
		return
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown pod sub-path: " + sub})
}

// routeDeployments 分发 deployment 子路径：
//   - ""                                              → GET   handleListDeployments
//   - "{ns}/{name}/scale"                             → POST  handleScaleDeployment
//   - "{ns}/{name}/restart"                           → POST  handleRestartDeployment
//   - "{ns}/{name}/rollback"                          → POST  handleRollbackDeployment
func (s *Server) routeDeployments(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	if sub == "" {
		if r.Method != http.MethodGet {
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleListDeployments(w, r, client)
		return
	}
	parts := strings.SplitN(sub, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deployment namespace and name required"})
		return
	}
	ns, name := parts[0], parts[1]
	if len(parts) == 3 {
		switch parts[2] {
		case "scale":
			if r.Method != http.MethodPost {
				paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			s.handleScaleDeployment(w, r, client, ns, name)
			return
		case "restart":
			if r.Method != http.MethodPost {
				paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			s.handleRestartDeployment(w, r, client, ns, name)
			return
		case "rollback":
			if r.Method != http.MethodPost {
				paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			s.handleRollbackDeployment(w, r, client, ns, name)
			return
		}
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown deployment sub-path: " + sub})
}

// handleListNamespaces 处理 GET /api/v1/k8s/clusters/{id}/namespaces：列出所有 namespace。
// 返回 {namespaces: [{name, status, createdAt}]}。

func formatAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ============================================================================
// M3 集成：集群监控仪表盘 API
// ============================================================================
//
// API 端点（{id} 为集群 ID，由 handleK8sClusterRouting → handleK8sResourceRouting 分发）：
//   - GET /api/v1/k8s/clusters/{id}/dashboard          — 集群仪表盘汇总
//   - GET /api/v1/k8s/clusters/{id}/nodes/{node}/metrics — 节点指标
//   - GET /api/v1/k8s/clusters/{id}/health             — 集群健康检查
//
// 仪表盘汇总返回：
//   {
//     nodes: { total, ready, notReady },
//     pods: { total, running, pending, failed, succeeded },
//     deployments: { total, available, unavailable },
//     cpu: { usagePercent, totalCores, usedCores },
//     memory: { usagePercent, totalBytes, usedBytes },
//     storage: { usagePercent, totalBytes, usedBytes }
//   }

// routeNodes 分发 node 子路径：
//   - ""              → GET handleListNodes
//   - "{node}/metrics" → GET handleNodeMetrics
func (s *Server) routeNodes(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	if sub == "" {
		if r.Method != http.MethodGet {
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleListNodes(w, r, client)
		return
	}
	// sub 形如 "{node}/metrics"。
	parts := strings.SplitN(sub, "/", 2)
	if len(parts) == 2 && parts[1] == "metrics" {
		if r.Method != http.MethodGet {
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleNodeMetrics(w, r, client, parts[0])
		return
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown node sub-path: " + sub})
}

// ClusterDashboard 是集群仪表盘汇总响应结构。

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100.0
}
