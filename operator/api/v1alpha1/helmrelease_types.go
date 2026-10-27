package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReleasePhase describes the phase of a Helm release.
type ReleasePhase string

const (
	// ReleasePhasePending means the release is waiting to be installed/upgraded.
	ReleasePhasePending ReleasePhase = "Pending"

	// ReleasePhaseDeployed means the release is successfully deployed.
	ReleasePhaseDeployed ReleasePhase = "Deployed"

	// ReleasePhaseFailed means the release failed to deploy.
	ReleasePhaseFailed ReleasePhase = "Failed"

	// ReleasePhasePendingUpgrade means the release is waiting to be upgraded.
	ReleasePhasePendingUpgrade ReleasePhase = "PendingUpgrade"

	// ReleasePhasePendingRollback means the release is waiting to be rolled back.
	ReleasePhasePendingRollback ReleasePhase = "PendingRollback"

	// ReleasePhaseUninstalled means the release has been uninstalled.
	ReleasePhaseUninstalled ReleasePhase = "Uninstalled"
)

// Environment represents the target environment for a Helm release.
type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentStaging     Environment = "staging"
	EnvironmentProduction  Environment = "production"
)

// HelmReleaseSpec defines the desired state of HelmRelease.
type HelmReleaseSpec struct {
	// ChartName is the name of the Helm chart.
	ChartName string `json:"chartName"`

	// ChartVersion is the version of the chart (optional, defaults to latest).
	ChartVersion string `json:"chartVersion,omitempty"`

	// RepoName is the name of the Helm repository (as registered in OpsMesh).
	RepoName string `json:"repoName"`

	// Namespace is the Kubernetes namespace to deploy to.
	Namespace string `json:"namespace"`

	// Values is the values.yaml content as a map.
	Values map[string]interface{} `json:"values,omitempty"`

	// ValuesFileRef references a ConfigMap or Secret containing values.yaml.
	ValuesFileRef *corev1.SecretKeySelector `json:"valuesFileRef,omitempty"`

	// Environment is the target environment (development/staging/production).
	Environment Environment `json:"environment"`

	// SyncIntervalSeconds controls how often the operator re-checks the release.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=60
	SyncIntervalSeconds int32 `json:"syncIntervalSeconds,omitempty"`

	// AutoUpgrade enables automatic upgrade when a new chart version is available.
	AutoUpgrade bool `json:"autoUpgrade,omitempty"`

	// ForceUpgrade forces upgrade even if values haven't changed.
	ForceUpgrade bool `json:"forceUpgrade,omitempty"`

	// WaitForJobs waits for Jobs to complete before marking release as deployed.
	WaitForJobs bool `json:"waitForJobs,omitempty"`

	// TimeoutSeconds is the timeout for install/upgrade operations.
	// +kubebuilder:default=300
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// HelmReleaseStatus defines the observed state of HelmRelease.
type HelmReleaseStatus struct {
	// Phase is the current phase of the release.
	Phase ReleasePhase `json:"phase,omitempty"`

	// DeployedVersion is the currently deployed chart version.
	DeployedVersion string `json:"deployedVersion,omitempty"`

	// Revision is the revision number of the release.
	Revision int32 `json:"revision,omitempty"`

	// LastDeployedTime is the timestamp of the last successful deploy.
	LastDeployedTime *metav1.Time `json:"lastDeployedTime,omitempty"`

	// Message carries a human-readable detail about the current state.
	Message string `json:"message,omitempty"`

	// LastUpgradeCheck is the timestamp of the last upgrade check.
	LastUpgradeCheck *metav1.Time `json:"lastUpgradeCheck,omitempty"`

	// NextAvailableVersion is the next available chart version (if autoUpgrade enabled).
	NextAvailableVersion string `json:"nextAvailableVersion,omitempty"`

	// Conditions describe the observed reconciliation state of the resource.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// HelmRelease is the Schema for the helmreleases API.
//
// A HelmRelease declaratively manages a Helm chart release on an external
// Kubernetes cluster. The operator watches these resources, connects to the
// target cluster, and manages the Helm release lifecycle (install/upgrade/rollback).
type HelmRelease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmReleaseSpec   `json:"spec,omitempty"`
	Status HelmReleaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HelmReleaseList contains a list of HelmRelease.
type HelmReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmRelease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HelmRelease{}, &HelmReleaseList{})
}

// SetCondition merges the supplied metav1.Condition into Status.Conditions.
func (s *HelmReleaseStatus) SetCondition(condition metav1.Condition) {
	if condition.LastTransitionTime.IsZero() {
		condition.LastTransitionTime = metav1.NewTime(now().Time)
	}
	meta.SetStatusCondition(&s.Conditions, condition)
}

// GetCondition returns the condition of the given type if present.
func (s *HelmReleaseStatus) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(s.Conditions, conditionType)
}

// RemoveCondition drops the condition of the given type.
func (s *HelmReleaseStatus) RemoveCondition(conditionType string) {
	meta.RemoveStatusCondition(&s.Conditions, conditionType)
}
