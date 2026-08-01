package controller

import (
	"fmt"

	opsmeshv1alpha1 "opsmesh/operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// parseQuantity parses a storage string like "10Gi" into a resource.Quantity.
// Falls back to "1Gi" on parse error so the StatefulSet stays valid.
func parseQuantity(s string) resource.Quantity {
	if s == "" {
		return resource.MustParse("1Gi")
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return resource.MustParse("1Gi")
	}
	return q
}

// labelsForOpsMesh returns the common labels every managed resource carries.
func labelsForOpsMesh(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "opsmesh",
		"app.kubernetes.io/instance": name,
		"app.kubernetes.io/managed-by": "opsmesh-operator",
	}
}

// objectMeta builds a minimal ObjectMeta owned by the given OpsMeshInstance.
func objectMeta(cr *opsmeshv1alpha1.OpsMeshInstance, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: cr.Namespace,
		Labels:    labelsForOpsMesh(cr.Name),
	}
}

// controlPlaneDeployment builds the desired control-plane Deployment for cr.
func controlPlaneDeployment(cr *opsmeshv1alpha1.OpsMeshInstance) *appsv1.Deployment {
	name := cr.Name + "-control-plane"
	replicas := cr.Spec.Replicas

	env := []corev1.EnvVar{
		{Name: "OPSMESH_STORE", Value: cr.Spec.Store},
		{Name: "OPSMESH_SEGMENT_CIDR", Value: cr.Spec.SegmentCIDR},
		{Name: "OPSMESH_PRODUCTION", Value: boolStr(cr.Spec.Production)},
		{Name: "OPSMESH_TLS_ENABLED", Value: boolStr(cr.Spec.TLSEnabled)},
	}
	if cr.Spec.MySQL.Enabled {
		env = append(env, corev1.EnvVar{
			Name: "OPSMESH_MYSQL_HOST",
			Value: cr.Name + "-mysql",
		})
	}
	if cr.Spec.Redis.Enabled {
		env = append(env, corev1.EnvVar{
			Name: "OPSMESH_REDIS_HOST",
			Value: cr.Name + "-redis",
		})
	}

	dep := &appsv1.Deployment{
		ObjectMeta: objectMeta(cr, name),
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labelsForOpsMesh(cr.Name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labelsForOpsMesh(cr.Name),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "opsmesh",
							Image: cr.Spec.Image,
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Name: "http"},
								{ContainerPort: 9090, Name: "grpc"},
							},
							Env:   env,
							EnvFrom: nil,
							ImagePullPolicy: corev1.PullIfNotPresent,
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8080),
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return dep
}

// agentDaemonSet builds the desired node-agent DaemonSet for cr.
func agentDaemonSet(cr *opsmeshv1alpha1.OpsMeshInstance) *appsv1.DaemonSet {
	name := cr.Name + "-agent"
	ds := &appsv1.DaemonSet{
		ObjectMeta: objectMeta(cr, name),
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labelsForOpsMesh(cr.Name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labelsForOpsMesh(cr.Name),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "agent",
							Image: cr.Spec.AgentImage,
							SecurityContext: &corev1.SecurityContext{
								Privileged: boolPtr(true),
				},
							Env: []corev1.EnvVar{
								{Name: "OPSMESH_CONTROL_PLANE", Value: fmt.Sprintf("%s-control-plane.%s.svc:9090", cr.Name, cr.Namespace)},
								{Name: "OPSMESH_SEGMENT_CIDR", Value: cr.Spec.SegmentCIDR},
								{Name: "OPSMESH_TLS_ENABLED", Value: boolStr(cr.Spec.TLSEnabled)},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "cni", MountPath: "/host/cni"},
								{Name: "kubelet", MountPath: "/var/lib/kubelet"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "cni", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/etc/cni/net.d"}}},
						{Name: "kubelet", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet"}}},
					},
				},
			},
		},
	}
	return ds
}

// mysqlStatefulSet builds the desired MySQL StatefulSet for cr.
func mysqlStatefulSet(cr *opsmeshv1alpha1.OpsMeshInstance) *appsv1.StatefulSet {
	name := cr.Name + "-mysql"
	ss := &appsv1.StatefulSet{
		ObjectMeta: objectMeta(cr, name),
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Selector:    &metav1.LabelSelector{MatchLabels: labelsForOpsMesh(cr.Name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labelsForOpsMesh(cr.Name)},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "mysql",
							Image: "mysql:8.0",
							Env: []corev1.EnvVar{
								{Name: "MYSQL_ROOT_PASSWORD", Value: cr.Spec.MySQL.Password},
							},
							Ports: []corev1.ContainerPort{{ContainerPort: 3306, Name: "mysql"}},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/var/lib/mysql"},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: parseQuantity(cr.Spec.MySQL.Storage),
							},
						},
					},
				},
			},
		},
	}
	return ss
}

// redisStatefulSet builds the desired Redis StatefulSet for cr.
func redisStatefulSet(cr *opsmeshv1alpha1.OpsMeshInstance) *appsv1.StatefulSet {
	name := cr.Name + "-redis"
	ss := &appsv1.StatefulSet{
		ObjectMeta: objectMeta(cr, name),
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Selector:    &metav1.LabelSelector{MatchLabels: labelsForOpsMesh(cr.Name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labelsForOpsMesh(cr.Name)},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "redis",
							Image: "redis:7-alpine",
							Ports: []corev1.ContainerPort{{ContainerPort: 6379, Name: "redis"}},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/data"},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: parseQuantity(cr.Spec.Redis.Storage),
							},
						},
					},
				},
			},
		},
	}
	return ss
}

// headlessService builds the headless Service that fronts the control-plane.
func headlessService(cr *opsmeshv1alpha1.OpsMeshInstance) *corev1.Service {
	name := cr.Name + "-control-plane"
	svc := &corev1.Service{
		ObjectMeta: objectMeta(cr, name),
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  labelsForOpsMesh(cr.Name),
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8080, TargetPort: intstr.FromInt(8080)},
				{Name: "grpc", Port: 9090, TargetPort: intstr.FromInt(9090)},
			},
		},
	}
	return svc
}

// boolStr converts a bool to "true"/"false" for env vars.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// boolPtr returns a pointer to b.
func boolPtr(b bool) *bool { return &b }