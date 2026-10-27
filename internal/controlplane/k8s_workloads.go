// k8s_workloads.go 实现 K8s 工作负载与存储/网络/RBAC/CRD 资源管理 HTTP handler。
package controlplane

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/k8s"
	"github.com/Levango7/OpsMesh/internal/proto"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ---------- StatefulSet ----------

func (s *Server) handleListStatefulSets(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listStatefulSets", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, ss := range list.Items {
		image := ""
		if len(ss.Spec.Template.Spec.Containers) > 0 {
			image = ss.Spec.Template.Spec.Containers[0].Image
		}
		out = append(out, map[string]interface{}{
			"name":        ss.Name,
			"namespace":   ss.Namespace,
			"replicas":    ss.Status.Replicas,
			"ready":       ss.Status.ReadyReplicas,
			"image":       image,
			"serviceName": ss.Spec.ServiceName,
			"age":         formatAge(ss.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"statefulsets": out})
}

func (s *Server) handleScaleStatefulSet(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
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
	scale, err := client.Clientset.AppsV1().StatefulSets(ns).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.getStatefulSetScale", err)
		return
	}
	scale.Spec.Replicas = body.Replicas
	updated, err := client.Clientset.AppsV1().StatefulSets(ns).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.updateStatefulSetScale", err)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_statefulset_scale", Target: ns + "/" + name,
		Detail: fmt.Sprintf("replicas=%d", body.Replicas),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"name":     name,
		"replicas": updated.Spec.Replicas,
	})
}

func (s *Server) handleRestartStatefulSet(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:write")
	if !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, now)
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	if _, err := client.Clientset.AppsV1().StatefulSets(ns).Patch(
		ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
		writeInternalError(r.Context(), w, "k8s.restartStatefulSet", err)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_statefulset_restart", Target: ns + "/" + name,
		Detail: "restartedAt=" + now,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "restarted", "restartedAt": now})
}

// ---------- DaemonSet ----------

func (s *Server) handleListDaemonSets(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listDaemonSets", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, ds := range list.Items {
		image := ""
		if len(ds.Spec.Template.Spec.Containers) > 0 {
			image = ds.Spec.Template.Spec.Containers[0].Image
		}
		out = append(out, map[string]interface{}{
			"name":      ds.Name,
			"namespace": ds.Namespace,
			"desired":   ds.Status.DesiredNumberScheduled,
			"ready":     ds.Status.NumberReady,
			"available": ds.Status.NumberAvailable,
			"image":     image,
			"age":       formatAge(ds.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"daemonsets": out})
}

func (s *Server) handleRestartDaemonSet(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:write")
	if !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, now)
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	if _, err := client.Clientset.AppsV1().DaemonSets(ns).Patch(
		ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
		writeInternalError(r.Context(), w, "k8s.restartDaemonSet", err)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_daemonset_restart", Target: ns + "/" + name,
		Detail: "restartedAt=" + now,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "restarted", "restartedAt": now})
}

// ---------- ReplicaSet ----------

func (s *Server) handleListReplicaSets(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listReplicaSets", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, rs := range list.Items {
		image := ""
		if len(rs.Spec.Template.Spec.Containers) > 0 {
			image = rs.Spec.Template.Spec.Containers[0].Image
		}
		out = append(out, map[string]interface{}{
			"name":      rs.Name,
			"namespace": rs.Namespace,
			"replicas":  rs.Status.Replicas,
			"ready":     rs.Status.ReadyReplicas,
			"image":     image,
			"age":       formatAge(rs.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"replicasets": out})
}

// ---------- Job ----------

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listJobs", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, job := range list.Items {
		status := "Running"
		if job.Status.Succeeded > 0 {
			status = "Succeeded"
		} else if job.Status.Failed > 0 {
			status = "Failed"
		} else if job.Status.Active == 0 {
			status = "Pending"
		}
		out = append(out, map[string]interface{}{
			"name":      job.Name,
			"namespace": job.Namespace,
			"status":    status,
			"succeeded": job.Status.Succeeded,
			"failed":    job.Status.Failed,
			"active":    job.Status.Active,
			"age":       formatAge(job.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"jobs": out})
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:delete")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	if err := client.Clientset.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		writeInternalError(r.Context(), w, "k8s.deleteJob", err)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_job_delete", Target: ns + "/" + name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---------- CronJob ----------

func (s *Server) handleListCronJobs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listCronJobs", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, cj := range list.Items {
		status := "Active"
		if cj.Spec.Suspend != nil && *cj.Spec.Suspend {
			status = "Suspended"
		}
		out = append(out, map[string]interface{}{
			"name":      cj.Name,
			"namespace": cj.Namespace,
			"schedule":  cj.Spec.Schedule,
			"status":    status,
			"suspend":   cj.Spec.Suspend,
			"age":       formatAge(cj.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"cronjobs": out})
}

func (s *Server) handleDeleteCronJob(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, ns, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:delete")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	if err := client.Clientset.BatchV1().CronJobs(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		writeInternalError(r.Context(), w, "k8s.deleteCronJob", err)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_cronjob_delete", Target: ns + "/" + name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Ingress ----------

func (s *Server) handleListIngresses(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listIngresses", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, ing := range list.Items {
		hosts := make([]string, 0)
		for _, rule := range ing.Spec.Rules {
			if rule.Host != "" {
				hosts = append(hosts, rule.Host)
			}
		}
		class := ""
		if ing.Spec.IngressClassName != nil {
			class = *ing.Spec.IngressClassName
		}
		out = append(out, map[string]interface{}{
			"name":      ing.Name,
			"namespace": ing.Namespace,
			"class":     class,
			"hosts":     hosts,
			"age":       formatAge(ing.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"ingresses": out})
}

// ---------- PVC ----------

func (s *Server) handleListPVCs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listPVCs", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, pvc := range list.Items {
		capacity := ""
		if cap, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			capacity = cap.String()
		}
		sc := ""
		if pvc.Spec.StorageClassName != nil {
			sc = *pvc.Spec.StorageClassName
		}
		out = append(out, map[string]interface{}{
			"name":         pvc.Name,
			"namespace":    pvc.Namespace,
			"status":       string(pvc.Status.Phase),
			"capacity":     capacity,
			"storageClass": sc,
			"volumeName":   pvc.Spec.VolumeName,
			"age":          formatAge(pvc.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"pvcs": out})
}

// ---------- PV ----------

func (s *Server) handleListPVs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listPVs", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, pv := range list.Items {
		capacity := ""
		if cap, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
			capacity = cap.String()
		}
		claimRef := ""
		if pv.Spec.ClaimRef != nil {
			claimRef = pv.Spec.ClaimRef.Namespace + "/" + pv.Spec.ClaimRef.Name
		}
		out = append(out, map[string]interface{}{
			"name":         pv.Name,
			"status":       string(pv.Status.Phase),
			"capacity":     capacity,
			"storageClass": pv.Spec.StorageClassName,
			"claimRef":     claimRef,
			"age":          formatAge(pv.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"pvs": out})
}

// ---------- HPA ----------

func (s *Server) handleListHPAs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listHPAs", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, hpa := range list.Items {
		out = append(out, map[string]interface{}{
			"name":        hpa.Name,
			"namespace":   hpa.Namespace,
			"target":      hpa.Spec.ScaleTargetRef.Name,
			"minReplicas": hpa.Spec.MinReplicas,
			"maxReplicas": hpa.Spec.MaxReplicas,
			"current":     hpa.Status.CurrentReplicas,
			"desired":     hpa.Status.DesiredReplicas,
			"age":         formatAge(hpa.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"hpas": out})
}

// ---------- RBAC ----------

func (s *Server) handleListK8sRoles(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.RbacV1().Roles(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listRoles", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, role := range list.Items {
		out = append(out, map[string]interface{}{
			"name":      role.Name,
			"namespace": role.Namespace,
			"rules":     len(role.Rules),
			"age":       formatAge(role.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"roles": out})
}

func (s *Server) handleListRoleBindings(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listRoleBindings", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, rb := range list.Items {
		out = append(out, map[string]interface{}{
			"name":      rb.Name,
			"namespace": rb.Namespace,
			"roleRef":   rb.RoleRef.Name,
			"subjects":  len(rb.Subjects),
			"age":       formatAge(rb.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"rolebindings": out})
}

func (s *Server) handleListClusterRoles(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listClusterRoles", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, cr := range list.Items {
		out = append(out, map[string]interface{}{
			"name":  cr.Name,
			"rules": len(cr.Rules),
			"age":   formatAge(cr.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"clusterroles": out})
}

func (s *Server) handleListClusterRoleBindings(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listClusterRoleBindings", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, crb := range list.Items {
		out = append(out, map[string]interface{}{
			"name":     crb.Name,
			"roleRef":  crb.RoleRef.Name,
			"subjects": len(crb.Subjects),
			"age":      formatAge(crb.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"clusterrolebindings": out})
}

// ---------- CRD ----------

func (s *Server) handleListCRDs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	_, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	gvrs, err := client.Clientset.Discovery().ServerResourcesForGroupVersion("apiextensions.k8s.io/v1")
	if err != nil {
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"crds": []map[string]interface{}{}})
		return
	}
	out := make([]map[string]interface{}, 0)
	for _, r := range gvrs.APIResources {
		out = append(out, map[string]interface{}{
			"name": r.Name,
			"kind": r.Kind,
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"crds": out})
}

// ---------- Namespace create/delete ----------

func (s *Server) handleCreateNamespace(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	caller, ok := s.requirePermission(w, r, "k8s:write")
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: body.Name},
	}
	created, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.createNamespace", err)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_namespace_create", Target: body.Name,
	})
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"name":      created.Name,
		"status":    string(created.Status.Phase),
		"createdAt": created.CreationTimestamp.Time.UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleDeleteNamespace(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, name string) {
	caller, ok := s.requirePermission(w, r, "k8s:delete")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	if err := client.Clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		writeInternalError(r.Context(), w, "k8s.deleteNamespace", err)
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "k8s_namespace_delete", Target: name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Sub-path routers ----------

func (s *Server) handleStatefulSetSubPath(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	parts := strings.SplitN(sub, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "statefulset namespace and name required"})
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
			s.handleScaleStatefulSet(w, r, client, ns, name)
			return
		case "restart":
			if r.Method != http.MethodPost {
				paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			s.handleRestartStatefulSet(w, r, client, ns, name)
			return
		}
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown statefulset sub-path: " + sub})
}

func (s *Server) handleDaemonSetSubPath(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	parts := strings.SplitN(sub, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "daemonset namespace and name required"})
		return
	}
	ns, name := parts[0], parts[1]
	if len(parts) == 3 && parts[2] == "restart" {
		if r.Method != http.MethodPost {
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleRestartDaemonSet(w, r, client, ns, name)
		return
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown daemonset sub-path: " + sub})
}

func (s *Server) handleJobSubPath(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	parts := strings.SplitN(sub, "/", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job namespace and name required"})
		return
	}
	ns, name := parts[0], parts[1]
	if r.Method != http.MethodDelete {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.handleDeleteJob(w, r, client, ns, name)
}

func (s *Server) handleCronJobSubPath(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient, sub string) {
	parts := strings.SplitN(sub, "/", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "cronjob namespace and name required"})
		return
	}
	ns, name := parts[0], parts[1]
	if r.Method != http.MethodDelete {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.handleDeleteCronJob(w, r, client, ns, name)
}

var _ = appsv1.StatefulSet{}
var _ = batchv1.Job{}
var _ = corev1.Namespace{}
var _ = networkingv1.Ingress{}
var _ = autoscalingv2.HorizontalPodAutoscaler{}
var _ = rbacv1.Role{}
var _ = sort.Strings
