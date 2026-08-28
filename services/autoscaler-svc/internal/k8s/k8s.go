package k8s

import (
	"fmt"
	"sync"
)

// Client is a wrapper around the Kubernetes API for scaling operations.
// In production, this would use client-go. Here it provides an in-memory
// implementation for testing and development.
type Client struct {
	mu       sync.RWMutex
	replicas map[string]int32
}

// NewClient creates a new K8s client.
func NewClient() *Client {
	return &Client{
		replicas: make(map[string]int32),
	}
}

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
