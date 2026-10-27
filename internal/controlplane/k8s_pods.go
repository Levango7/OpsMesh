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

// containerStateString 返回容器的当前状态字符串。
func containerStateString(cs corev1.ContainerStatus) string {
	if cs.State.Running != nil {
		return "Running"
	}
	if cs.State.Waiting != nil {
		return cs.State.Waiting.Reason
	}
	if cs.State.Terminated != nil {
		return cs.State.Terminated.Reason
	}
	return "Unknown"
}

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
		// 容器详情（名称、镜像、就绪状态、重启次数、状态）
		containers := make([]map[string]interface{}, 0, len(pod.Spec.Containers))
		for i, c := range pod.Spec.Containers {
			cr := map[string]interface{}{
				"name":  c.Name,
				"image": c.Image,
			}
			// 探针配置
			if c.LivenessProbe != nil {
				cr["livenessProbe"] = true
			}
			if c.ReadinessProbe != nil {
				cr["readinessProbe"] = true
			}
			if c.StartupProbe != nil {
				cr["startupProbe"] = true
			}
			// 容器状态
			if i < len(pod.Status.ContainerStatuses) {
				cs := pod.Status.ContainerStatuses[i]
				cr["ready"] = cs.Ready
				cr["restartCount"] = cs.RestartCount
				cr["state"] = containerStateString(cs)
			}
			containers = append(containers, cr)
		}
		// 污点容忍度
		tolerations := make([]string, 0, len(pod.Spec.Tolerations))
		for _, t := range pod.Spec.Tolerations {
			tol := t.Key
			if t.Operator != "" {
				tol += " " + string(t.Operator)
			}
			if t.Value != "" {
				tol += "=" + t.Value
			}
			if t.Effect != "" {
				tol += " " + string(t.Effect)
			}
			tolerations = append(tolerations, tol)
		}
		// 亲合度
		affinity := ""
		if pod.Spec.Affinity != nil {
			if pod.Spec.Affinity.NodeAffinity != nil {
				affinity = "nodeAffinity"
			}
			if pod.Spec.Affinity.PodAffinity != nil {
				if affinity != "" {
					affinity += ","
				}
				affinity += "podAffinity"
			}
			if pod.Spec.Affinity.PodAntiAffinity != nil {
				if affinity != "" {
					affinity += ","
				}
				affinity += "podAntiAffinity"
			}
		}
		// 镜像列表（去重）
		images := make([]string, 0)
		imageSet := make(map[string]bool)
		for _, c := range pod.Spec.Containers {
			if !imageSet[c.Image] {
				imageSet[c.Image] = true
				images = append(images, c.Image)
			}
		}
		for _, c := range pod.Spec.InitContainers {
			if !imageSet[c.Image] {
				imageSet[c.Image] = true
				images = append(images, c.Image)
			}
		}
		out = append(out, map[string]interface{}{
			"name":        pod.Name,
			"namespace":   pod.Namespace,
			"status":      status,
			"podIP":       pod.Status.PodIP,
			"nodeIP":      pod.Status.HostIP,
			"nodeName":    pod.Spec.NodeName,
			"restarts":    restarts,
			"age":         formatAge(pod.CreationTimestamp.Time),
			"containers":  containers,
			"images":      images,
			"tolerations": tolerations,
			"affinity":    affinity,
			"sidecars":    len(pod.Spec.Containers) > 1,
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
