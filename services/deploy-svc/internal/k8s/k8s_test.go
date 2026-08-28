package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newTestClient() (*Client, *fake.Clientset) {
	fc := fake.NewSimpleClientset()
	c := &Client{
		clientset: fc,
		connected: true,
		simDeploy: make(map[string]DeploymentSpec),
		simPods:   make(map[string]PodInfo),
		simNodes:  make(map[string]NodeResources),
	}
	return c, fc
}

func TestCreateDeployment(t *testing.T) {
	c, _ := newTestClient()
	ctx := context.Background()

	spec := &DeploymentSpec{
		Name:      "test-deploy",
		Namespace: "default",
		Replicas:  3,
		Image:     "nginx:1.25",
		Labels:    map[string]string{"app": "test"},
	}

	deploy, err := c.CreateDeployment(ctx, "default", spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}
	if deploy.Name != "test-deploy" {
		t.Fatalf("expected name test-deploy, got %s", deploy.Name)
	}
	if *deploy.Spec.Replicas != 3 {
		t.Fatalf("expected 3 replicas, got %d", *deploy.Spec.Replicas)
	}
}

func TestGetDeployment(t *testing.T) {
	c, fc := newTestClient()
	ctx := context.Background()

	// Pre-populate with a deployment via fake clientset.
	existing := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-deploy",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "main", Image: "redis:7"}},
				},
			},
		},
	}
	_, err := fc.AppsV1().Deployments("default").Create(ctx, existing, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create pre-existing deployment: %v", err)
	}

	deploy, err := c.GetDeployment(ctx, "default", "existing-deploy")
	if err != nil {
		t.Fatalf("GetDeployment failed: %v", err)
	}
	if deploy.Name != "existing-deploy" {
		t.Fatalf("expected name existing-deploy, got %s", deploy.Name)
	}
	if *deploy.Spec.Replicas != 2 {
		t.Fatalf("expected 2 replicas, got %d", *deploy.Spec.Replicas)
	}
}

func TestUpdateDeployment(t *testing.T) {
	c, _ := newTestClient()
	ctx := context.Background()

	spec := &DeploymentSpec{
		Name:      "update-deploy",
		Namespace: "default",
		Replicas:  1,
		Image:     "nginx:1.24",
		Labels:    map[string]string{"app": "update-test"},
	}
	_, err := c.CreateDeployment(ctx, "default", spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	spec.Replicas = 5
	spec.Image = "nginx:1.25"
	updated, err := c.UpdateDeployment(ctx, "default", spec)
	if err != nil {
		t.Fatalf("UpdateDeployment failed: %v", err)
	}
	if *updated.Spec.Replicas != 5 {
		t.Fatalf("expected 5 replicas after update, got %d", *updated.Spec.Replicas)
	}
	if updated.Spec.Template.Spec.Containers[0].Image != "nginx:1.25" {
		t.Fatalf("expected image nginx:1.25, got %s", updated.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestDeleteDeployment(t *testing.T) {
	c, _ := newTestClient()
	ctx := context.Background()

	spec := &DeploymentSpec{
		Name:      "delete-deploy",
		Namespace: "default",
		Replicas:  1,
		Image:     "busybox:latest",
		Labels:    map[string]string{"app": "delete-test"},
	}
	_, err := c.CreateDeployment(ctx, "default", spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	err = c.DeleteDeployment(ctx, "default", "delete-deploy")
	if err != nil {
		t.Fatalf("DeleteDeployment failed: %v", err)
	}

	_, err = c.GetDeployment(ctx, "default", "delete-deploy")
	if err == nil {
		t.Fatal("expected error after deleting deployment, got nil")
	}
}

func TestGetPodList(t *testing.T) {
	c, fc := newTestClient()
	ctx := context.Background()

	// Create fake pods.
	for i := 1; i <= 3; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-" + string(rune('0'+i)),
				Namespace: "default",
				Labels:    map[string]string{"app": "test"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "main", Image: "nginx"}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
		_, err := fc.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create pod: %v", err)
		}
	}

	pods, err := c.GetPodList(ctx, "default", "")
	if err != nil {
		t.Fatalf("GetPodList failed: %v", err)
	}
	if len(pods) != 3 {
		t.Fatalf("expected 3 pods, got %d", len(pods))
	}

	for _, p := range pods {
		if p.Status != "Running" {
			t.Fatalf("expected pod status Running, got %s", p.Status)
		}
	}
}

func TestGetNodeResources(t *testing.T) {
	c, fc := newTestClient()
	ctx := context.Background()

	// Create fake nodes.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-node-1",
			Labels: map[string]string{"kubernetes.io/os": "linux"},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			},
		},
	}
	_, err := fc.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	nodes, err := c.GetNodeResources(ctx)
	if err != nil {
		t.Fatalf("GetNodeResources failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Name != "test-node-1" {
		t.Fatalf("expected node name test-node-1, got %s", nodes[0].Name)
	}
	if nodes[0].CPUCapacity != 8000 {
		t.Fatalf("expected 8000 millicores, got %d", nodes[0].CPUCapacity)
	}
}

func TestScaleDeployment(t *testing.T) {
	c, _ := newTestClient()
	ctx := context.Background()

	spec := &DeploymentSpec{
		Name:      "scale-deploy",
		Namespace: "default",
		Replicas:  2,
		Image:     "nginx:latest",
		Labels:    map[string]string{"app": "scale-test"},
	}
	_, err := c.CreateDeployment(ctx, "default", spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	err = c.ScaleDeployment(ctx, "default", "scale-deploy", 10)
	if err != nil {
		t.Fatalf("ScaleDeployment failed: %v", err)
	}

	deploy, err := c.GetDeployment(ctx, "default", "scale-deploy")
	if err != nil {
		t.Fatalf("GetDeployment after scale failed: %v", err)
	}
	if *deploy.Spec.Replicas != 10 {
		t.Fatalf("expected 10 replicas after scale, got %d", *deploy.Spec.Replicas)
	}
}

func TestSimulatedMode(t *testing.T) {
	// Create a client in simulated mode (no real clientset).
	c := &Client{
		connected: false,
		simDeploy: make(map[string]DeploymentSpec),
		simPods:   make(map[string]PodInfo),
		simNodes:  make(map[string]NodeResources),
	}
	c.initSimulatedData()

	ctx := context.Background()

	// Test simulated get.
	deploy, err := c.GetDeployment(ctx, "default", "sim-deployment")
	if err != nil {
		t.Fatalf("simGetDeployment failed: %v", err)
	}
	if deploy.Name != "sim-deployment" {
		t.Fatalf("expected sim-deployment, got %s", deploy.Name)
	}

	// Test simulated pod list.
	pods, err := c.GetPodList(ctx, "default", "")
	if err != nil {
		t.Fatalf("simGetPodList failed: %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("expected 2 simulated pods, got %d", len(pods))
	}

	// Test simulated node resources.
	nodes, err := c.GetNodeResources(ctx)
	if err != nil {
		t.Fatalf("simGetNodeResources failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 simulated node, got %d", len(nodes))
	}

	// Test simulated scale.
	err = c.ScaleDeployment(ctx, "default", "sim-deployment", 5)
	if err != nil {
		t.Fatalf("simScaleDeployment failed: %v", err)
	}
	deploy, _ = c.GetDeployment(ctx, "default", "sim-deployment")
	if *deploy.Spec.Replicas != 5 {
		t.Fatalf("expected 5 replicas after simulated scale, got %d", *deploy.Spec.Replicas)
	}
}

func TestIsConnected(t *testing.T) {
	c, _ := newTestClient()
	if !c.IsConnected() {
		t.Fatal("expected connected client to report IsConnected()=true")
	}

	simClient := &Client{connected: false}
	if simClient.IsConnected() {
		t.Fatal("expected simulated client to report IsConnected()=false")
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}
