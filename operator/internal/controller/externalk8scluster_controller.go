/*
Package controller implements the ExternalK8sCluster reconciliation loop.

The ExternalK8sClusterReconciler watches ExternalK8sCluster custom resources and
connects to the referenced external K8s clusters using the kubeconfig stored in
the referenced Secret. It reports connectivity state, node count and API server
URL back onto the CR status so that `kubectl describe externalk8scluster`
surfaces a human-readable rollup.
*/
package controller

import (
	"context"
	"fmt"
	"time"

	opsmeshv1alpha1 "github.com/Levango7/OpsMesh/operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// externalk8sclusterFinalizer is applied to every ExternalK8sCluster CR so
	// the reconciler gets a chance to run cleanup logic before the CR itself
	// is removed from the cluster.
	externalk8sclusterFinalizer = "opsmesh.levango.com/externalk8scluster-finalizer"

	// kubeconfigSecretKey is the key within the referenced Secret that holds
	// the kubeconfig YAML content.
	kubeconfigSecretKey = "kubeconfig"

	// defaultSyncIntervalSeconds is the default re-sync interval when the
	// spec does not specify one.
	defaultSyncIntervalSeconds = 300

	// conditionTypeConnection tracks the external cluster connection health.
	conditionTypeConnection = "ConnectionReady"
)

// ExternalK8sClusterReconciler reconciles an ExternalK8sCluster object.
type ExternalK8sClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ExternalK8sClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cluster opsmeshv1alpha1.ExternalK8sCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ExternalK8sCluster resource not found; ignoring since object was deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	conditions := newExternalClusterConditionTracker(&cluster)

	// ------------------------------------------------------------------
	// Deletion path: run the finalizer cleanup and drop it.
	// ------------------------------------------------------------------
	if !cluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &cluster, conditions)
	}

	// ------------------------------------------------------------------
	// Creation path: ensure the finalizer is present.
	// ------------------------------------------------------------------
	if !controllerutil.ContainsFinalizer(&cluster, externalk8sclusterFinalizer) {
		controllerutil.AddFinalizer(&cluster, externalk8sclusterFinalizer)
		if err := r.Update(ctx, &cluster); err != nil {
			conditions.set(conditionTypeConnection, metav1.ConditionFalse, "FinalizerAddFailed", err.Error())
			return ctrl.Result{RequeueAfter: 30 * time.Second}, conditions.flush(ctx, r, err)
		}
	}

	conditions.set(conditionTypeConnection, metav1.ConditionUnknown, "Connecting", "attempting to connect to external cluster")

	syncInterval := cluster.Spec.SyncIntervalSeconds
	if syncInterval <= 0 {
		syncInterval = defaultSyncIntervalSeconds
	}

	// Read the kubeconfig from the referenced Secret.
	kubeconfig, serverURL, err := r.loadKubeconfig(ctx, &cluster)
	if err != nil {
		logger.Error(err, "failed to load kubeconfig", "secret", cluster.Spec.KubeconfigSecretRef.Name)
		conditions.set(conditionTypeConnection, metav1.ConditionFalse, "KubeconfigLoadFailed", err.Error())
		conditions.setConnectionState(opsmeshv1alpha1.ConnectionStateError, err.Error())
		return ctrl.Result{RequeueAfter: time.Duration(syncInterval) * time.Second}, conditions.flush(ctx, r, err)
	}
	conditions.setServerURL(serverURL)

	// Create a REST config and clientset from the kubeconfig.
	cfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		logger.Error(err, "failed to parse kubeconfig into rest.Config")
		conditions.set(conditionTypeConnection, metav1.ConditionFalse, "KubeconfigParseFailed", err.Error())
		conditions.setConnectionState(opsmeshv1alpha1.ConnectionStateError, err.Error())
		return ctrl.Result{RequeueAfter: time.Duration(syncInterval) * time.Second}, conditions.flush(ctx, r, err)
	}
	cfg.Insecure = false // security: never skip TLS verification

	cs, err := KubernetesForConfig(cfg)
	if err != nil {
		logger.Error(err, "failed to create clientset from kubeconfig")
		conditions.set(conditionTypeConnection, metav1.ConditionFalse, "ClientsetCreateFailed", err.Error())
		conditions.setConnectionState(opsmeshv1alpha1.ConnectionStateError, err.Error())
		return ctrl.Result{RequeueAfter: time.Duration(syncInterval) * time.Second}, conditions.flush(ctx, r, err)
	}

	// Test connectivity by listing namespaces.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	nsList, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		logger.Error(err, "failed to list namespaces on external cluster")
		conditions.set(conditionTypeConnection, metav1.ConditionFalse, "ConnectivityTestFailed", err.Error())
		conditions.setConnectionState(opsmeshv1alpha1.ConnectionStateError, err.Error())
		return ctrl.Result{RequeueAfter: time.Duration(syncInterval) * time.Second}, conditions.flush(ctx, r, err)
	}
	_ = nsList // namespaces listed successfully

	// Discover node count.
	nodeList, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	nodeCount := int32(0)
	if err == nil {
		nodeCount = int32(len(nodeList.Items))
	}

	// Create DynamicClient and DiscoveryClient for CRD/API resource discovery.
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "failed to create dynamic client")
		// Non-fatal: continue without dynamic discovery
	} else {
		discoClient, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			logger.Error(err, "failed to create discovery client")
			// Non-fatal: continue without discovery
		} else {
			// Discover CRDs.
			crdList, err := discoClient.ServerResourcesForGroupVersion("apiextensions.k8s.io/v1")
			if err == nil && crdList != nil {
				for _, r := range crdList.APIResources {
					if r.Kind == "CustomResourceDefinition" {
						crdRes := dynClient.Resource(schema.GroupVersionResource{
							Group:    "apiextensions.k8s.io",
							Version:  "v1",
							Resource: "customresourcedefinitions",
						})
						crdListObj, err := crdRes.List(ctx, metav1.ListOptions{})
						if err == nil && crdListObj != nil {
							cluster.Status.CRDCount = int32(len(crdListObj.Items))
							for _, crd := range crdListObj.Items {
								spec, _, err := unstructured.NestedMap(crd.Object, "spec")
								if err != nil {
									continue
								}
								group, _, err := unstructured.NestedString(spec, "group")
								if err != nil {
									continue
								}
								names, _, err := unstructured.NestedMap(spec, "names")
								if err != nil {
									continue
								}
								kind, _, err := unstructured.NestedString(names, "kind")
								if err != nil {
									continue
								}
								cluster.Status.CRDNames = append(cluster.Status.CRDNames,
									fmt.Sprintf("%s/%s", group, kind))
							}
						}
					}
				}
			}

			// Discover API resources (group/version/kind summary).
			apiGroups, err := discoClient.ServerGroups()
			if err == nil {
				for _, g := range apiGroups.Groups {
					for _, v := range g.Versions {
						gv := v.GroupVersion
						resList, err := discoClient.ServerResourcesForGroupVersion(gv)
						if err == nil && resList != nil {
							for _, r := range resList.APIResources {
								// Skip subresources (contain '/')
								if !containsSlash(r.Name) {
									cluster.Status.APIResourceList = append(cluster.Status.APIResourceList,
										opsmeshv1alpha1.APIResourceSummary{
											Group:      r.Group,
											Version:    r.Version,
											Kind:       r.Kind,
											Namespaced: r.Namespaced,
										})
								}
							}
						}
					}
				}
			}
		}
	}

	// Update status.
	now := metav1.Now()
	cluster.Status.ConnectionState = opsmeshv1alpha1.ConnectionStateConnected
	cluster.Status.Server = serverURL
	cluster.Status.NodeCount = nodeCount
	cluster.Status.LastSyncTime = &now
	cluster.Status.Message = "connected"
	conditions.set(conditionTypeConnection, metav1.ConditionTrue, "Connected", "external cluster reachable")
	conditions.setConnectionState(opsmeshv1alpha1.ConnectionStateConnected, "external cluster reachable")

	logger.Info("ExternalK8sCluster reconciled successfully",
		"server", serverURL, "nodeCount", nodeCount)

	// Requeue periodically to self-heal connectivity drift.
	return ctrl.Result{RequeueAfter: time.Duration(syncInterval) * time.Second}, conditions.flush(ctx, r, nil)
}

// reconcileDelete runs the graceful teardown sequence when an
// ExternalK8sCluster is being deleted.
func (r *ExternalK8sClusterReconciler) reconcileDelete(
	ctx context.Context,
	cluster *opsmeshv1alpha1.ExternalK8sCluster,
	conditions *externalClusterConditionTracker,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("ExternalK8sCluster is being deleted; running finalizer cleanup")

	// Drop the finalizer so the API server can garbage-collect the CR.
	controllerutil.RemoveFinalizer(cluster, externalk8sclusterFinalizer)
	if err := r.Update(ctx, cluster); err != nil {
		conditions.set(conditionTypeConnection, metav1.ConditionFalse, "FinalizerRemoveFailed", err.Error())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, conditions.flush(ctx, r, err)
	}
	logger.Info("finalizer removed; ExternalK8sCluster deletion complete")

	conditions.setConnectionState(opsmeshv1alpha1.ConnectionStateDisconnected, "deleting")
	return ctrl.Result{}, conditions.flush(ctx, r, nil)
}

// loadKubeconfig reads the kubeconfig YAML content from the Secret referenced
// by the cluster spec, and returns the kubeconfig string plus the API server
// URL parsed from it.
func (r *ExternalK8sClusterReconciler) loadKubeconfig(
	ctx context.Context,
	cluster *opsmeshv1alpha1.ExternalK8sCluster,
) (string, string, error) {
	ref := cluster.Spec.KubeconfigSecretRef
	if ref.Name == "" {
		return "", "", fmt.Errorf("kubeconfigSecretRef.name is empty")
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: cluster.Namespace}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "", fmt.Errorf("secret %q not found in namespace %q", ref.Name, cluster.Namespace)
		}
		return "", "", fmt.Errorf("get secret %q: %w", ref.Name, err)
	}

	key := ref.Key
	if key == "" {
		key = kubeconfigSecretKey
	}
	kubeconfigBytes, ok := secret.Data[key]
	if !ok {
		return "", "", fmt.Errorf("secret %q does not contain key %q", ref.Name, key)
	}

	// Parse the kubeconfig to extract the API server URL.
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return "", "", fmt.Errorf("parse kubeconfig: %w", err)
	}

	return string(kubeconfigBytes), cfg.Host, nil
}

// kubernetesForConfig creates a kubernetes clientset from a rest.Config.
// It is a separate function (and variable) so tests can stub it with a fake.
// kubernetesForConfig creates a kubernetes clientset from a rest.Config.
// It is a variable (not a function) so tests can stub it with a fake clientset.
// KubernetesForConfig creates a kubernetes clientset from a rest.Config.
// It is a variable (not a function) so tests can stub it with a fake clientset.
var KubernetesForConfig = func(cfg *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(cfg)
}

// newExternalClusterConditionTracker creates a condition tracker for the
// ExternalK8sCluster.
func newExternalClusterConditionTracker(cluster *opsmeshv1alpha1.ExternalK8sCluster) *externalClusterConditionTracker {
	return &externalClusterConditionTracker{
		cluster: cluster,
		base:    cluster.DeepCopy(),
	}
}

// externalClusterConditionTracker batches status updates so we only issue a
// single Patch.
type externalClusterConditionTracker struct {
	cluster *opsmeshv1alpha1.ExternalK8sCluster
	base    *opsmeshv1alpha1.ExternalK8sCluster
	dirty   bool
}

func (c *externalClusterConditionTracker) set(t string, s metav1.ConditionStatus, reason, msg string) {
	cond := metav1.Condition{
		Type:               t,
		Status:             s,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: c.cluster.Generation,
		LastTransitionTime: metav1.Now(),
	}
	meta.SetStatusCondition(&c.cluster.Status.Conditions, cond)
	c.dirty = true
}

func (c *externalClusterConditionTracker) setConnectionState(state opsmeshv1alpha1.ConnectionState, msg string) {
	c.cluster.Status.ConnectionState = state
	if msg != "" {
		c.cluster.Status.Message = msg
	}
	c.dirty = true
}

func (c *externalClusterConditionTracker) setServerURL(serverURL string) {
	c.cluster.Status.Server = serverURL
	c.dirty = true
}

// flush patches the ExternalK8sCluster status if it changed.
func (c *externalClusterConditionTracker) flush(ctx context.Context, r statusClient, original error) error {
	if !c.dirty {
		return original
	}
	if err := r.Status().Patch(ctx, c.cluster, client.MergeFrom(c.base)); err != nil {
		if original == nil {
			return fmt.Errorf("patch ExternalK8sCluster status: %w", err)
		}
		return fmt.Errorf("%v (and patch status: %w)", original, err)
	}
	return original
}

// containsSlash reports whether the string contains a '/' character.
func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}

// SetupWithManager wires the reconciler into the controller-runtime manager.
func (r *ExternalK8sClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsmeshv1alpha1.ExternalK8sCluster{}).
		Complete(r)
}
