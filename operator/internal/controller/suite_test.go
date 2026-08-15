/*
Package controller_test exercises the OpsMeshInstance reconciler against an
in-memory fake client. We deliberately avoid controller-runtime's envtest here
because envtest requires a downloaded kube-apiserver/etcd binary pair which is
not available in the build sandbox. The fake-client suite still gives us
meaningful coverage of the create/update/own semantics of every managed
resource (Deployment, DaemonSet, StatefulSet, Service).
*/
package controller_test

import (
	"context"
	"testing"
	"time"

	opsmeshv1alpha1 "opsmesh/operator/api/v1alpha1"
	"opsmesh/operator/internal/controller"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestOpsMeshInstanceReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opsmeshv1alpha1.AddToScheme(scheme))

	instance := &opsmeshv1alpha1.OpsMeshInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "demo",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: opsmeshv1alpha1.OpsMeshInstanceSpec{
			Replicas:    3,
			Image:       "opsmesh/opsmesh:v0.1.0",
			AgentImage:  "opsmesh/opsmesh-agent:v0.1.0",
			Store:       "mysql",
			Production:  true,
			TLSEnabled:  true,
			SegmentCIDR: "10.244.0.0/16",
			MySQL:       opsmeshv1alpha1.MySQLSpec{Enabled: true, Storage: "5Gi", Password: "s3cret"},
			Redis:       opsmeshv1alpha1.RedisSpec{Enabled: true, Storage: "1Gi"},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance).
		WithStatusSubresource(&opsmeshv1alpha1.OpsMeshInstance{}).
		Build()

	r := &controller.OpsMeshInstanceReconciler{Client: cl, Scheme: scheme}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First reconcile should create all managed resources.
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "demo", Namespace: "default"}}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	assertExists(t, ctx, cl, &appsv1.Deployment{}, "demo-control-plane")
	assertExists(t, ctx, cl, &appsv1.DaemonSet{}, "demo-agent")
	assertExists(t, ctx, cl, &appsv1.StatefulSet{}, "demo-mysql")
	assertExists(t, ctx, cl, &appsv1.StatefulSet{}, "demo-redis")
	assertExists(t, ctx, cl, &corev1.Service{}, "demo-control-plane")

	// Second reconcile should be idempotent (no error, resources still present).
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "demo", Namespace: "default"}}); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}

	// Status should now carry a Ready=True condition.
	var got opsmeshv1alpha1.OpsMeshInstance
	if err := cl.Get(ctx, types.NamespacedName{Name: "demo", Namespace: "default"}, &got); err != nil {
		t.Fatalf("get instance for status check: %v", err)
	}
	ready := metaFindStatusCondition(got.Status.Conditions, "Ready")
	if ready == nil {
		t.Fatalf("expected Ready condition, got %+v", got.Status)
	}
	if ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %s: %s", ready.Status, ready.Message)
	}
}

func TestOpsMeshInstanceReconcile_MemoryStore(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opsmeshv1alpha1.AddToScheme(scheme))

	instance := &opsmeshv1alpha1.OpsMeshInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "lite", Namespace: "default", Generation: 1},
		Spec: opsmeshv1alpha1.OpsMeshInstanceSpec{
			Replicas:    1,
			Image:       "opsmesh/opsmesh:latest",
			AgentImage:  "opsmesh/opsmesh-agent:latest",
			Store:       "memory",
			SegmentCIDR: "10.244.0.0/16",
			MySQL:       opsmeshv1alpha1.MySQLSpec{Enabled: false},
			Redis:       opsmeshv1alpha1.RedisSpec{Enabled: false},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance).
		WithStatusSubresource(&opsmeshv1alpha1.OpsMeshInstance{}).
		Build()

	r := &controller.OpsMeshInstanceReconciler{Client: cl, Scheme: scheme}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "lite", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	assertExists(t, ctx, cl, &appsv1.Deployment{}, "lite-control-plane")
	assertExists(t, ctx, cl, &appsv1.DaemonSet{}, "lite-agent")
	assertExists(t, ctx, cl, &corev1.Service{}, "lite-control-plane")

	// MySQL/Redis StatefulSets must NOT exist when disabled.
	assertAbsent(t, ctx, cl, &appsv1.StatefulSet{}, "lite-mysql")
	assertAbsent(t, ctx, cl, &appsv1.StatefulSet{}, "lite-redis")
}

// assertExists fetches obj by name in default namespace and fails the test if absent.
func assertExists(t *testing.T, ctx context.Context, cl client.Client, obj client.Object, name string) {
	t.Helper()
	key := types.NamespacedName{Name: name, Namespace: "default"}
	if err := cl.Get(ctx, key, obj); err != nil {
		t.Fatalf("expected %s to exist, got error: %v", name, err)
	}
}

// assertAbsent fetches obj by name and fails the test if it unexpectedly exists.
func assertAbsent(t *testing.T, ctx context.Context, cl client.Client, obj client.Object, name string) {
	t.Helper()
	key := types.NamespacedName{Name: name, Namespace: "default"}
	err := cl.Get(ctx, key, obj)
	if err == nil {
		t.Fatalf("expected %s to be absent but it exists", name)
	}
}

// metaFindStatusCondition mirrors metav1.FindStatusCondition so the test file
// stays self-contained and easy to read.
func metaFindStatusCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}
