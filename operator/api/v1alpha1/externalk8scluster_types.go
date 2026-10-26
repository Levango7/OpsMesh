package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConnectionState describes the current connectivity state of an external
// K8s cluster as observed by the operator reconciler.
type ConnectionState string

const (
	// ConnectionStateConnected means the operator successfully reached the
	// external cluster's API server during the last reconcile.
	ConnectionStateConnected ConnectionState = "Connected"

	// ConnectionStateDisconnected means the operator could not reach the
	// external cluster (network unreachable, refused, timeout).
	ConnectionStateDisconnected ConnectionState = "Disconnected"

	// ConnectionStateError means the kubeconfig is invalid or the cluster
	// returned an auth/permission error.
	ConnectionStateError ConnectionState = "Error"
)

// ExternalK8sClusterSpec defines the desired state of ExternalK8sCluster.
type ExternalK8sClusterSpec struct {
	// ClusterName is a human-readable name for the external cluster.
	// +kubebuilder:default=my-external-cluster
	ClusterName string `json:"clusterName"`

	// KubeconfigSecretRef references a Kubernetes Secret containing the
	// kubeconfig YAML under the key "kubeconfig". The Secret must live in
	// the same namespace as the ExternalK8sCluster. Required.
	KubeconfigSecretRef corev1.SecretKeySelector `json:"kubeconfigSecretRef"`

	// Description is an optional free-text description of the cluster.
	Description string `json:"description,omitempty"`

	// SyncIntervalSeconds controls how often the operator re-checks the
	// external cluster's connectivity and resource counts.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=30
	SyncIntervalSeconds int32 `json:"syncIntervalSeconds,omitempty"`
}

// ExternalK8sClusterStatus defines the observed state of ExternalK8sCluster.
type ExternalK8sClusterStatus struct {
	// ConnectionState is the last observed connectivity state.
	ConnectionState ConnectionState `json:"connectionState,omitempty"`

	// Server is the external cluster's API server URL (parsed from kubeconfig).
	Server string `json:"server,omitempty"`

	// NodeCount is the number of nodes in the external cluster.
	NodeCount int32 `json:"nodeCount,omitempty"`

	// CRDCount is the number of CustomResourceDefinitions discovered in the external cluster.
	CRDCount int32 `json:"crdCount,omitempty"`

	// CRDNames is a list of discovered CRD names (group/kind).
	CRDNames []string `json:"crdNames,omitempty"`

	// APIResourceList is a summary of discovered API resources (group/version/kind).
	APIResourceList []APIResourceSummary `json:"apiResourceList,omitempty"`

	// LastSyncTime is the timestamp of the last successful sync.
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Message carries a human-readable detail about the current state
	// (e.g. error reason when ConnectionState==Error).
	Message string `json:"message,omitempty"`

	// Conditions describe the observed reconciliation state of the resource.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// APIResourceSummary summarizes a discovered API resource.
type APIResourceSummary struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
	Namespaced bool `json:"namespaced"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ExternalK8sCluster is the Schema for the externalk8sclusters API.
//
// An ExternalK8sCluster declaratively registers an external Kubernetes cluster
// with OpsMesh. The operator watches these resources, connects to the external
// cluster using the kubeconfig in the referenced Secret, and reports
// connectivity and basic resource counts back onto the CR status.
type ExternalK8sCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExternalK8sClusterSpec   `json:"spec,omitempty"`
	Status ExternalK8sClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExternalK8sClusterList contains a list of ExternalK8sCluster.
type ExternalK8sClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalK8sCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ExternalK8sCluster{}, &ExternalK8sClusterList{})
}

// SetCondition merges the supplied metav1.Condition into Status.Conditions.
func (s *ExternalK8sClusterStatus) SetCondition(condition metav1.Condition) {
	if condition.LastTransitionTime.IsZero() {
		condition.LastTransitionTime = metav1.NewTime(now().Time)
	}
	meta.SetStatusCondition(&s.Conditions, condition)
}

// GetCondition returns the condition of the given type if present.
func (s *ExternalK8sClusterStatus) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(s.Conditions, conditionType)
}

// RemoveCondition drops the condition of the given type.
func (s *ExternalK8sClusterStatus) RemoveCondition(conditionType string) {
	meta.RemoveStatusCondition(&s.Conditions, conditionType)
}

// now returns the current time. It is a variable so tests can stub it.
var now = metav1.Now
