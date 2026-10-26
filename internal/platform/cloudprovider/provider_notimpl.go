package cloudprovider

import (
	"context"
)

// NotImplementedProvider 是所有内建厂商 stub 的基础实现。
// 所有能力方法均返回 ErrNotImplemented，供后续波次按厂商 SDK 逐个填充。
type NotImplementedProvider struct{}

func (p *NotImplementedProvider) ProviderType() ProviderType {
	return ""
}

func (p *NotImplementedProvider) ValidateCredentials(ctx context.Context, creds Credentials) error {
	return ErrNotImplemented
}

func (p *NotImplementedProvider) ListRegions(ctx context.Context, creds Credentials) ([]Region, error) {
	return nil, ErrNotImplemented
}

func (p *NotImplementedProvider) DiscoverVPCs(ctx context.Context, creds Credentials, region string) ([]Resource, error) {
	return nil, ErrNotImplemented
}

func (p *NotImplementedProvider) DiscoverSubnets(ctx context.Context, creds Credentials, region string, vpcID string) ([]Resource, error) {
	return nil, ErrNotImplemented
}

func (p *NotImplementedProvider) DiscoverInstances(ctx context.Context, creds Credentials, region string) ([]Resource, error) {
	return nil, ErrNotImplemented
}

func (p *NotImplementedProvider) DiscoverClusters(ctx context.Context, creds Credentials, region string) ([]Resource, error) {
	return nil, ErrNotImplemented
}

func (p *NotImplementedProvider) CreateResource(ctx context.Context, creds Credentials, req CreateResourceRequest) (Resource, error) {
	return Resource{}, ErrNotImplemented
}

func (p *NotImplementedProvider) DeleteResource(ctx context.Context, creds Credentials, resourceID string) error {
	return ErrNotImplemented
}

func (p *NotImplementedProvider) SyncTags(ctx context.Context, creds Credentials, resourceID string, tags map[string]string) error {
	return ErrNotImplemented
}
