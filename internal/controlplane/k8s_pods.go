// k8s_pods.go 实现 K8s Pod 与 Namespace 资源管理 HTTP handler。
//
// 从 k8s_manage.go 拆分而来，包含 handleListNamespaces/handleListPods/handlePodLogs/handleDeletePod。
package controlplane

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/k8s"
	"github.com/Levango7/OpsMesh/internal/proto"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) handleListNamespaces(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listNamespaces", err)
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"namespaces": out})
}

// handleListPods 处理 GET /api/v1/k8s/clusters/{id}/pods?namespace={ns}：列出 pod。
// namespace 为空时跨所有 namespace 列出（client-go 支持 ns="" 表示 all namespaces）。
// 返回 {pods: [{name, namespace, status, podIP, nodeIP, restarts, age}]}。
func (s *Server) handleListPods(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listPods", err)
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"pods": out})
}

// handlePodLogs 处理 GET /api/v1/k8s/clusters/{id}/pods/{ns}/{name}/logs：获取 pod 日志。
// 查询参数：
//   - ?container={name}：指定容器（多容器 pod 时必填，单容器可省略）；
//   - ?tailLines=N：仅返回最后 N 行（默认不限制，由 K8s API 决定）。
//
// 返回 {logs: "..."}。
func (s *Server) handlePodLogs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
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
		writeInternalError(r.Context(), w, "k8s.getPodLogs", err)
		return
	}
	defer stream.Close()
	const maxPodLogBytes = 2 << 20 // ：2MB 上限，防超大日志打爆控制面内存
	data, err := io.ReadAll(io.LimitReader(stream, maxPodLogBytes))
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.readPodLogs", err)
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"logs": string(data)})
}

// handleDeletePod 处理 DELETE /api/v1/k8s/clusters/{id}/pods/{ns}/{name}：删除 pod。
// 删除由 deployment/replicaset 控制器托管的 pod 时，控制器会立即拉起新 pod。
// 返回 204 No Content。
func (s *Server) handleDeletePod(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:delete")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	if err := client.Clientset.CoreV1().Pods(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		writeInternalError(r.Context(), w, "k8s.deletePod", err)
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_pod_delete", Target: ns + "/" + name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleListDeployments 处理 GET /api/v1/k8s/clusters/{id}/deployments?namespace={ns}：列出 deployment。
// 返回 {deployments: [{name, namespace, replicas, availableReplicas, image}]}。
// image 取 template 中第一个容器的 image（多容器 pod 仅展示主容器）。
