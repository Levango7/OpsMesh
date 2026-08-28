package cloud

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/k8s"
)

// Provider type constants.
const (
	ProviderAWS     = "aws"
	ProviderHuawei  = "huawei"
	ProviderAli     = "ali"
	ProviderOnPrem  = "onprem"
)

// DeploymentStatus represents the status of a cloud deployment.
type DeploymentStatus string

const (
	StatusPending   DeploymentStatus = "pending"
	StatusRunning   DeploymentStatus = "running"
	StatusSuccess   DeploymentStatus = "success"
	StatusFailed    DeploymentStatus = "failed"
	StatusRolledBack DeploymentStatus = "rolled_back"
)

// DeploymentConfig holds the configuration for a deployment to a cloud provider.
type DeploymentConfig struct {
	DeploymentID string            `json:"deployment_id"`
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Provider     string            `json:"provider"`
	Region       string            `json:"region"`
	Cluster      string            `json:"cluster"`
	RepoURL      string            `json:"repo_url"`
	Content      string            `json:"content"`
	Path         string            `json:"path"`
	Targets      []string          `json:"targets"`
	Parameters   map[string]string `json:"parameters"`
}

// DeploymentResult holds the result of a cloud deployment operation.
type DeploymentResult struct {
	DeploymentID  string           `json:"deployment_id"`
	Provider      string           `json:"provider"`
	Status        DeploymentStatus `json:"status"`
	Region        string           `json:"region"`
	Message       string           `json:"message"`
	ExternalRef   string           `json:"external_ref,omitempty"`
	DeployedAt    time.Time        `json:"deployed_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

// CloudProvider defines the interface for multi-cloud deployment operations.
type CloudProvider interface {
	// Validate checks if the deployment config is valid for this provider.
	Validate(config DeploymentConfig) error
	// Deploy executes the deployment to the cloud provider.
	Deploy(config DeploymentConfig) (DeploymentResult, error)
	// GetStatus retrieves the current status of a deployment.
	GetStatus(deploymentID string) (DeploymentResult, error)
	// Rollback reverts a deployment.
	Rollback(deploymentID string) (DeploymentResult, error)
	// ProviderName returns the provider name.
	ProviderName() string
}

// ErrProviderUnsupported is returned when an unknown provider type is requested.
var ErrProviderUnsupported = errors.New("unsupported cloud provider")

// ErrDeploymentNotFound is returned when a deployment is not found.
var ErrDeploymentNotFound = errors.New("cloud deployment not found")

// ErrInvalidConfig is returned when the deployment config is invalid.
var ErrInvalidConfig = errors.New("invalid deployment config")

// AWSProvider implements CloudProvider for Amazon Web Services (ECS/EC2).
type AWSProvider struct {
	mu            sync.RWMutex
	deployments   map[string]DeploymentResult
	region        string
	K8sClient     *k8s.Client
}

// NewAWSProvider creates a new AWS provider.
func NewAWSProvider(region string) *AWSProvider {
	if region == "" {
		region = "us-east-1"
	}
	p := &AWSProvider{
		deployments: make(map[string]DeploymentResult),
		region:      region,
	}
	registerProvider(p)
	return p
}

// ProviderName returns the provider name.
func (a *AWSProvider) ProviderName() string {
	return ProviderAWS
}

// Validate checks if the deployment config is valid for AWS.
func (a *AWSProvider) Validate(config DeploymentConfig) error {
	if config.DeploymentID == "" {
		return fmt.Errorf("%w: deployment_id required", ErrInvalidConfig)
	}
	if config.Name == "" {
		return fmt.Errorf("%w: name required", ErrInvalidConfig)
	}
	if config.Region == "" {
		config.Region = a.region
	}
	if config.Type != "k8s" && config.Type != "script" && config.Type != "file" {
		return fmt.Errorf("%w: unsupported deployment type %q for AWS", ErrInvalidConfig, config.Type)
	}
	return nil
}

// Deploy executes the deployment to AWS.
func (a *AWSProvider) Deploy(config DeploymentConfig) (DeploymentResult, error) {
	if err := a.Validate(config); err != nil {
		return DeploymentResult{}, err
	}

	if config.Region == "" {
		config.Region = a.region
	}

	now := time.Now()

	// If this is a K8s deployment and we have a K8s client, use the real API.
	if config.Type == "k8s" && a.K8sClient != nil && a.K8sClient.IsConnected() {
		spec := &k8s.DeploymentSpec{
			Name:      config.Name,
			Namespace: config.Cluster,
			Replicas:  1,
			Image:     "nginx:latest",
			Labels:    map[string]string{"app": config.Name, "tenant": config.TenantID},
		}
		if config.Parameters != nil {
			if img, ok := config.Parameters["image"]; ok {
				spec.Image = img
			}
			if ns, ok := config.Parameters["namespace"]; ok {
				spec.Namespace = ns
			}
			if rep, ok := config.Parameters["replicas"]; ok {
				var r int32
				fmt.Sscanf(rep, "%d", &r)
				if r > 0 {
					spec.Replicas = r
				}
			}
		}
		if spec.Namespace == "" {
			spec.Namespace = "default"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		deploy, err := a.K8sClient.CreateDeployment(ctx, spec.Namespace, spec)
		if err != nil {
			return DeploymentResult{}, fmt.Errorf("k8s deploy failed: %w", err)
		}

		result := DeploymentResult{
			DeploymentID: config.DeploymentID,
			Provider:     ProviderAWS,
			Status:       StatusRunning,
			Region:       config.Region,
			Message:      fmt.Sprintf("Deployed %s to EKS cluster in %s (K8s deployment: %s)", config.Name, config.Region, deploy.Name),
			ExternalRef:  fmt.Sprintf("arn:aws:eks:%s:123456789:deployment/%s", config.Region, deploy.Name),
			DeployedAt:   now,
			UpdatedAt:    now,
		}

		a.mu.Lock()
		a.deployments[config.DeploymentID] = result
		a.mu.Unlock()

		return result, nil
	}

	result := DeploymentResult{
		DeploymentID: config.DeploymentID,
		Provider:     ProviderAWS,
		Status:       StatusRunning,
		Region:       config.Region,
		Message:      fmt.Sprintf("Deploying %s to AWS ECS/EC2 in %s", config.Name, config.Region),
		ExternalRef:  fmt.Sprintf("arn:aws:ecs:%s:123456789:deployment/%s", config.Region, config.DeploymentID),
		DeployedAt:   now,
		UpdatedAt:    now,
	}

	a.mu.Lock()
	a.deployments[config.DeploymentID] = result
	a.mu.Unlock()

	return result, nil
}

// GetStatus retrieves the current status of an AWS deployment.
func (a *AWSProvider) GetStatus(deploymentID string) (DeploymentResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result, ok := a.deployments[deploymentID]
	if !ok {
		return DeploymentResult{}, ErrDeploymentNotFound
	}
	return result, nil
}

// Rollback reverts an AWS deployment.
func (a *AWSProvider) Rollback(deploymentID string) (DeploymentResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	result, ok := a.deployments[deploymentID]
	if !ok {
		return DeploymentResult{}, ErrDeploymentNotFound
	}

	result.Status = StatusRolledBack
	result.Message = fmt.Sprintf("Rolled back deployment %s on AWS", deploymentID)
	result.UpdatedAt = time.Now()
	a.deployments[deploymentID] = result

	return result, nil
}

// HuaweiProvider implements CloudProvider for Huawei Cloud.
type HuaweiProvider struct {
	mu            sync.RWMutex
	deployments   map[string]DeploymentResult
	region        string
	K8sClient     *k8s.Client
}

// NewHuaweiProvider creates a new Huawei Cloud provider.
func NewHuaweiProvider(region string) *HuaweiProvider {
	if region == "" {
		region = "cn-north-4"
	}
	p := &HuaweiProvider{
		deployments: make(map[string]DeploymentResult),
		region:      region,
	}
	registerProvider(p)
	return p
}

// ProviderName returns the provider name.
func (h *HuaweiProvider) ProviderName() string {
	return ProviderHuawei
}

// Validate checks if the deployment config is valid for Huawei Cloud.
func (h *HuaweiProvider) Validate(config DeploymentConfig) error {
	if config.DeploymentID == "" {
		return fmt.Errorf("%w: deployment_id required", ErrInvalidConfig)
	}
	if config.Name == "" {
		return fmt.Errorf("%w: name required", ErrInvalidConfig)
	}
	if config.Region == "" {
		config.Region = h.region
	}
	if config.Type != "k8s" && config.Type != "script" && config.Type != "file" {
		return fmt.Errorf("%w: unsupported deployment type %q for Huawei Cloud", ErrInvalidConfig, config.Type)
	}
	return nil
}

// Deploy executes the deployment to Huawei Cloud.
func (h *HuaweiProvider) Deploy(config DeploymentConfig) (DeploymentResult, error) {
	if err := h.Validate(config); err != nil {
		return DeploymentResult{}, err
	}

	if config.Region == "" {
		config.Region = h.region
	}

	now := time.Now()

	if config.Type == "k8s" && h.K8sClient != nil && h.K8sClient.IsConnected() {
		spec := &k8s.DeploymentSpec{
			Name:      config.Name,
			Namespace: config.Cluster,
			Replicas:  1,
			Image:     "nginx:latest",
			Labels:    map[string]string{"app": config.Name, "tenant": config.TenantID},
		}
		if config.Parameters != nil {
			if img, ok := config.Parameters["image"]; ok {
				spec.Image = img
			}
			if ns, ok := config.Parameters["namespace"]; ok {
				spec.Namespace = ns
			}
			if rep, ok := config.Parameters["replicas"]; ok {
				var r int32
				fmt.Sscanf(rep, "%d", &r)
				if r > 0 {
					spec.Replicas = r
				}
			}
		}
		if spec.Namespace == "" {
			spec.Namespace = "default"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		deploy, err := h.K8sClient.CreateDeployment(ctx, spec.Namespace, spec)
		if err != nil {
			return DeploymentResult{}, fmt.Errorf("k8s deploy failed: %w", err)
		}

		result := DeploymentResult{
			DeploymentID: config.DeploymentID,
			Provider:     ProviderHuawei,
			Status:       StatusRunning,
			Region:       config.Region,
			Message:      fmt.Sprintf("Deployed %s to Huawei CCE in %s (K8s deployment: %s)", config.Name, config.Region, deploy.Name),
			ExternalRef:  fmt.Sprintf("huawei:cce:%s:deployment/%s", config.Region, deploy.Name),
			DeployedAt:   now,
			UpdatedAt:    now,
		}

		h.mu.Lock()
		h.deployments[config.DeploymentID] = result
		h.mu.Unlock()

		return result, nil
	}

	result := DeploymentResult{
		DeploymentID: config.DeploymentID,
		Provider:     ProviderHuawei,
		Status:       StatusRunning,
		Region:       config.Region,
		Message:      fmt.Sprintf("Deploying %s to Huawei Cloud CCE in %s", config.Name, config.Region),
		ExternalRef:  fmt.Sprintf("huawei:cce:%s:deployment/%s", config.Region, config.DeploymentID),
		DeployedAt:   now,
		UpdatedAt:    now,
	}

	h.mu.Lock()
	h.deployments[config.DeploymentID] = result
	h.mu.Unlock()

	return result, nil
}

// GetStatus retrieves the current status of a Huawei Cloud deployment.
func (h *HuaweiProvider) GetStatus(deploymentID string) (DeploymentResult, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result, ok := h.deployments[deploymentID]
	if !ok {
		return DeploymentResult{}, ErrDeploymentNotFound
	}
	return result, nil
}

// Rollback reverts a Huawei Cloud deployment.
func (h *HuaweiProvider) Rollback(deploymentID string) (DeploymentResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	result, ok := h.deployments[deploymentID]
	if !ok {
		return DeploymentResult{}, ErrDeploymentNotFound
	}

	result.Status = StatusRolledBack
	result.Message = fmt.Sprintf("Rolled back deployment %s on Huawei Cloud", deploymentID)
	result.UpdatedAt = time.Now()
	h.deployments[deploymentID] = result

	return result, nil
}

// AliProvider implements CloudProvider for Alibaba Cloud.
type AliProvider struct {
	mu            sync.RWMutex
	deployments   map[string]DeploymentResult
	region        string
	K8sClient     *k8s.Client
}

// NewAliProvider creates a new Alibaba Cloud provider.
func NewAliProvider(region string) *AliProvider {
	if region == "" {
		region = "cn-hangzhou"
	}
	p := &AliProvider{
		deployments: make(map[string]DeploymentResult),
		region:      region,
	}
	registerProvider(p)
	return p
}

// ProviderName returns the provider name.
func (a *AliProvider) ProviderName() string {
	return ProviderAli
}

// Validate checks if the deployment config is valid for Alibaba Cloud.
func (a *AliProvider) Validate(config DeploymentConfig) error {
	if config.DeploymentID == "" {
		return fmt.Errorf("%w: deployment_id required", ErrInvalidConfig)
	}
	if config.Name == "" {
		return fmt.Errorf("%w: name required", ErrInvalidConfig)
	}
	if config.Region == "" {
		config.Region = a.region
	}
	if config.Type != "k8s" && config.Type != "script" && config.Type != "file" {
		return fmt.Errorf("%w: unsupported deployment type %q for Alibaba Cloud", ErrInvalidConfig, config.Type)
	}
	return nil
}

// Deploy executes the deployment to Alibaba Cloud.
func (a *AliProvider) Deploy(config DeploymentConfig) (DeploymentResult, error) {
	if err := a.Validate(config); err != nil {
		return DeploymentResult{}, err
	}

	if config.Region == "" {
		config.Region = a.region
	}

	now := time.Now()

	if config.Type == "k8s" && a.K8sClient != nil && a.K8sClient.IsConnected() {
		spec := &k8s.DeploymentSpec{
			Name:      config.Name,
			Namespace: config.Cluster,
			Replicas:  1,
			Image:     "nginx:latest",
			Labels:    map[string]string{"app": config.Name, "tenant": config.TenantID},
		}
		if config.Parameters != nil {
			if img, ok := config.Parameters["image"]; ok {
				spec.Image = img
			}
			if ns, ok := config.Parameters["namespace"]; ok {
				spec.Namespace = ns
			}
			if rep, ok := config.Parameters["replicas"]; ok {
				var r int32
				fmt.Sscanf(rep, "%d", &r)
				if r > 0 {
					spec.Replicas = r
				}
			}
		}
		if spec.Namespace == "" {
			spec.Namespace = "default"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		deploy, err := a.K8sClient.CreateDeployment(ctx, spec.Namespace, spec)
		if err != nil {
			return DeploymentResult{}, fmt.Errorf("k8s deploy failed: %w", err)
		}

		result := DeploymentResult{
			DeploymentID: config.DeploymentID,
			Provider:     ProviderAli,
			Status:       StatusRunning,
			Region:       config.Region,
			Message:      fmt.Sprintf("Deployed %s to Alibaba ACK in %s (K8s deployment: %s)", config.Name, config.Region, deploy.Name),
			ExternalRef:  fmt.Sprintf("ali:ack:%s:deployment/%s", config.Region, deploy.Name),
			DeployedAt:   now,
			UpdatedAt:    now,
		}

		a.mu.Lock()
		a.deployments[config.DeploymentID] = result
		a.mu.Unlock()

		return result, nil
	}

	result := DeploymentResult{
		DeploymentID: config.DeploymentID,
		Provider:     ProviderAli,
		Status:       StatusRunning,
		Region:       config.Region,
		Message:      fmt.Sprintf("Deploying %s to Alibaba Cloud ACK in %s", config.Name, config.Region),
		ExternalRef:  fmt.Sprintf("ali:ack:%s:deployment/%s", config.Region, config.DeploymentID),
		DeployedAt:   now,
		UpdatedAt:    now,
	}

	a.mu.Lock()
	a.deployments[config.DeploymentID] = result
	a.mu.Unlock()

	return result, nil
}

// GetStatus retrieves the current status of an Alibaba Cloud deployment.
func (a *AliProvider) GetStatus(deploymentID string) (DeploymentResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result, ok := a.deployments[deploymentID]
	if !ok {
		return DeploymentResult{}, ErrDeploymentNotFound
	}
	return result, nil
}

// Rollback reverts an Alibaba Cloud deployment.
func (a *AliProvider) Rollback(deploymentID string) (DeploymentResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	result, ok := a.deployments[deploymentID]
	if !ok {
		return DeploymentResult{}, ErrDeploymentNotFound
	}

	result.Status = StatusRolledBack
	result.Message = fmt.Sprintf("Rolled back deployment %s on Alibaba Cloud", deploymentID)
	result.UpdatedAt = time.Now()
	a.deployments[deploymentID] = result

	return result, nil
}

// OnPremProvider implements CloudProvider for on-premise servers.
type OnPremProvider struct {
	mu            sync.RWMutex
	deployments   map[string]DeploymentResult
	K8sClient     *k8s.Client
}

// NewOnPremProvider creates a new on-premise provider.
func NewOnPremProvider() *OnPremProvider {
	p := &OnPremProvider{
		deployments: make(map[string]DeploymentResult),
	}
	registerProvider(p)
	return p
}

// ProviderName returns the provider name.
func (o *OnPremProvider) ProviderName() string {
	return ProviderOnPrem
}

// Validate checks if the deployment config is valid for on-premise deployment.
func (o *OnPremProvider) Validate(config DeploymentConfig) error {
	if config.DeploymentID == "" {
		return fmt.Errorf("%w: deployment_id required", ErrInvalidConfig)
	}
	if config.Name == "" {
		return fmt.Errorf("%w: name required", ErrInvalidConfig)
	}
	if len(config.Targets) == 0 {
		return fmt.Errorf("%w: at least one target required for on-premise deployment", ErrInvalidConfig)
	}
	if config.Type != "script" && config.Type != "file" {
		return fmt.Errorf("%w: on-premise supports script and file types only, got %q", ErrInvalidConfig, config.Type)
	}
	return nil
}

// Deploy executes the deployment to on-premise servers.
func (o *OnPremProvider) Deploy(config DeploymentConfig) (DeploymentResult, error) {
	if err := o.Validate(config); err != nil {
		return DeploymentResult{}, err
	}

	now := time.Now()
	result := DeploymentResult{
		DeploymentID: config.DeploymentID,
		Provider:     ProviderOnPrem,
		Status:       StatusRunning,
		Region:       "on-premise",
		Message:      fmt.Sprintf("Deploying %s to %d on-premise target(s)", config.Name, len(config.Targets)),
		ExternalRef:  fmt.Sprintf("onprem:deployment/%s", config.DeploymentID),
		DeployedAt:   now,
		UpdatedAt:    now,
	}

	o.mu.Lock()
	o.deployments[config.DeploymentID] = result
	o.mu.Unlock()

	return result, nil
}

// GetStatus retrieves the current status of an on-premise deployment.
func (o *OnPremProvider) GetStatus(deploymentID string) (DeploymentResult, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	result, ok := o.deployments[deploymentID]
	if !ok {
		return DeploymentResult{}, ErrDeploymentNotFound
	}
	return result, nil
}

// Rollback reverts an on-premise deployment.
func (o *OnPremProvider) Rollback(deploymentID string) (DeploymentResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	result, ok := o.deployments[deploymentID]
	if !ok {
		return DeploymentResult{}, ErrDeploymentNotFound
	}

	result.Status = StatusRolledBack
	result.Message = fmt.Sprintf("Rolled back deployment %s on on-premise servers", deploymentID)
	result.UpdatedAt = time.Now()
	o.deployments[deploymentID] = result

	return result, nil
}

// NewProvider creates a CloudProvider based on the provider type.
func NewProvider(providerType string) (CloudProvider, error) {
	switch providerType {
	case ProviderAWS:
		return NewAWSProvider(""), nil
	case ProviderHuawei:
		return NewHuaweiProvider(""), nil
	case ProviderAli:
		return NewAliProvider(""), nil
	case ProviderOnPrem:
		return NewOnPremProvider(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrProviderUnsupported, providerType)
	}
}

// providerRegistry holds the most recently created providers so they can be
// updated with a K8s client after construction.
var (
	providerRegistry []CloudProvider
	registryMu       sync.Mutex
)

// registerProvider adds a provider to the global registry.
func registerProvider(p CloudProvider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	providerRegistry = append(providerRegistry, p)
}

// SetK8sClient assigns a Kubernetes client to all registered providers that
// support K8s operations. Providers created before this call will not receive
// the client; call this before creating providers or recreate them afterward.
func SetK8sClient(client *k8s.Client) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, p := range providerRegistry {
		switch prov := p.(type) {
		case *AWSProvider:
			prov.K8sClient = client
		case *HuaweiProvider:
			prov.K8sClient = client
		case *AliProvider:
			prov.K8sClient = client
		case *OnPremProvider:
			prov.K8sClient = client
		}
	}
}

// ListProviders returns all supported provider type names.
func ListProviders() []string {
	return []string{ProviderAWS, ProviderHuawei, ProviderAli, ProviderOnPrem}
}
