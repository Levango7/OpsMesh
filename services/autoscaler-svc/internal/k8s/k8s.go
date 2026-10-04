package k8s

import (
	"fmt"
	"sync"
)

// Client 是**模拟执行器**：副本数存在进程内 map 里，不连接任何集群。
//
// 它此前被叫作"K8s client"并由 main.go 无条件装配，导致 API 对外报"已扩容"
// 而实际零副作用（见 Scaler 接口注释）。名字保留是为了不牵动既有调用方，
// 但身份已由 Executor() 显式暴露：ExecutorSimulated。
// 需要真实执行请走 real.go 的 NewFromKubeconfig / NewInCluster。
type Client struct {
	mu       sync.RWMutex
	replicas map[string]int32
}

// NewClient 创建模拟执行器（副本数只活在进程内存）。
func NewClient() *Client {
	return &Client{
		replicas: make(map[string]int32),
	}
}

// Executor 实现 Scaler：本实现恒为 ExecutorSimulated。
func (c *Client) Executor() string { return ExecutorSimulated }

// GetReplicas returns the current replica count for a deployment.
func (c *Client) GetReplicas(deployment, namespace string) (int32, error) {
	if deployment == "" {
		return 0, fmt.Errorf("deployment name cannot be empty")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := namespace + "/" + deployment
	return c.replicas[key], nil
}

// SetReplicas sets the replica count for a deployment.
func (c *Client) SetReplicas(deployment, namespace string, replicas int32) error {
	if deployment == "" {
		return fmt.Errorf("deployment name cannot be empty")
	}
	if replicas < 0 {
		return fmt.Errorf("replicas cannot be negative")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := namespace + "/" + deployment
	c.replicas[key] = replicas
	return nil
}

// RegisterDeployment registers a deployment with an initial replica count.
func (c *Client) RegisterDeployment(deployment, namespace string, replicas int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := namespace + "/" + deployment
	c.replicas[key] = replicas
}
