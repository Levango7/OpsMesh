package k8s

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// DeploymentSpec represents a simplified deployment specification.
type DeploymentSpec struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Replicas  int32             `json:"replicas"`
	Image     string            `json:"image"`
	Labels    map[string]string `json:"labels"`
}

// PodInfo represents simplified pod information.
type PodInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Status    string            `json:"status"`
	Labels    map[string]string `json:"labels"`
	Node      string            `json:"node"`
	CreatedAt time.Time         `json:"created_at"`
}

// NodeResources represents a node's available resources.
type NodeResources struct {
	Name           string            `json:"name"`
	CPUCapacityStr string            `json:"cpu_capacity"`
	MemCapacityStr string            `json:"mem_capacity"`
	CPUCapacity    int64             `json:"cpu_capacity_millicores"`
	MemCapacity    int64             `json:"mem_capacity_bytes"`
	Labels         map[string]string `json:"labels"`
}

// Client wraps a Kubernetes clientset with graceful fallback to simulated mode.
type Client struct {
	clientset kubernetes.Interface
	connected bool
	mu        sync.RWMutex
	simDeploy map[string]DeploymentSpec
	simPods   map[string]PodInfo
	simNodes  map[string]NodeResources
}

// ClientConfig holds configuration for creating a K8s client.
type ClientConfig struct {
	KubeconfigPath string
	Namespace      string
	MasterURL      string
}

// NewClient creates a new K8s client, attempting real cluster connection.
// Falls back to simulated mode if no cluster is reachable.
func NewClient(cfg ClientConfig) *Client {
	c := &Client{
		simDeploy: make(map[string]DeploymentSpec),
		simPods:   make(map[string]PodInfo),
		simNodes:  make(map[string]NodeResources),
	}

	clientset, err := createClientset(cfg)
	if err != nil {
		log.Printf("[k8s] Unable to connect to Kubernetes cluster: %v — using simulated mode", err)
		c.connected = false
		c.initSimulatedData()
		return c
	}

	c.clientset = clientset
	c.connected = true

	// Verify connectivity with a lightweight call.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		log.Printf("[k8s] Cluster reachable but API call failed: %v — using simulated mode", err)
		c.connected = false
		c.clientset = nil
		c.initSimulatedData()
		return c
	}

	log.Printf("[k8s] Connected to Kubernetes cluster successfully")
	return c
}

// createClientset attempts to create a real Kubernetes clientset.
func createClientset(cfg ClientConfig) (kubernetes.Interface, error) {
	var restConfig *rest.Config
	var err error

	if cfg.KubeconfigPath != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags(cfg.MasterURL, cfg.KubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig %q: %w", cfg.KubeconfigPath, err)
		}
	} else {
		// Try in-cluster config first.
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig location.
			restConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
			if err != nil {
				return nil, fmt.Errorf("failed to build in-cluster or default kubeconfig: %w", err)
			}
		}
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return clientset, nil
}

// IsConnected returns true if the client is connected to a real cluster.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetDeployment retrieves a deployment by name and namespace.
func (c *Client) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	if !c.connected {
		return c.simGetDeployment(namespace, name)
	}
	return c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
}

// CreateDeployment creates a new deployment in the cluster.
func (c *Client) CreateDeployment(ctx context.Context, namespace string, spec *DeploymentSpec) (*appsv1.Deployment, error) {
	if !c.connected {
		return c.simCreateDeployment(namespace, spec)
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: namespace,
			Labels:    spec.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: spec.Labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: spec.Labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  spec.Name,
							Image: spec.Image,
						},
					},
				},
			},
		},
	}

	return c.clientset.AppsV1().Deployments(namespace).Create(ctx, deploy, metav1.CreateOptions{})
}

// UpdateDeployment updates an existing deployment.
func (c *Client) UpdateDeployment(ctx context.Context, namespace string, spec *DeploymentSpec) (*appsv1.Deployment, error) {
	if !c.connected {
		return c.simUpdateDeployment(namespace, spec)
	}

	deploy, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("deployment %q not found: %w", spec.Name, err)
	}

	deploy.Spec.Replicas = &spec.Replicas
	deploy.Spec.Template.Spec.Containers = []corev1.Container{
		{
			Name:  spec.Name,
			Image: spec.Image,
		},
	}
	if spec.Labels != nil {
		deploy.ObjectMeta.Labels = spec.Labels
		deploy.Spec.Selector = &metav1.LabelSelector{MatchLabels: spec.Labels}
		deploy.Spec.Template.ObjectMeta.Labels = spec.Labels
	}

	return c.clientset.AppsV1().Deployments(namespace).Update(ctx, deploy, metav1.UpdateOptions{})
}

// DeleteDeployment deletes a deployment by name and namespace.
func (c *Client) DeleteDeployment(ctx context.Context, namespace, name string) error {
	if !c.connected {
		return c.simDeleteDeployment(namespace, name)
	}
	return c.clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// GetPodList lists pods in a namespace matching the given label selector.
func (c *Client) GetPodList(ctx context.Context, namespace, labelSelector string) ([]PodInfo, error) {
	if !c.connected {
		return c.simGetPodList(namespace, labelSelector)
	}

	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	result := make([]PodInfo, 0, len(pods.Items))
	for _, p := range pods.Items {
		result = append(result, PodInfo{
			Name:      p.Name,
			Namespace: p.Namespace,
			Status:    string(p.Status.Phase),
			Labels:    p.Labels,
			Node:      p.Spec.NodeName,
			CreatedAt: p.CreationTimestamp.Time,
		})
	}
	return result, nil
}

// GetNodeResources returns resource information for all nodes in the cluster.
func (c *Client) GetNodeResources(ctx context.Context) ([]NodeResources, error) {
	if !c.connected {
		return c.simGetNodeResources()
	}

	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	result := make([]NodeResources, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		cpuCap := n.Status.Capacity.Cpu()
		memCap := n.Status.Capacity.Memory()

		result = append(result, NodeResources{
			Name:           n.Name,
			CPUCapacityStr: cpuCap.String(),
			MemCapacityStr: memCap.String(),
			CPUCapacity:    cpuCap.MilliValue(),
			MemCapacity:    memCap.Value(),
			Labels:         n.Labels,
		})
	}
	return result, nil
}

// ScaleDeployment scales a deployment to the specified number of replicas.
func (c *Client) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	if !c.connected {
		return c.simScaleDeployment(namespace, name, replicas)
	}

	deploy, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %q for scaling: %w", name, err)
	}
	deploy.Spec.Replicas = &replicas
	_, err = c.clientset.AppsV1().Deployments(namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale deployment %q: %w", name, err)
	}
	return nil
}

// initSimulatedData populates the simulated store with sample data.
func (c *Client) initSimulatedData() {
	c.simDeploy["default/sim-deployment"] = DeploymentSpec{
		Name:      "sim-deployment",
		Namespace: "default",
		Replicas:  3,
		Image:     "nginx:latest",
		Labels:    map[string]string{"app": "sim"},
	}

	c.simPods["sim-pod-1"] = PodInfo{
		Name:      "sim-pod-1",
		Namespace: "default",
		Status:    "Running",
		Labels:    map[string]string{"app": "sim"},
		Node:      "sim-node-1",
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	c.simPods["sim-pod-2"] = PodInfo{
		Name:      "sim-pod-2",
		Namespace: "default",
		Status:    "Running",
		Labels:    map[string]string{"app": "sim"},
		Node:      "sim-node-1",
		CreatedAt: time.Now().Add(-30 * time.Minute),
	}

	c.simNodes["sim-node-1"] = NodeResources{
		Name:           "sim-node-1",
		CPUCapacityStr: "4",
		MemCapacityStr: "16Gi",
		CPUCapacity:    4000,
		MemCapacity:    16 * 1024 * 1024 * 1024,
		Labels:         map[string]string{"kubernetes.io/os": "linux"},
	}
}

// Simulated mode implementations.

func (c *Client) simGetDeployment(namespace, name string) (*appsv1.Deployment, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	spec, ok := c.simDeploy[namespace+"/"+name]
	if !ok {
		return nil, fmt.Errorf("deployment %q not found in namespace %q (simulated)", name, namespace)
	}

	replicas := spec.Replicas
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: spec.Name, Image: spec.Image}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:          replicas,
			AvailableReplicas: replicas,
		},
	}, nil
}

func (c *Client) simCreateDeployment(namespace string, spec *DeploymentSpec) (*appsv1.Deployment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if spec.Namespace == "" {
		spec.Namespace = namespace
	}
	c.simDeploy[namespace+"/"+spec.Name] = *spec

	replicas := spec.Replicas
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: namespace,
			Labels:    spec.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: spec.Name, Image: spec.Image}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:          replicas,
			AvailableReplicas: replicas,
		},
	}, nil
}

func (c *Client) simUpdateDeployment(namespace string, spec *DeploymentSpec) (*appsv1.Deployment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := namespace + "/" + spec.Name
	if _, ok := c.simDeploy[key]; !ok {
		return nil, fmt.Errorf("deployment %q not found in namespace %q (simulated)", spec.Name, namespace)
	}

	c.simDeploy[key] = *spec

	replicas := spec.Replicas
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: namespace,
			Labels:    spec.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: spec.Name, Image: spec.Image}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:          replicas,
			AvailableReplicas: replicas,
		},
	}, nil
}

func (c *Client) simDeleteDeployment(namespace, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := namespace + "/" + name
	if _, ok := c.simDeploy[key]; !ok {
		return fmt.Errorf("deployment %q not found in namespace %q (simulated)", name, namespace)
	}
	delete(c.simDeploy, key)
	return nil
}

func (c *Client) simGetPodList(namespace, labelSelector string) ([]PodInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []PodInfo
	for _, pod := range c.simPods {
		if pod.Namespace == namespace {
			if labelSelector == "" || pod.Labels["app"] == labelSelector {
				result = append(result, pod)
			}
		}
	}
	return result, nil
}

func (c *Client) simGetNodeResources() ([]NodeResources, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]NodeResources, 0, len(c.simNodes))
	for _, n := range c.simNodes {
		result = append(result, n)
	}
	return result, nil
}

func (c *Client) simScaleDeployment(namespace, name string, replicas int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := namespace + "/" + name
	spec, ok := c.simDeploy[key]
	if !ok {
		return fmt.Errorf("deployment %q not found in namespace %q (simulated)", name, namespace)
	}
	spec.Replicas = replicas
	c.simDeploy[key] = spec
	return nil
}
