
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
package controlplane

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"opsmesh/internal/k8s"
	"opsmesh/internal/proto"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cluster manager not initialized"})
		return
	}
	client, err := s.clusterMgr.GetClient(clusterID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not connected: " + err.Error()})
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
		s.handleListNodes(w, r, client)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown resource: " + resource})
	}
}

// routePods 分发 pod 子路径：
//   - ""                                              → GET   handleListPods
//   - "{ns}/{name}"                                   → DELETE handleDeletePod
//   - "{ns}/{name}/logs"                              → GET   handlePodLogs
func (s *Server) routePods(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	if sub == "" {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleListPods(w, r, client)
		return
	}
	parts := strings.SplitN(sub, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pod namespace and name required"})
		return
	}
	ns, name := parts[0], parts[1]
	// /logs 子路径：获取 pod 日志。
	if len(parts) == 3 && parts[2] == "logs" {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handlePodLogs(w, r, client, ns, name)
		return
	}
	// {ns}/{name} 主路径：当前仅支持 DELETE。
	if len(parts) == 2 {
		if r.Method != http.MethodDelete {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleDeletePod(w, r, client, ns, name)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown pod sub-path: " + sub})
}

// routeDeployments 分发 deployment 子路径：
//   - ""                                              → GET   handleListDeployments
//   - "{ns}/{name}/scale"                             → POST  handleScaleDeployment
//   - "{ns}/{name}/restart"                           → POST  handleRestartDeployment
func (s *Server) routeDeployments(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	if sub == "" {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleListDeployments(w, r, client)
		return
	}
	parts := strings.SplitN(sub, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "deployment namespace and name required"})
		return
	}
	ns, name := parts[0], parts[1]
	if len(parts) == 3 {
		switch parts[2] {
		case "scale":
			if r.Method != http.MethodPost {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			s.handleScaleDeployment(w, r, client, ns, name)
			return
		case "restart":
			if r.Method != http.MethodPost {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			s.handleRestartDeployment(w, r, client, ns, name)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown deployment sub-path: " + sub})
}

// handleListNamespaces 处理 GET /api/v1/k8s/clusters/{id}/namespaces：列出所有 namespace。
// 返回 {namespaces: [{name, status, createdAt}]}。
func (s *Server) handleListNamespaces(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list namespaces failed: " + err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, ns := range list.Items {
		status := "Active"
		if ns.Status.Phase != "" {
			status = string(ns.Status.Phase)
		}
		out = append(out, map[string]interface{}{
			"name":      ns.Name,
			"status":    status,
			"createdAt": ns.CreationTimestamp.Time.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"namespaces": out})
}

// handleListPods 处理 GET /api/v1/k8s/clusters/{id}/pods?namespace={ns}：列出 pod。
// namespace 为空时跨所有 namespace 列出（client-go 支持 ns="" 表示 all namespaces）。
// 返回 {pods: [{name, namespace, status, podIP, nodeIP, restarts, age}]}。
func (s *Server) handleListPods(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list pods failed: " + err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, pod := range list.Items {
		restarts := 0
		for _, cs := range pod.Status.ContainerStatuses {
			restarts += int(cs.RestartCount)
		}
		status := string(pod.Status.Phase)
		if status == "" {
			status = "Unknown"
		}
		out = append(out, map[string]interface{}{
			"name":      pod.Name,
			"namespace": pod.Namespace,
			"status":    status,
			"podIP":     pod.Status.PodIP,
			"nodeIP":    pod.Status.HostIP,
			"restarts":  restarts,
			"age":       formatAge(pod.CreationTimestamp.Time),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"pods": out})
}

// handlePodLogs 处理 GET /api/v1/k8s/clusters/{id}/pods/{ns}/{name}/logs：获取 pod 日志。
// 查询参数：
//   - ?container={name}：指定容器（多容器 pod 时必填，单容器可省略）；
//   - ?tailLines=N：仅返回最后 N 行（默认不限制，由 K8s API 决定）。
//
// 返回 {logs: "..."}。
func (s *Server) handlePodLogs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	opts := &corev1.PodLogOptions{}
	if c := r.URL.Query().Get("container"); c != "" {
		opts.Container = c
	}
	if tl := r.URL.Query().Get("tailLines"); tl != "" {
		if n, err := strconv.ParseInt(tl, 10, 64); err == nil && n > 0 {
			opts.TailLines = &n
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	stream, err := client.Clientset.CoreV1().Pods(ns).GetLogs(name, opts).Stream(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get pod logs failed: " + err.Error()})
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read pod logs failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": string(data)})
}

// handleDeletePod 处理 DELETE /api/v1/k8s/clusters/{id}/pods/{ns}/{name}：删除 pod。
// 删除由 deployment/replicaset 控制器托管的 pod 时，控制器会立即拉起新 pod。
// 返回 204 No Content。
func (s *Server) handleDeletePod(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "user:delete")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	if err := client.Clientset.CoreV1().Pods(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete pod failed: " + err.Error()})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "k8s_pod_delete", Target: ns + "/" + name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleListDeployments 处理 GET /api/v1/k8s/clusters/{id}/deployments?namespace={ns}：列出 deployment。
// 返回 {deployments: [{name, namespace, replicas, availableReplicas, image}]}。
// image 取 template 中第一个容器的 image（多容器 pod 仅展示主容器）。
func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list deployments failed: " + err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, d := range list.Items {
		image := ""
		if len(d.Spec.Template.Spec.Containers) > 0 {
			image = d.Spec.Template.Spec.Containers[0].Image
		}
		out = append(out, map[string]interface{}{
			"name":              d.Name,
			"namespace":         d.Namespace,
			"replicas":          d.Status.Replicas,
			"availableReplicas": d.Status.AvailableReplicas,
			"image":             image,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"deployments": out})
}

// handleScaleDeployment 处理 POST /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/scale：扩缩容。
// 请求体：{replicas: 3}。
// 行为：GetScale → 修改 Spec.Replicas → UpdateScale（保持 ResourceVersion 一致）。
// 返回 {name, replicas}。
func (s *Server) handleScaleDeployment(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	var body struct {
		Replicas int32 `json:"replicas"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	scale, err := client.Clientset.AppsV1().Deployments(ns).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get scale failed: " + err.Error()})
		return
	}
	scale.Spec.Replicas = body.Replicas
	updated, err := client.Clientset.AppsV1().Deployments(ns).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "update scale failed: " + err.Error()})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "k8s_deployment_scale", Target: ns + "/" + name,
		Detail: fmt.Sprintf("replicas=%d", body.Replicas),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":     name,
		"replicas": updated.Spec.Replicas,
	})
}

// handleRestartDeployment 处理 POST /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/restart：滚动重启。
// 通过 strategic merge patch 修改 template.metadata.annotations[kubectl.kubernetes.io/restartedAt]，
// 触发 deployment 控制器滚动更新（与 kubectl rollout restart 等价）。
// 返回 {status: "restarted", restartedAt: "..."}。
func (s *Server) handleRestartDeployment(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// StrategicMergePatch：仅覆盖 annotations 中的 restartedAt 键，不影响其他元数据。
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, now)
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	if _, err := client.Clientset.AppsV1().Deployments(ns).Patch(
		ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "restart deployment failed: " + err.Error()})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "k8s_deployment_restart", Target: ns + "/" + name,
		Detail: "restartedAt=" + now,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarted", "restartedAt": now})
}

// handleListServices 处理 GET /api/v1/k8s/clusters/{id}/services?namespace={ns}：列出 service。
// 返回 {services: [{name, namespace, type, clusterIP, externalIP, ports}]}。
// ports: [{name, port, targetPort, protocol}]；externalIP 取 LoadBalancer Ingress 第一个 IP/Host。
func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list services failed: " + err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, svc := range list.Items {
		ports := make([]map[string]interface{}, 0, len(svc.Spec.Ports))
		for _, p := range svc.Spec.Ports {
			ports = append(ports, map[string]interface{}{
				"name":       p.Name,
				"port":       p.Port,
				"targetPort": p.TargetPort.IntValue(),
				"protocol":   string(p.Protocol),
			})
		}
		extIP := ""
		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			if svc.Status.LoadBalancer.Ingress[0].IP != "" {
				extIP = svc.Status.LoadBalancer.Ingress[0].IP
			} else {
				extIP = svc.Status.LoadBalancer.Ingress[0].Hostname
			}
		}
		out = append(out, map[string]interface{}{
			"name":       svc.Name,
			"namespace":  svc.Namespace,
			"type":       string(svc.Spec.Type),
			"clusterIP":  svc.Spec.ClusterIP,
			"externalIP": extIP,
			"ports":      ports,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"services": out})
}

// handleListConfigMaps 处理 GET /api/v1/k8s/clusters/{id}/configmaps?namespace={ns}：列出 configmap。
// 返回 {configmaps: [{name, namespace, dataKeys}]}。
// dataKeys 为 Data map 的 key 列表（已排序），不返回 value（避免大体积响应）。
func (s *Server) handleListConfigMaps(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list configmaps failed: " + err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, cm := range list.Items {
		keys := make([]string, 0, len(cm.Data))
		for k := range cm.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, map[string]interface{}{
			"name":      cm.Name,
			"namespace": cm.Namespace,
			"dataKeys":  keys,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"configmaps": out})
}

// handleListSecrets 处理 GET /api/v1/k8s/clusters/{id}/secrets?namespace={ns}：列出 secret。
// 返回 {secrets: [{name, namespace, type, dataKeys}]}。
// 安全约束：仅返回 key 名（dataKeys），不返回 secret 值（避免敏感内容泄露到前端）。
func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list secrets failed: " + err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, sec := range list.Items {
		keys := make([]string, 0, len(sec.Data))
		for k := range sec.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, map[string]interface{}{
			"name":      sec.Name,
			"namespace": sec.Namespace,
			"type":      string(sec.Type),
			"dataKeys":  keys,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"secrets": out})
}

// handleListNodes 处理 GET /api/v1/k8s/clusters/{id}/nodes：列出 node。
// 返回 {nodes: [{name, status, roles, version, internalIP, externalIP, cpu, memory}]}。
// 字段说明：
//   - status：Ready/NotReady（取 NodeReady condition）；
//   - roles：从 labels[node-role.kubernetes.io/*] 提取，无角色标签时默认 ["worker"]；
//   - cpu/memory：取 Status.Capacity（节点总容量，非已分配）。
func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list nodes failed: " + err.Error()})
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": out})
}

// formatAge 将 K8s 资源的 CreationTimestamp 转为人类可读的"已运行时长"字符串。
// 返回示例：45s / 12m / 3h / 7d。零值返回空串。
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