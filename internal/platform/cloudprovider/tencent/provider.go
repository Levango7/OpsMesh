package tencent

import (
	"context"
	"errors"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

type TencentProvider struct{}

func NewTencentProvider() *TencentProvider { return &TencentProvider{} }

func (p *TencentProvider) ProviderType() types.ProviderType { return types.ProviderTypeTencent }

func (p *TencentProvider) ValidateCredentials(ctx context.Context, creds types.Credentials) error {
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
			return errors.New("tencent: SecretId (AccessKeyID) is required")
		}
		if creds.AccessKeySecret == "" {
			return errors.New("tencent: SecretKey (AccessKeySecret) is required")
		}
	case types.AuthTypeOIDC:
		if creds.OIDCToken == "" {
			return errors.New("tencent: OIDCToken is required")
		}
	default:
		return errors.New("tencent: unsupported auth type, expect aksk or oidc")
	}
	logx.Info(ctx, "tencent credentials validated", "authType", creds.Type, "region", creds.Region)
	return nil
}

func (p *TencentProvider) ListRegions(ctx context.Context, creds types.Credentials) ([]types.Region, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return []types.Region{
		{ID: "ap-guangzhou", Name: "广州", Endpoint: "cvm.ap-guangzhou.tencentcloudapi.com"},
		{ID: "ap-shanghai", Name: "上海", Endpoint: "cvm.ap-shanghai.tencentcloudapi.com"},
		{ID: "ap-beijing", Name: "北京", Endpoint: "cvm.ap-beijing.tencentcloudapi.com"},
		{ID: "ap-chengdu", Name: "成都", Endpoint: "cvm.ap-chengdu.tencentcloudapi.com"},
		{ID: "ap-chongqing", Name: "重庆", Endpoint: "cvm.ap-chongqing.tencentcloudapi.com"},
		{ID: "ap-hongkong", Name: "香港", Endpoint: "cvm.ap-hongkong.tencentcloudapi.com"},
		{ID: "ap-singapore", Name: "新加坡", Endpoint: "cvm.ap-singapore.tencentcloudapi.com"},
		{ID: "ap-tokyo", Name: "东京", Endpoint: "cvm.ap-tokyo.tencentcloudapi.com"},
		{ID: "na-siliconvalley", Name: "硅谷", Endpoint: "cvm.na-siliconvalley.tencentcloudapi.com"},
		{ID: "eu-frankfurt", Name: "法兰克福", Endpoint: "cvm.eu-frankfurt.tencentcloudapi.com"},
	}, nil
}

func (p *TencentProvider) DiscoverVPCs(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *TencentProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *TencentProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *TencentProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *TencentProvider) CreateResource(ctx context.Context, creds types.Credentials, req types.CreateResourceRequest) (types.Resource, error) {
	return types.Resource{}, types.ErrNotImplemented
}

func (p *TencentProvider) DeleteResource(ctx context.Context, creds types.Credentials, resourceID string) error {
	return types.ErrNotImplemented
}

func (p *TencentProvider) SyncTags(ctx context.Context, creds types.Credentials, resourceID string, tags map[string]string) error {
	return types.ErrNotImplemented
}
