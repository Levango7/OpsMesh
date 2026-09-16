// k8s_cluster_ops.go 实现 K8s 集群级资源管理 HTTP handler（dashboard/metrics/health/nodes）。
//
// 从 k8s_manage.go 拆分而来，包含 handleListNodes/handleClusterDashboard/
// handleNodeMetrics/handleClusterHealth 及相关类型定义。
package controlplane

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/k8s"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listNodes", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, n := range list.Items {
		status := "NotReady"
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				if cond.Status == corev1.ConditionTrue {
					status = "Ready"
				}
				break
			}
		}
		roles := make([]string, 0, 2)
		for k, v := range n.Labels {
			if strings.HasPrefix(k, "node-role.kubernetes.io/") && v == "" {
				roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
			}
		}
		sort.Strings(roles)
		if len(roles) == 0 {
			roles = []string{"worker"}
		}
		internalIP, externalIP := "", ""
		for _, addr := range n.Status.Addresses {
			switch addr.Type {
			case corev1.NodeInternalIP:
				internalIP = addr.Address
			case corev1.NodeExternalIP:
				externalIP = addr.Address
			}
		}
		cpu := n.Status.Capacity[corev1.ResourceCPU]
		memory := n.Status.Capacity[corev1.ResourceMemory]
		out = append(out, map[string]interface{}{
			"name":       n.Name,
			"status":     status,
			"roles":      roles,
			"version":    n.Status.NodeInfo.KubeletVersion,
			"internalIP": internalIP,
			"externalIP": externalIP,
			"cpu":        cpu.String(),
			"memory":     memory.String(),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"nodes": out})
}

// formatAge 将 K8s 资源的 CreationTimestamp 转为人类可读的"已运行时长"字符串。
// 返回示例：45s / 12m / 3h / 7d。零值返回空串。

type ClusterDashboard struct {
	Nodes       NodeSummary       `json:"nodes"`
	Pods        PodSummary        `json:"pods"`
	Deployments DeploymentSummary `json:"deployments"`
	CPU         ResourceUsage     `json:"cpu"`
	Memory      ResourceUsage     `json:"memory"`
	Storage     ResourceUsage     `json:"storage"`
}

// NodeSummary 节点状态汇总。
type NodeSummary struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	NotReady int `json:"notReady"`
}

// PodSummary Pod 状态汇总。
type PodSummary struct {
	Total     int `json:"total"`
	Running   int `json:"running"`
	Pending   int `json:"pending"`
	Failed    int `json:"failed"`
	Succeeded int `json:"succeeded"`
}

// DeploymentSummary Deployment 状态汇总。
type DeploymentSummary struct {
	Total       int `json:"total"`
	Available   int `json:"available"`
	Unavailable int `json:"unavailable"`
}

// ResourceUsage 资源使用率（CPU/内存/存储）。
type ResourceUsage struct {
	UsagePercent float64 `json:"usagePercent"`
	Total        float64 `json:"total"` // CPU=cores, 内存/存储=bytes
	Used         float64 `json:"used"`  // CPU=cores, 内存/存储=bytes
}

// handleClusterDashboard 处理 GET /api/v1/k8s/clusters/{id}/dashboard：集群仪表盘汇总。
// 聚合 Nodes/Pods/Deployments 状态 + CPU/内存/存储 使用率。
// 设计要点：
//   - 一次性 list nodes/pods/deployments，避免多次往返；
//   - CPU/内存使用率取节点 requests 之和 / capacity 之和（非实时使用，需 metrics-server 才能取真实使用）；
//   - 存储使用率取 PVC 容量之和（capacity vs requested）；
//   - 任何子资源 list 失败不阻断整体，对应字段归零。

func (s *Server) handleClusterDashboard(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()

	dash := ClusterDashboard{}

	// ---- 节点状态 + 容量汇总 ----
	nodeList, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		var totalCPU, totalMem float64
		for _, n := range nodeList.Items {
			dash.Nodes.Total++
			ready := false
			for _, cond := range n.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if ready {
				dash.Nodes.Ready++
			} else {
				dash.Nodes.NotReady++
			}
			// 容量汇总。
			cpu := n.Status.Capacity[corev1.ResourceCPU]
			mem := n.Status.Capacity[corev1.ResourceMemory]
			totalCPU += float64((&cpu).MilliValue()) / 1000.0
			totalMem += float64((&mem).Value())
		}
		dash.CPU.Total = totalCPU
		dash.Memory.Total = totalMem
	}

	// ---- Pod 状态汇总（跨所有 namespace） ----
	podList, err := client.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, p := range podList.Items {
			dash.Pods.Total++
			switch p.Status.Phase {
			case corev1.PodRunning:
				dash.Pods.Running++
			case corev1.PodPending:
				dash.Pods.Pending++
			case corev1.PodFailed:
				dash.Pods.Failed++
			case corev1.PodSucceeded:
				dash.Pods.Succeeded++
			}
		}
	}

	// ---- Deployment 状态 + 资源 requests 汇总 ----
	depList, err := client.Clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err == nil {
		var usedCPU, usedMem float64
		for _, d := range depList.Items {
			dash.Deployments.Total++
			if d.Status.UpdatedReplicas == *d.Spec.Replicas && d.Status.AvailableReplicas == *d.Spec.Replicas {
				dash.Deployments.Available++
			} else {
				dash.Deployments.Unavailable++
			}
			// 累加每个 container 的 requests。
			if d.Spec.Template.Spec.Containers != nil {
				replicas := int32(1)
				if d.Spec.Replicas != nil {
					replicas = *d.Spec.Replicas
				}
				for _, c := range d.Spec.Template.Spec.Containers {
					if c.Resources.Requests != nil {
						cpuReq := c.Resources.Requests[corev1.ResourceCPU]
						memReq := c.Resources.Requests[corev1.ResourceMemory]
						usedCPU += float64((&cpuReq).MilliValue()) / 1000.0 * float64(replicas)
						usedMem += float64((&memReq).Value()) * float64(replicas)
					}
				}
			}
		}
		dash.CPU.Used = usedCPU
		dash.Memory.Used = usedMem
	}

	// ---- 存储使用率（PVC 汇总） ----
	pvcList, err := client.Clientset.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err == nil {
		var totalPVC, usedPVC float64
		for _, pvc := range pvcList.Items {
			if cap, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
				totalPVC += float64((&cap).Value())
			}
			if req, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				usedPVC += float64((&req).Value())
			}
		}
		dash.Storage.Total = totalPVC
		dash.Storage.Used = usedPVC
	}

	// ---- 计算使用率百分比 ----
	if dash.CPU.Total > 0 {
		dash.CPU.UsagePercent = round2(dash.CPU.Used / dash.CPU.Total * 100)
	}
	if dash.Memory.Total > 0 {
		dash.Memory.UsagePercent = round2(dash.Memory.Used / dash.Memory.Total * 100)
	}
	if dash.Storage.Total > 0 {
		dash.Storage.UsagePercent = round2(dash.Storage.Used / dash.Storage.Total * 100)
	}

	paginate.WriteJSON(w, http.StatusOK, dash)
}

// handleNodeMetrics 处理 GET /api/v1/k8s/clusters/{id}/nodes/{node}/metrics：节点指标。
// 返回节点容量 + 已分配 requests + 使用率。
// 注：真实实时指标需 metrics-server（Kubernetes Metrics API），此处返回基于 Pod requests 的分配视图。
func (s *Server) handleNodeMetrics(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, nodeName string) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	if nodeName == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node name required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()

	node, err := client.Clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.getNode", err)
		return
	}

	// 节点容量。
	capCPUQty := node.Status.Capacity[corev1.ResourceCPU]
	capMemQty := node.Status.Capacity[corev1.ResourceMemory]
	capCPU := float64((&capCPUQty).MilliValue()) / 1000.0
	capMem := float64((&capMemQty).Value())

	// 通过 fieldSelector 查询本节点上的所有 Pod，累加 requests。
	podList, err := client.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	var usedCPU, usedMem float64
	if err == nil {
		for _, p := range podList.Items {
			if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodPending {
				continue
			}
			for _, c := range p.Spec.Containers {
				if c.Resources.Requests != nil {
					cpuReq := c.Resources.Requests[corev1.ResourceCPU]
					memReq := c.Resources.Requests[corev1.ResourceMemory]
					usedCPU += float64((&cpuReq).MilliValue()) / 1000.0
					usedMem += float64((&memReq).Value())
				}
			}
		}
	}

	cpuPercent, memPercent := 0.0, 0.0
	if capCPU > 0 {
		cpuPercent = round2(usedCPU / capCPU * 100)
	}
	if capMem > 0 {
		memPercent = round2(usedMem / capMem * 100)
	}

	// 节点角色。
	roles := make([]string, 0, 2)
	for k, v := range node.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") && v == "" {
			roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
		}
	}
	sort.Strings(roles)
	if len(roles) == 0 {
		roles = []string{"worker"}
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"name":   node.Name,
		"roles":  roles,
		"cpu":    ResourceUsage{UsagePercent: cpuPercent, Total: round2(capCPU), Used: round2(usedCPU)},
		"memory": ResourceUsage{UsagePercent: memPercent, Total: capMem, Used: usedMem},
	})
}

// ClusterHealth 是集群健康检查响应结构。

type ClusterHealth struct {
	Status     string            `json:"status"` // healthy / degraded / unhealthy
	Nodes      NodeSummary       `json:"nodes"`
	Pods       PodSummary        `json:"pods"`
	Components []ComponentHealth `json:"components"`
	Checks     []HealthCheck     `json:"checks"`
}

// ComponentHealth 描述 K8s 控制面组件健康状态。
type ComponentHealth struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Message string `json:"message,omitempty"`
}

// HealthCheck 描述一项健康检查项的结果。
type HealthCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// handleClusterHealth 处理 GET /api/v1/k8s/clusters/{id}/health：集群健康检查。
// 检查项：
//   - 节点是否全部 Ready；
//   - 是否存在 Failed Pod；
//   - 控制面组件（scheduler/controller-manager/etcd）是否 Healthy；
//   - API server 是否可访问（已隐含在本请求成功中）。
func (s *Server) handleClusterHealth(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()

	health := ClusterHealth{Status: "healthy"}

	// ---- 节点健康 ----
	nodeList, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		health.Status = "unhealthy"
		health.Checks = append(health.Checks, HealthCheck{
			Name: "list-nodes", Passed: false, Message: err.Error(),
		})
		paginate.WriteJSON(w, http.StatusOK, health)
		return
	}
	for _, n := range nodeList.Items {
		health.Nodes.Total++
		ready := false
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if ready {
			health.Nodes.Ready++
		} else {
			health.Nodes.NotReady++
		}
	}
	health.Checks = append(health.Checks, HealthCheck{
		Name:   "all-nodes-ready",
		Passed: health.Nodes.NotReady == 0,
		Message: fmt.Sprintf("%d ready / %d not-ready / %d total",
			health.Nodes.Ready, health.Nodes.NotReady, health.Nodes.Total),
	})
	if health.Nodes.NotReady > 0 {
		health.Status = "degraded"
	}

	// ---- Pod 失败检查 ----
	podList, err := client.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, p := range podList.Items {
			health.Pods.Total++
			switch p.Status.Phase {
			case corev1.PodRunning:
				health.Pods.Running++
			case corev1.PodPending:
				health.Pods.Pending++
			case corev1.PodFailed:
				health.Pods.Failed++
			case corev1.PodSucceeded:
				health.Pods.Succeeded++
			}
		}
	}
	health.Checks = append(health.Checks, HealthCheck{
		Name:    "no-failed-pods",
		Passed:  health.Pods.Failed == 0,
		Message: fmt.Sprintf("%d failed / %d pending / %d running", health.Pods.Failed, health.Pods.Pending, health.Pods.Running),
	})
	if health.Pods.Failed > 0 {
		health.Status = "degraded"
	}

	// ---- 控制面组件健康 ----
	compList, err := client.Clientset.CoreV1().ComponentStatuses().List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, c := range compList.Items {
			healthy := false
			msg := ""
			for _, cond := range c.Conditions {
				if cond.Type == "Healthy" {
					healthy = cond.Status == corev1.ConditionTrue
					msg = cond.Message
					break
				}
			}
			health.Components = append(health.Components, ComponentHealth{
				Name: c.Name, Healthy: healthy, Message: msg,
			})
			if !healthy {
				health.Status = "degraded"
			}
		}
	}
	// ComponentStatuses 在新版 K8s 已废弃，可能返回空列表；不视为错误。

	paginate.WriteJSON(w, http.StatusOK, health)
}

// round2 保留两位小数。
