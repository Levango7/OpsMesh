/*
Package controller implements the OpsMeshInstance reconciliation loop.

The controller watches OpsMeshInstance custom resources and declaratively
drives a complete OpsMesh deployment: a control-plane Deployment, a node-agent
DaemonSet, optional MySQL / Redis StatefulSets and the headless Service that
fronts the control plane. Status conditions are mirrored back onto the CR so
that `kubectl describe opsmeshinstance` surfaces a human-readable rollup.
*/
package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	opsmeshv1alpha1 "opsmesh/operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// conditionTypeReady is the rollup condition reported on the CR status.
	conditionTypeReady = "Ready"

	// conditionTypeControlPlane tracks the control-plane Deployment health.
	conditionTypeControlPlane = "ControlPlaneReady"

	// conditionTypeAgent tracks the agent DaemonSet health.
	conditionTypeAgent = "AgentReady"

	// conditionTypeMySQL tracks the MySQL StatefulSet health.
	conditionTypeMySQL = "MySQLReady"

	// conditionTypeRedis tracks the Redis StatefulSet health.
	conditionTypeRedis = "RedisReady"

	// conditionTypeService tracks the headless Service health.
	conditionTypeService = "ServiceReady"
)

// OpsMeshInstanceReconciler reconciles an OpsMeshInstance object.
type OpsMeshInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.0/pkg/reconcile
func (r *OpsMeshInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the OpsMeshInstance CR.
	var instance opsmeshv1alpha1.OpsMeshInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("OpsMeshInstance resource not found; ignoring since object was deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Track per-resource conditions; we always patch the status at the end.
	conditions := newConditionTracker(&instance)

	// 1. Reconcile the headless Service fronting the control plane.
	if err := r.reconcileService(ctx, &instance); err != nil {
		conditions.set(conditionTypeService, metav1.ConditionFalse, "ReconcileError", err.Error())
		conditions.set(conditionTypeReady, metav1.ConditionFalse, "ServiceFailed", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, conditions.flush(ctx, r, err)
	}
	conditions.set(conditionTypeService, metav1.ConditionTrue, "ServiceReconciled", "headless Service in sync")

	// 2. Reconcile the control-plane Deployment.
	if err := r.reconcileDeployment(ctx, &instance); err != nil {
		conditions.set(conditionTypeControlPlane, metav1.ConditionFalse, "ReconcileError", err.Error())
		conditions.set(conditionTypeReady, metav1.ConditionFalse, "ControlPlaneFailed", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, conditions.flush(ctx, r, err)
	}
	conditions.set(conditionTypeControlPlane, metav1.ConditionTrue, "DeploymentReconciled", "control-plane Deployment in sync")

	// 3. Reconcile the node-agent DaemonSet.
	if err := r.reconcileDaemonSet(ctx, &instance); err != nil {
		conditions.set(conditionTypeAgent, metav1.ConditionFalse, "ReconcileError", err.Error())
		conditions.set(conditionTypeReady, metav1.ConditionFalse, "AgentFailed", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, conditions.flush(ctx, r, err)
	}
	conditions.set(conditionTypeAgent, metav1.ConditionTrue, "DaemonSetReconciled", "agent DaemonSet in sync")

	// 4. Reconcile MySQL StatefulSet when enabled.
	if instance.Spec.MySQL.Enabled || instance.Spec.Store == "mysql" {
		if err := r.reconcileMySQLStatefulSet(ctx, &instance); err != nil {
			conditions.set(conditionTypeMySQL, metav1.ConditionFalse, "ReconcileError", err.Error())
			conditions.set(conditionTypeReady, metav1.ConditionFalse, "MySQLFailed", err.Error())
			return ctrl.Result{RequeueAfter: 30 * time.Second}, conditions.flush(ctx, r, err)
		}
		conditions.set(conditionTypeMySQL, metav1.ConditionTrue, "StatefulSetReconciled", "MySQL StatefulSet in sync")
	} else {
		conditions.remove(conditionTypeMySQL)
	}

	// 5. Reconcile Redis StatefulSet when enabled.
	if instance.Spec.Redis.Enabled {
		if err := r.reconcileRedisStatefulSet(ctx, &instance); err != nil {
			conditions.set(conditionTypeRedis, metav1.ConditionFalse, "ReconcileError", err.Error())
			conditions.set(conditionTypeReady, metav1.ConditionFalse, "RedisFailed", err.Error())
			return ctrl.Result{RequeueAfter: 30 * time.Second}, conditions.flush(ctx, r, err)
		}
		conditions.set(conditionTypeRedis, metav1.ConditionTrue, "StatefulSetReconciled", "Redis StatefulSet in sync")
	} else {
		conditions.remove(conditionTypeRedis)
	}

	// 6. All resources reconciled -> mark Ready.
	conditions.set(conditionTypeReady, metav1.ConditionTrue, "AllResourcesReconciled", "all managed resources in sync")

	// Requeue periodically so we self-heal drift even without watch events.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, conditions.flush(ctx, r, nil)
}

// reconcileService creates or updates the headless Service for instance.
func (r *OpsMeshInstanceReconciler) reconcileService(ctx context.Context, instance *opsmeshv1alpha1.OpsMeshInstance) error {
	desired := headlessService(instance)
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference on Service: %w", err)
	}

	var existing corev1.Service
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	if err := r.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get Service: %w", err)
		}
		return r.Create(ctx, desired)
	}

	// Preserve immutable fields, then patch the mutable Spec.
	desired.ResourceVersion = existing.ResourceVersion
	desired.Spec.ClusterIP = existing.Spec.ClusterIP
	desired.Spec.ClusterIPs = existing.Spec.ClusterIPs
	if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		return r.Update(ctx, desired)
	}
	return nil
}

// reconcileDeployment creates or updates the control-plane Deployment.
func (r *OpsMeshInstanceReconciler) reconcileDeployment(ctx context.Context, instance *opsmeshv1alpha1.OpsMeshInstance) error {
	desired := controlPlaneDeployment(instance)
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference on Deployment: %w", err)
	}

	var existing appsv1.Deployment
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	if err := r.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get Deployment: %w", err)
		}
		return r.Create(ctx, desired)
	}

	if !deploymentSpecEqual(existing.Spec, desired.Spec) {
		desired.ResourceVersion = existing.ResourceVersion
		return r.Update(ctx, desired)
	}
	return nil
}

// reconcileDaemonSet creates or updates the agent DaemonSet.
func (r *OpsMeshInstanceReconciler) reconcileDaemonSet(ctx context.Context, instance *opsmeshv1alpha1.OpsMeshInstance) error {
	desired := agentDaemonSet(instance)
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference on DaemonSet: %w", err)
	}

	var existing appsv1.DaemonSet
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	if err := r.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get DaemonSet: %w", err)
		}
		return r.Create(ctx, desired)
	}

	if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		desired.ResourceVersion = existing.ResourceVersion
		return r.Update(ctx, desired)
	}
	return nil
}

// reconcileMySQLStatefulSet creates or updates the MySQL StatefulSet.
func (r *OpsMeshInstanceReconciler) reconcileMySQLStatefulSet(ctx context.Context, instance *opsmeshv1alpha1.OpsMeshInstance) error {
	desired := mysqlStatefulSet(instance)
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference on MySQL StatefulSet: %w", err)
	}

	var existing appsv1.StatefulSet
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	if err := r.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get MySQL StatefulSet: %w", err)
		}
		return r.Create(ctx, desired)
	}

	if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		desired.ResourceVersion = existing.ResourceVersion
		return r.Update(ctx, desired)
	}
	return nil
}

// reconcileRedisStatefulSet creates or updates the Redis StatefulSet.
func (r *OpsMeshInstanceReconciler) reconcileRedisStatefulSet(ctx context.Context, instance *opsmeshv1alpha1.OpsMeshInstance) error {
	desired := redisStatefulSet(instance)
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference on Redis StatefulSet: %w", err)
	}

	var existing appsv1.StatefulSet
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	if err := r.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get Redis StatefulSet: %w", err)
		}
		return r.Create(ctx, desired)
	}

	if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		desired.ResourceVersion = existing.ResourceVersion
		return r.Update(ctx, desired)
	}
	return nil
}

// deploymentSpecEqual reports whether two DeploymentSpecs are functionally
// equivalent for our purposes. We compare the user-mutable fields rather than
// the whole struct so that defaulted server-side fields don't trigger churn.
func deploymentSpecEqual(a, b appsv1.DeploymentSpec) bool {
	if !reflect.DeepEqual(a.Replicas, b.Replicas) {
		return false
	}
	if !equality.Semantic.DeepEqual(a.Selector, b.Selector) {
		return false
	}
	return equality.Semantic.DeepEqual(a.Template, b.Template)
}

// SetupWithManager wires the reconciler into the controller-runtime manager.
func (r *OpsMeshInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsmeshv1alpha1.OpsMeshInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

// conditionTracker batches status updates so we only issue a single Patch.
type conditionTracker struct {
	instance *opsmeshv1alpha1.OpsMeshInstance
	// base is a deep copy of the instance status captured at tracker creation
	// time. We diff against it when flushing so the merge patch only carries
	// the conditions that changed during this reconcile pass.
	base  *opsmeshv1alpha1.OpsMeshInstance
	dirty bool
}

func newConditionTracker(instance *opsmeshv1alpha1.OpsMeshInstance) *conditionTracker {
	return &conditionTracker{
		instance: instance,
		base:     instance.DeepCopy(),
	}
}

func (c *conditionTracker) set(t string, s metav1.ConditionStatus, reason, msg string) {
	cond := metav1.Condition{
		Type:               t,
		Status:             s,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: c.instance.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&c.instance.Status.Conditions, cond)
	c.dirty = true
}

func (c *conditionTracker) remove(t string) {
	before := len(c.instance.Status.Conditions)
	meta.RemoveStatusCondition(&c.instance.Status.Conditions, t)
	if len(c.instance.Status.Conditions) != before {
		c.dirty = true
	}
}

// statusClient is the minimal interface needed to patch a resource's /status
// subresource. We deliberately avoid client.Client here because the reconciler
// struct has a Scheme field that shadows client.Client's Scheme() method,
// which would otherwise prevent *OpsMeshInstanceReconciler from satisfying
// the full client.Client interface.
type statusClient interface {
	Status() client.StatusWriter
}

// flush patches the OpsMeshInstance status if it changed and returns the
// supplied error so callers can `return ..., conditions.flush(ctx, r, err)`.
func (c *conditionTracker) flush(ctx context.Context, r statusClient, original error) error {
	if !c.dirty {
		return original
	}
	// Use a merge patch diffed against the status snapshot taken when the
	// tracker was created, so only the conditions changed this pass are sent.
	if err := r.Status().Patch(ctx, c.instance, client.MergeFrom(c.base)); err != nil {
		// Surface the status patch error but keep the original reconcile error
		// so the caller still requeues on the underlying failure.
		if original == nil {
			return fmt.Errorf("patch OpsMeshInstance status: %w", err)
		}
		return fmt.Errorf("%v (and patch status: %w)", original, err)
	}
	return original
}
