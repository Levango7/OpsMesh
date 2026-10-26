package types

import (
	"context"
	"errors"
)

// ProviderType 识别云厂商。
type ProviderType string

const (
	ProviderTypeAliyun  ProviderType = "aliyun"
	ProviderTypeAWS     ProviderType = "aws"
	ProviderTypeAzure   ProviderType = "azure"
	ProviderTypeTencent ProviderType = "tencent"
	ProviderTypeHuawei  ProviderType = "huawei"
)

// AuthType 识别认证方式。
type AuthType string

const (
	AuthTypeAKSK            AuthType = "aksk"
	AuthTypeOIDC            AuthType = "oidc"
	AuthTypeManagedIdentity AuthType = "managed_identity"
)

// Credentials 云厂商认证信息（运行时临时持有者，不持久化明文）。
type Credentials struct {
	Type            AuthType `json:"type"`
	AccessKeyID     string   `json:"accessKeyId,omitempty"`
	AccessKeySecret string   `json:"accessKeySecret,omitempty"`
	OIDCToken       string   `json:"oidcToken,omitempty"`
	RoleARN         string   `json:"roleArn,omitempty"`
	Region          string   `json:"region"`
}

// Resource 通用云资源。
type Resource struct {
	ID           string            `json:"id"`
	Provider     ProviderType      `json:"provider"`
	Region       string            `json:"region"`
	ResourceType string            `json:"resourceType"`
	ResourceID   string            `json:"resourceId"`
	ResourceName string            `json:"resourceName"`
	Status       string            `json:"status"`
	PrivateIP    string            `json:"privateIp,omitempty"`
	PublicIP     string            `json:"publicIp,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	CreatedAt    string            `json:"createdAt,omitempty"`
	UpdatedAt    string            `json:"updatedAt,omitempty"`
}

// Region 云区域。
type Region struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Endpoint string `json:"endpoint,omitempty"`
}

// ErrNotImplemented 表示该能力尚未由具体厂商适配器实现。
var ErrNotImplemented = errors.New("not implemented")

// CreateResourceRequest 创建云资源请求。
type CreateResourceRequest struct {
	ResourceType string            `json:"resourceType"`
	Region       string            `json:"region"`
	Name         string            `json:"name"`
	Properties   map[string]string `json:"properties,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
}

// CloudProvider 所有云厂商适配器的统一抽象接口。
type CloudProvider interface {
	// Identity
	ProviderType() ProviderType

	// Connection
	ValidateCredentials(ctx context.Context, creds Credentials) error
	ListRegions(ctx context.Context, creds Credentials) ([]Region, error)

	// Resource discovery
	DiscoverVPCs(ctx context.Context, creds Credentials, region string) ([]Resource, error)
	DiscoverSubnets(ctx context.Context, creds Credentials, region string, vpcID string) ([]Resource, error)
	DiscoverInstances(ctx context.Context, creds Credentials, region string) ([]Resource, error)
	DiscoverClusters(ctx context.Context, creds Credentials, region string) ([]Resource, error)

	// Operations
	CreateResource(ctx context.Context, creds Credentials, req CreateResourceRequest) (Resource, error)
	DeleteResource(ctx context.Context, creds Credentials, resourceID string) error

	// Tags
	SyncTags(ctx context.Context, creds Credentials, resourceID string, tags map[string]string) error
}
