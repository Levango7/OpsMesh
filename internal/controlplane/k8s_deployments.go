// k8s_deployments.go 实现 K8s Deployment/Service/ConfigMap/Secret 资源管理 HTTP handler。
//
// 从 k8s_manage.go 拆分而来，包含 handleListDeployments/handleScaleDeployment/
// handleRestartDeployment/handleRollbackDeployment/handleListServices/
// handleListConfigMaps/handleListSecrets。
package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/k8s"
	"opsmesh/internal/proto"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listDeployments", err)
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deployments": out})
}

// handleScaleDeployment 处理 POST /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/scale：扩缩容。
// 请求体：{replicas: 3}。
// 行为：GetScale → 修改 Spec.Replicas → UpdateScale（保持 ResourceVersion 一致）。
// 返回 {name, replicas}。
func (s *Server) handleScaleDeployment(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:write")
	if !ok {
		return
	}
	var body struct {
		Replicas int32 `json:"replicas"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	scale, err := client.Clientset.AppsV1().Deployments(ns).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.getScale", err)
		return
	}
	scale.Spec.Replicas = body.Replicas
	updated, err := client.Clientset.AppsV1().Deployments(ns).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.updateScale", err)
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_deployment_scale", Target: ns + "/" + name,
		Detail: fmt.Sprintf("replicas=%d", body.Replicas),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"name":     name,
		"replicas": updated.Spec.Replicas,
	})
}

// handleRestartDeployment 处理 POST /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/restart：滚动重启。
// 通过 strategic merge patch 修改 template.metadata.annotations[kubectl.kubernetes.io/restartedAt]，
// 触发 deployment 控制器滚动更新（与 kubectl rollout restart 等价）。
// 返回 {status: "restarted", restartedAt: "..."}。
func (s *Server) handleRestartDeployment(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:write")
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
		writeInternalError(r.Context(), w, "k8s.restartDeployment", err)
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_deployment_restart", Target: ns + "/" + name,
		Detail: "restartedAt=" + now,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "restarted", "restartedAt": now})
}

// handleRollbackDeployment 处理 POST /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/rollback：回滚到上一 revision。
//
// 实现思路（与 kubectl rollout undo 等价，无 kubectl 依赖）：
//  1. Get 当前 deployment，读取 annotations["deployment.kubernetes.io/revision"] 得到当前 revision；
//  2. 目标 revision = 当前 revision - 1（若 ≤1 表示无历史可回滚，返回 400）；
//  3. List 该 namespace 下所有 ReplicaSet，通过 ControllerRef 过滤出属于此 deployment 的 ReplicaSet，
//     再匹配 annotations["deployment.kubernetes.io/revision"] == 目标 revision；
//  4. 将目标 ReplicaSet 的 Spec.Template 通过 StrategicMergePatch 写回 deployment 的 Spec.Template，
//     触发 deployment controller 滚动更新到目标 revision 的 Pod 模板。
//
// 返回 {status: "rolled back", fromRevision, toRevision}。
func (s *Server) handleRollbackDeployment(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:write")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()

	// 1. 获取当前 deployment，读取 revision annotation。
	dep, err := client.Clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.getDeployment", err)
		return
	}
	currentRevStr := dep.Annotations["deployment.kubernetes.io/revision"]
	if currentRevStr == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deployment has no revision annotation"})
		return
	}
	currentRev, err := strconv.ParseInt(currentRevStr, 10, 64)
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.parseCurrentRevision", err)
		return
	}
	if currentRev <= 1 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deployment is at initial revision, no history to rollback"})
		return
	}
	targetRev := currentRev - 1
	targetRevStr := strconv.FormatInt(targetRev, 10)

	// 2. 列出 ReplicaSet，找到属于此 deployment 且 revision 为 targetRev 的 ReplicaSet。
	rsList, err := client.Clientset.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listReplicaSets", err)
		return
	}
	var targetRS *appsv1.ReplicaSet
	for i := range rsList.Items {
		rs := &rsList.Items[i]
		if !metav1.IsControlledBy(rs, dep) {
			continue
		}
		if rs.Annotations["deployment.kubernetes.io/revision"] == targetRevStr {
			targetRS = rs
			break
		}
	}
	if targetRS == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("replicaset with revision %d not found", targetRev)})
		return
	}

	// 3. 将目标 ReplicaSet 的 template patch 回 deployment（StrategicMergePatch）。
	templateBytes, err := json.Marshal(targetRS.Spec.Template)
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.marshalTemplate", err)
		return
	}
	patch := fmt.Sprintf(`{"spec":{"template":%s}}`, string(templateBytes))
	if _, err := client.Clientset.AppsV1().Deployments(ns).Patch(
		ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
		writeInternalError(r.Context(), w, "k8s.rollbackDeployment", err)
		return
	}

	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_deployment_rollback", Target: ns + "/" + name,
		Detail: fmt.Sprintf("from revision %d to %d", currentRev, targetRev),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "rolled back",
		"fromRevision": currentRev,
		"toRevision":   targetRev,
	})
}

// handleListServices 处理 GET /api/v1/k8s/clusters/{id}/services?namespace={ns}：列出 service。
// 返回 {services: [{name, namespace, type, clusterIP, externalIP, ports}]}。
// ports: [{name, port, targetPort, protocol}]；externalIP 取 LoadBalancer Ingress 第一个 IP/Host。
func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listServices", err)
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"services": out})
}

// handleListConfigMaps 处理 GET /api/v1/k8s/clusters/{id}/configmaps?namespace={ns}：列出 configmap。
// 返回 {configmaps: [{name, namespace, dataKeys}]}。
// dataKeys 为 Data map 的 key 列表（已排序），不返回 value（避免大体积响应）。
func (s *Server) handleListConfigMaps(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listConfigMaps", err)
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"configmaps": out})
}

// handleListSecrets 处理 GET /api/v1/k8s/clusters/{id}/secrets?namespace={ns}：列出 secret。
// 返回 {secrets: [{name, namespace, type, dataKeys}]}。
// 安全约束：仅返回 key 名（dataKeys），不返回 secret 值（避免敏感内容泄露到前端）。
func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listSecrets", err)
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"secrets": out})
}

// handleListNodes 处理 GET /api/v1/k8s/clusters/{id}/nodes：列出 node。
// 返回 {nodes: [{name, status, roles, version, internalIP, externalIP, cpu, memory}]}。
// 字段说明：
//   - status：Ready/NotReady（取 NodeReady condition）；
//   - roles：从 labels[node-role.kubernetes.io/*] 提取，无角色标签时默认 ["worker"]；
//   - cpu/memory：取 Status.Capacity（节点总容量，非已分配）。
