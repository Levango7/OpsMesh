package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MySQLSpec defines the desired state of the embedded MySQL StatefulSet.
type MySQLSpec struct {
	// Enabled controls whether the operator reconciles a MySQL StatefulSet.
	Enabled bool `json:"enabled"`

	// Storage is the requested persistent volume size, e.g. "10Gi".
	// +kubebuilder:default="10Gi"
	Storage string `json:"storage"`

	// Password is the MySQL root password. In production this should be
	// sourced from a Secret, but for the v1alpha1 scaffold we accept a
	// plain string to keep the CRD self-contained.
	Password string `json:"password,omitempty"`
}

// RedisSpec defines the desired state of the embedded Redis StatefulSet.
type RedisSpec struct {
	// Enabled controls whether the operator reconciles a Redis StatefulSet.
	Enabled bool `json:"enabled"`

	// Storage is the requested persistent volume size, e.g. "1Gi".
	// +kubebuilder:default="1Gi"
	Storage string `json:"storage"`
}

// OpsMeshInstanceSpec defines the desired state of OpsMeshInstance.
type OpsMeshInstanceSpec struct {
	// Replicas is the number of control-plane Deployment replicas.
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas"`

	// Image is the control-plane container image.
	// +kubebuilder:default="opsmesh/opsmesh:latest"
	Image string `json:"image"`

	// AgentImage is the node-agent DaemonSet container image.
	// +kubebuilder:default="opsmesh/opsmesh-agent:latest"
	AgentImage string `json:"agentImage"`

	// Store selects the backing store: "memory" or "mysql".
	// +kubebuilder:validation:Enum=memory;mysql
	// +kubebuilder:default="memory"
	Store string `json:"store"`

	// Production toggles production-grade defaults (resource limits, PDB, etc.).
	Production bool `json:"production,omitempty"`

	// TLSEnabled enables mTLS between control-plane and agents.
	TLSEnabled bool `json:"tlsEnabled,omitempty"`

	// SegmentCIDR is the overlay segment CIDR used by agents, e.g. "10.244.0.0/16".
	// +kubebuilder:default="10.244.0.0/16"
	SegmentCIDR string `json:"segmentCIDR"`

	// MySQL configures the embedded MySQL StatefulSet. Only reconciled when
	// Store=="mysql" or MySQL.Enabled==true.
	MySQL MySQLSpec `json:"mysql,omitempty"`

	// Redis configures the embedded Redis StatefulSet.
	Redis RedisSpec `json:"redis,omitempty"`
}

// OpsMeshInstanceStatus defines the observed state of OpsMeshInstance.
type OpsMeshInstanceStatus struct {
	// Conditions describe the observed reconciliation state of the resource.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// OpsMeshInstance is the Schema for the opsmeshinstances API.
//
// An OpsMeshInstance declaratively manages a complete OpsMesh deployment:
// the control-plane Deployment, the node-agent DaemonSet, optional MySQL /
// Redis StatefulSets and the headless Service that wires them together.
type OpsMeshInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OpsMeshInstanceSpec   `json:"spec,omitempty"`
	Status OpsMeshInstanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OpsMeshInstanceList contains a list of OpsMeshInstance.
type OpsMeshInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpsMeshInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpsMeshInstance{}, &OpsMeshInstanceList{})
}