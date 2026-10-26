package huawei

import (
	"context"
	"errors"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

type HuaweiProvider struct{}

func NewHuaweiProvider() *HuaweiProvider { return &HuaweiProvider{} }

func (p *HuaweiProvider) ProviderType() types.ProviderType { return types.ProviderTypeHuawei }

func (p *HuaweiProvider) ValidateCredentials(ctx context.Context, creds types.Credentials) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	switch creds.Type {
	case types.AuthTypeAKSK:
		if creds.AccessKeyID == "" {
			return errors.New("huawei: AccessKey (AccessKeyID) is required")
		}
		if creds.AccessKeySecret == "" {
			return errors.New("huawei: SecretKey (AccessKeySecret) is required")
		}
	case types.AuthTypeOIDC:
		if creds.OIDCToken == "" {
			return errors.New("huawei: OIDCToken is required")
		}
	default:
		return errors.New("huawei: unsupported auth type, expect aksk or oidc")
	}
	logx.Info(ctx, "huawei credentials validated", "authType", creds.Type, "region", creds.Region)
	return nil
}

func (p *HuaweiProvider) ListRegions(ctx context.Context, creds types.Credentials) ([]types.Region, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return []types.Region{
		{ID: "cn-north-1", Name: "华北-北京一", Endpoint: "ecs.cn-north-1.myhuaweicloud.com"},
		{ID: "cn-north-4", Name: "华北-北京四", Endpoint: "ecs.cn-north-4.myhuaweicloud.com"},
		{ID: "cn-east-2", Name: "华东-上海二", Endpoint: "ecs.cn-east-2.myhuaweicloud.com"},
		{ID: "cn-east-3", Name: "华东-上海一", Endpoint: "ecs.cn-east-3.myhuaweicloud.com"},
		{ID: "cn-south-1", Name: "华南-广州", Endpoint: "ecs.cn-south-1.myhuaweicloud.com"},
		{ID: "cn-southwest-2", Name: "西南-贵阳一", Endpoint: "ecs.cn-southwest-2.myhuaweicloud.com"},
		{ID: "ap-southeast-2", Name: "亚太-曼谷", Endpoint: "ecs.ap-southeast-2.myhuaweicloud.com"},
		{ID: "ap-southeast-3", Name: "亚太-新加坡", Endpoint: "ecs.ap-southeast-3.myhuaweicloud.com"},
		{ID: "eu-west-101", Name: "欧洲-巴黎", Endpoint: "ecs.eu-west-101.myhuaweicloud.com"},
		{ID: "af-south-1", Name: "非洲-约翰内斯堡", Endpoint: "ecs.af-south-1.myhuaweicloud.com"},
	}, nil
}

func (p *HuaweiProvider) DiscoverVPCs(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *HuaweiProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *HuaweiProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *HuaweiProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *HuaweiProvider) CreateResource(ctx context.Context, creds types.Credentials, req types.CreateResourceRequest) (types.Resource, error) {
	return types.Resource{}, types.ErrNotImplemented
}

func (p *HuaweiProvider) DeleteResource(ctx context.Context, creds types.Credentials, resourceID string) error {
	return types.ErrNotImplemented
}

func (p *HuaweiProvider) SyncTags(ctx context.Context, creds types.Credentials, resourceID string, tags map[string]string) error {
	return types.ErrNotImplemented
}
