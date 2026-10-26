// Package cloudprovider 提供多云厂商适配器抽象与内建注册。
//
// CloudProvider 是所有云厂商适配器的统一抽象接口；AdapterFactory 按
// ProviderType 注册/查找适配器；registry.go 注册内建厂商适配器（aliyun
// 已接入真实实现，其余为 stub，均返回 "not implemented" 错误，待后续
// 波次实现具体 SDK 调用）。
//
// Credentials 为运行时临时持有者：AK/SK 等密钥由调用方在调用时从秘密
// 存储（internal/secrets）解析后填入，本包不持久化明文凭据。
package cloudprovider

import (
	"context"

	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// 向后兼容别名：历史代码直接引用 cloudprovider.ProviderType 等名字，
// 迁移后统一指向 types 包，外部零感知。
type (
	ProviderType          = types.ProviderType
	AuthType              = types.AuthType
	Credentials           = types.Credentials
	Resource              = types.Resource
	Region                = types.Region
	CreateResourceRequest = types.CreateResourceRequest
)

const (
	ProviderTypeAliyun  = types.ProviderTypeAliyun
	ProviderTypeAWS     = types.ProviderTypeAWS
	ProviderTypeAzure   = types.ProviderTypeAzure
	ProviderTypeTencent = types.ProviderTypeTencent
	ProviderTypeHuawei  = types.ProviderTypeHuawei
)

const (
	AuthTypeAKSK            = types.AuthTypeAKSK
	AuthTypeOIDC            = types.AuthTypeOIDC
	AuthTypeManagedIdentity = types.AuthTypeManagedIdentity
)

// ErrNotImplemented 表示该能力尚未由具体厂商适配器实现。
var ErrNotImplemented = types.ErrNotImplemented

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
