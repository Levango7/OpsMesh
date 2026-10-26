/*
Package controller_test exercises the ExternalK8sCluster reconciler against an
in-memory fake client.
*/
package controller_test

import (
	"context"
	"testing"
	"time"

	opsmeshv1alpha1 "github.com/Levango7/OpsMesh/operator/api/v1alpha1"
	"github.com/Levango7/OpsMesh/operator/internal/controller"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/kubernetes"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestExternalK8sClusterReconcile_FinalizerAdded(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opsmeshv1alpha1.AddToScheme(scheme))

	instance := &opsmeshv1alpha1.ExternalK8sCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Generation: 1,
		},
		Spec: opsmeshv1alpha1.ExternalK8sClusterSpec{
			ClusterName: "test-cluster",
			KubeconfigSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "test-kubeconfig"},
				Key:                   "kubeconfig",
			},
		},
	}

	// Create the referenced Secret with a dummy kubeconfig.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kubeconfig",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": []byte("apiVersion: v1\nkind: Config\nclusters:\n- cluster:\n    server: https://127.0.0.1:6443\n  name: test\ncontexts:\n- context:\n    cluster: test\n    user: test\n  name: test\ncurrent-context: test\nusers:\n- name: test\n  user:\n    token: dummy\n"),
		},
	}

	cl := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, secret).
		WithStatusSubresource(&opsmeshv1alpha1.ExternalK8sCluster{}).
		Build()

	r := &controller.ExternalK8sClusterReconciler{Client: cl, Scheme: scheme}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First reconcile should add the finalizer.
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "test-cluster", Namespace: "default"}}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	// Verify finalizer was added.
	var got opsmeshv1alpha1.ExternalK8sCluster
	if err := cl.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: "default"}, &got); err != nil {
		t.Fatalf("get instance for finalizer check: %v", err)
	}

	hasFinalizer := false
	for _, f := range got.Finalizers {
		if f == "opsmesh.levango.com/externalk8scluster-finalizer" {
			hasFinalizer = true
			break
		}
	}
	if !hasFinalizer {
		t.Fatal("expected finalizer to be added")
	}
}

func TestExternalK8sClusterReconcile_DeleteRemovesFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opsmeshv1alpha1.AddToScheme(scheme))

	instance := &opsmeshv1alpha1.ExternalK8sCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-cluster-delete",
			Namespace:         "default",
			Generation:        1,
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{"opsmesh.levango.com/externalk8scluster-finalizer"},
		},
		Spec: opsmeshv1alpha1.ExternalK8sClusterSpec{
			ClusterName: "test-cluster-delete",
			KubeconfigSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "test-kubeconfig"},
				Key:                   "kubeconfig",
			},
		},
	}

	// Create the referenced Secret with a dummy kubeconfig.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kubeconfig",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": []byte("apiVersion: v1\nkind: Config\nclusters:\n- cluster:\n    server: https://127.0.0.1:6443\n  name: test\ncontexts:\n- context:\n    cluster: test\n    user: test\n  name: test\ncurrent-context: test\nusers:\n- name: test\n  user:\n    token: dummy\n"),
		},
	}

	cl := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, secret).
		WithStatusSubresource(&opsmeshv1alpha1.ExternalK8sCluster{}).
		Build()

	r := &controller.ExternalK8sClusterReconciler{Client: cl, Scheme: scheme}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Reconcile should remove the finalizer and not requeue.
	result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "test-cluster-delete", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile during delete: %v", err)
	}

	if result.Requeue || result.RequeueAfter > 0 {
		t.Fatal("expected no requeue after finalizer removal")
	}

	// Verify finalizer was removed.
	var got opsmeshv1alpha1.ExternalK8sCluster
	if err := cl.Get(ctx, types.NamespacedName{Name: "test-cluster-delete", Namespace: "default"}, &got); err != nil {
		if apierrors.IsNotFound(err) {
			// Object might be gone entirely after finalizer removal
			return
		}
		t.Fatalf("get instance after delete: %v", err)
	}

	for _, f := range got.Finalizers {
		if f == "opsmesh.levango.com/externalk8scluster-finalizer" {
			t.Fatal("expected finalizer to be removed")
		}
	}
}

func TestExternalK8sClusterReconcile_SuccessPath(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opsmeshv1alpha1.AddToScheme(scheme))

	instance := &opsmeshv1alpha1.ExternalK8sCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-success",
			Namespace: "default",
			Generation: 1,
		},
		Spec: opsmeshv1alpha1.ExternalK8sClusterSpec{
			ClusterName: "test-cluster-success",
			KubeconfigSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "test-kubeconfig"},
				Key:                   "kubeconfig",
			},
		},
	}

	// Create the referenced Secret with a dummy kubeconfig that has a valid server.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kubeconfig",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": []byte("apiVersion: v1\nkind: Config\nclusters:\n- cluster:\n    server: https://127.0.0.1:6443\n  name: test\ncontexts:\n- context:\n    cluster: test\n    user: test\n  name: test\ncurrent-context: test\nusers:\n- name: test\n  user:\n    token: dummy\n"),
		},
	}

	cl := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, secret).
		WithStatusSubresource(&opsmeshv1alpha1.ExternalK8sCluster{}).
		Build()

	// Save original KubernetesForConfig and restore after test.
	originalKubernetesForConfig := controller.KubernetesForConfig
	defer func() { controller.KubernetesForConfig = originalKubernetesForConfig }()

	// Stub KubernetesForConfig to return a fake clientset that succeeds.
	fakeClientset := k8sfake.NewSimpleClientset()
	controller.KubernetesForConfig = func(cfg *rest.Config) (kubernetes.Interface, error) {
		return fakeClientset, nil
	}

	instance.Spec.KubeconfigSecretRef = corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "test-kubeconfig"},
		Key:                   "kubeconfig",
	}

	cl = fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, secret).
		WithStatusSubresource(&opsmeshv1alpha1.ExternalK8sCluster{}).
		Build()

	r := &controller.ExternalK8sClusterReconciler{Client: cl, Scheme: scheme}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First reconcile should succeed and update status.
	result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "test-cluster-success", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue after successful reconcile")
	}

	// Verify status was updated.
	var got opsmeshv1alpha1.ExternalK8sCluster
	if err := cl.Get(ctx, types.NamespacedName{Name: "test-cluster-success", Namespace: "default"}, &got); err != nil {
		t.Fatalf("get instance for status check: %v", err)
	}

	if got.Status.ConnectionState != opsmeshv1alpha1.ConnectionStateConnected {
		t.Fatalf("expected ConnectionState=Connected, got %s", got.Status.ConnectionState)
	}
	if got.Status.Server == "" {
		t.Fatal("expected server URL to be populated")
	}
	if got.Status.NodeCount == 0 {
		t.Fatal("expected nodeCount > 0")
	}
	readyCond := metaFindStatusCondition(got.Status.Conditions, "ConnectionReady")
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected ConnectionReady=True condition, got %+v", got.Status.Conditions)
	}
}