// Package controlplane k8s_resources_ext.go 实现 K8s 扩展资源与控制器管理
// 覆盖：NetworkPolicy、IngressClass、StorageClass、PDB、ServiceAccount、EndpointSlice、
// ResourceQuota、LimitRange、PriorityClass、RuntimeClass、Lease、Event、
// MutatingWebhookConfiguration、ValidatingWebhookConfiguration、CSR、VPA
package controlplane

import (
	"context"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/k8s"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ============================================================================
// NetworkPolicy (networking.k8s.io/v1)
// ============================================================================

func (s *Server) handleListNetworkPolicies(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listNetworkPolicies", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, np := range list.Items {
		types := make([]string, 0)
		for _, t := range np.Spec.PolicyTypes {
			types = append(types, string(t))
		}
		out = append(out, map[string]interface{}{
			"name":        np.Name,
			"namespace":   np.Namespace,
			"podSelector": len(np.Spec.PodSelector.MatchLabels),
			"policyTypes": types,
			"age":         formatAge(np.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"networkpolicies": out})
}

// ============================================================================
// IngressClass (networking.k8s.io/v1)
// ============================================================================

func (s *Server) handleListIngressClasses(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listIngressClasses", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, ic := range list.Items {
		controller := ""
		if ic.Spec.Controller != "" {
			controller = ic.Spec.Controller
		}
		isDefault := false
		if ic.Annotations["ingressclass.kubernetes.io/is-default-class"] == "true" {
			isDefault = true
		}
		out = append(out, map[string]interface{}{
			"name":       ic.Name,
			"controller": controller,
			"isDefault":  isDefault,
			"age":        formatAge(ic.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"ingressclasses": out})
}

// ============================================================================
// StorageClass (storage.k8s.io/v1)
// ============================================================================

func (s *Server) handleListStorageClasses(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listStorageClasses", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, sc := range list.Items {
		provisioner := sc.Provisioner
		reclaimPolicy := ""
		if sc.ReclaimPolicy != nil {
			reclaimPolicy = string(*sc.ReclaimPolicy)
		}
		volumeBindingMode := ""
		if sc.VolumeBindingMode != nil {
			volumeBindingMode = string(*sc.VolumeBindingMode)
		}
		allowExpansion := false
		if sc.AllowVolumeExpansion != nil {
			allowExpansion = *sc.AllowVolumeExpansion
		}
		isDefault := false
		if sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			isDefault = true
		}
		out = append(out, map[string]interface{}{
			"name":              sc.Name,
			"provisioner":       provisioner,
			"reclaimPolicy":     reclaimPolicy,
			"volumeBindingMode": volumeBindingMode,
			"allowExpansion":    allowExpansion,
			"isDefault":         isDefault,
			"age":               formatAge(sc.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"storageclasses": out})
}

// ============================================================================
// PodDisruptionBudget (policy/v1)
// ============================================================================

func (s *Server) handleListPDBs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.PolicyV1().PodDisruptionBudgets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listPDBs", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, pdb := range list.Items {
		minAvailable := ""
		if pdb.Spec.MinAvailable != nil {
			minAvailable = pdb.Spec.MinAvailable.String()
		}
		maxUnavailable := ""
		if pdb.Spec.MaxUnavailable != nil {
			maxUnavailable = pdb.Spec.MaxUnavailable.String()
		}
		out = append(out, map[string]interface{}{
			"name":           pdb.Name,
			"namespace":      pdb.Namespace,
			"minAvailable":   minAvailable,
			"maxUnavailable": maxUnavailable,
			"currentHealthy": pdb.Status.CurrentHealthy,
			"desiredHealthy": pdb.Status.DesiredHealthy,
			"disruptionsAllowed": pdb.Status.DisruptionsAllowed,
			"age":            formatAge(pdb.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"pdbs": out})
}

// ============================================================================
// ServiceAccount (core/v1)
// ============================================================================

func (s *Server) handleListServiceAccounts(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().ServiceAccounts(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listServiceAccounts", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, sa := range list.Items {
		out = append(out, map[string]interface{}{
			"name":      sa.Name,
			"namespace": sa.Namespace,
			"secrets":   len(sa.Secrets),
			"age":       formatAge(sa.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"serviceaccounts": out})
}

// ============================================================================
// EndpointSlice (discovery.k8s.io/v1)
// ============================================================================

func (s *Server) handleListEndpointSlices(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listEndpointSlices", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, es := range list.Items {
		addressType := string(es.AddressType)
		ready := 0
		for _, ep := range es.Endpoints {
			if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
				ready++
			}
		}
		out = append(out, map[string]interface{}{
			"name":        es.Name,
			"namespace":   es.Namespace,
			"service":     es.Labels["kubernetes.io/service-name"],
			"addressType": addressType,
			"endpoints":   len(es.Endpoints),
			"ready":       ready,
			"age":         formatAge(es.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"endpointslices": out})
}

// ============================================================================
// ResourceQuota (core/v1)
// ============================================================================

func (s *Server) handleListResourceQuotas(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().ResourceQuotas(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listResourceQuotas", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, rq := range list.Items {
		hard := make(map[string]string)
		for k, v := range rq.Status.Hard {
			hard[string(k)] = v.String()
		}
		used := make(map[string]string)
		for k, v := range rq.Status.Used {
			used[string(k)] = v.String()
		}
		out = append(out, map[string]interface{}{
			"name":      rq.Name,
			"namespace": rq.Namespace,
			"hard":      hard,
			"used":      used,
			"age":       formatAge(rq.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"resourcequotas": out})
}

// ============================================================================
// LimitRange (core/v1)
// ============================================================================

func (s *Server) handleListLimitRanges(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().LimitRanges(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listLimitRanges", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, lr := range list.Items {
		out = append(out, map[string]interface{}{
			"name":      lr.Name,
			"namespace": lr.Namespace,
			"limits":    len(lr.Spec.Limits),
			"age":       formatAge(lr.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"limitranges": out})
}

// ============================================================================
// PriorityClass (scheduling.k8s.io/v1)
// ============================================================================

func (s *Server) handleListPriorityClasses(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.SchedulingV1().PriorityClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listPriorityClasses", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, pc := range list.Items {
		globalDefault := false
		if pc.GlobalDefault {
			globalDefault = true
		}
		out = append(out, map[string]interface{}{
			"name":          pc.Name,
			"value":         pc.Value,
			"globalDefault": globalDefault,
			"preemptionPolicy": func() string {
				if pc.PreemptionPolicy != nil {
					return string(*pc.PreemptionPolicy)
				}
				return ""
			}(),
			"age": formatAge(pc.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"priorityclasses": out})
}

// ============================================================================
// RuntimeClass (node.k8s.io/v1)
// ============================================================================

func (s *Server) handleListRuntimeClasses(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.NodeV1().RuntimeClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listRuntimeClasses", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, rc := range list.Items {
		out = append(out, map[string]interface{}{
			"name":      rc.Name,
			"handler":   rc.Handler,
			"age":       formatAge(rc.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"runtimeclasses": out})
}

// ============================================================================
// Lease (coordination.k8s.io/v1)
// ============================================================================

func (s *Server) handleListLeases(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		ns = "kube-node-lease"
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoordinationV1().Leases(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listLeases", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, lease := range list.Items {
		holder := ""
		if lease.Spec.HolderIdentity != nil {
			holder = *lease.Spec.HolderIdentity
		}
		renewTime := ""
		if lease.Spec.RenewTime != nil {
			renewTime = lease.Spec.RenewTime.Time.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, map[string]interface{}{
			"name":        lease.Name,
			"namespace":   lease.Namespace,
			"holder":      holder,
			"renewTime":   renewTime,
			"age":         formatAge(lease.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"leases": out})
}

// ============================================================================
// Event (core/v1)
// ============================================================================

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listEvents", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, ev := range list.Items {
		out = append(out, map[string]interface{}{
			"name":      ev.Name,
			"namespace": ev.Namespace,
			"type":      ev.Type,
			"reason":    ev.Reason,
			"message":   ev.Message,
			"object":    ev.InvolvedObject.Kind + "/" + ev.InvolvedObject.Name,
			"count":     ev.Count,
			"firstSeen": ev.FirstTimestamp.Time,
			"lastSeen":  ev.LastTimestamp.Time,
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"events": out})
}

// ============================================================================
// MutatingWebhookConfiguration (admissionregistration.k8s.io/v1)
// ============================================================================

func (s *Server) handleListMutatingWebhooks(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listMutatingWebhooks", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, mwc := range list.Items {
		out = append(out, map[string]interface{}{
			"name":     mwc.Name,
			"webhooks": len(mwc.Webhooks),
			"age":      formatAge(mwc.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"mutatingwebhooks": out})
}

// ============================================================================
// ValidatingWebhookConfiguration (admissionregistration.k8s.io/v1)
// ============================================================================

func (s *Server) handleListValidatingWebhooks(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listValidatingWebhooks", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, vwc := range list.Items {
		out = append(out, map[string]interface{}{
			"name":     vwc.Name,
			"webhooks": len(vwc.Webhooks),
			"age":      formatAge(vwc.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"validatingwebhooks": out})
}

// ============================================================================
// CertificateSigningRequest (certificates.k8s.io/v1)
// ============================================================================

func (s *Server) handleListCSRs(w http.ResponseWriter, r *http.Request, client *k8s.K8sClient) {
	if _, ok := s.requirePermission(w, r, "k8s:read"); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), k8sAPITimeout)
	defer cancel()
	list, err := client.Clientset.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.listCSRs", err)
		return
	}
	out := make([]map[string]interface{}, 0, len(list.Items))
	for _, csr := range list.Items {
		condition := ""
		if len(csr.Status.Conditions) > 0 {
			condition = string(csr.Status.Conditions[0].Type)
		}
		out = append(out, map[string]interface{}{
			"name":      csr.Name,
			"signer":    csr.Spec.SignerName,
			"condition": condition,
			"age":       formatAge(csr.CreationTimestamp.Time),
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"csrs": out})
}

// ============================================================================
// 路由注册 (追加到 k8s_manage.go 的 switch)
// ============================================================================

// registerK8sExtResourceRoutes 注册 K8s 扩展资源路由 (在 k8s_manage.go 的 switch 中调用)
func (s *Server) dispatchK8sExtResource(w http.ResponseWriter, r *http.Request, resource string, client *k8s.K8sClient) bool {
	switch resource {
	case "networkpolicies":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListNetworkPolicies(w, r, client)
		return true
	case "ingressclasses":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListIngressClasses(w, r, client)
		return true
	case "storageclasses":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListStorageClasses(w, r, client)
		return true
	case "pdbs":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListPDBs(w, r, client)
		return true
	case "serviceaccounts":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListServiceAccounts(w, r, client)
		return true
	case "endpointslices":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListEndpointSlices(w, r, client)
		return true
	case "resourcequotas":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListResourceQuotas(w, r, client)
		return true
	case "limitranges":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListLimitRanges(w, r, client)
		return true
	case "priorityclasses":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListPriorityClasses(w, r, client)
		return true
	case "runtimeclasses":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListRuntimeClasses(w, r, client)
		return true
	case "leases":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListLeases(w, r, client)
		return true
	case "events":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListEvents(w, r, client)
		return true
	case "mutatingwebhooks":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListMutatingWebhooks(w, r, client)
		return true
	case "validatingwebhooks":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListValidatingWebhooks(w, r, client)
		return true
	case "csrs":
		if r.Method != http.MethodGet {
			return false
		}
		s.handleListCSRs(w, r, client)
		return true
	default:
		return false
	}
}
