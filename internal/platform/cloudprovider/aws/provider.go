package aws

import (
	"context"
	"errors"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// AWSProvider implements types.CloudProvider for AWS.
type AWSProvider struct{}

func NewAWSProvider() *AWSProvider { return &AWSProvider{} }

func (p *AWSProvider) ProviderType() types.ProviderType { return types.ProviderTypeAWS }

func (p *AWSProvider) ValidateCredentials(ctx context.Context, creds types.Credentials) error {
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
			return errors.New("aws: AccessKeyID is required")
		}
		if creds.AccessKeySecret == "" {
			return errors.New("aws: SecretAccessKey is required")
		}
	case types.AuthTypeOIDC:
		if creds.OIDCToken == "" {
			return errors.New("aws: OIDCToken is required")
		}
	default:
		return errors.New("aws: unsupported auth type, expect aksk or oidc")
	}
	logx.Info(ctx, "aws credentials validated", "authType", creds.Type, "region", creds.Region)
	return nil
}

func (p *AWSProvider) ListRegions(ctx context.Context, creds types.Credentials) ([]types.Region, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return []types.Region{
		{ID: "us-east-1", Name: "US East (N. Virginia)", Endpoint: "ec2.us-east-1.amazonaws.com"},
		{ID: "us-east-2", Name: "US East (Ohio)", Endpoint: "ec2.us-east-2.amazonaws.com"},
		{ID: "us-west-1", Name: "US West (N. California)", Endpoint: "ec2.us-west-1.amazonaws.com"},
		{ID: "us-west-2", Name: "US West (Oregon)", Endpoint: "ec2.us-west-2.amazonaws.com"},
		{ID: "eu-west-1", Name: "Europe (Ireland)", Endpoint: "ec2.eu-west-1.amazonaws.com"},
		{ID: "eu-central-1", Name: "Europe (Frankfurt)", Endpoint: "ec2.eu-central-1.amazonaws.com"},
		{ID: "ap-southeast-1", Name: "Asia Pacific (Singapore)", Endpoint: "ec2.ap-southeast-1.amazonaws.com"},
		{ID: "ap-northeast-1", Name: "Asia Pacific (Tokyo)", Endpoint: "ec2.ap-northeast-1.amazonaws.com"},
		{ID: "ap-southeast-2", Name: "Asia Pacific (Sydney)", Endpoint: "ec2.ap-southeast-2.amazonaws.com"},
		{ID: "cn-north-1", Name: "China (Beijing)", Endpoint: "ec2.cn-north-1.amazonaws.com.cn"},
	}, nil
}

func (p *AWSProvider) DiscoverVPCs(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *AWSProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *AWSProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *AWSProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *AWSProvider) CreateResource(ctx context.Context, creds types.Credentials, req types.CreateResourceRequest) (types.Resource, error) {
	return types.Resource{}, types.ErrNotImplemented
}

func (p *AWSProvider) DeleteResource(ctx context.Context, creds types.Credentials, resourceID string) error {
	return types.ErrNotImplemented
}

func (p *AWSProvider) SyncTags(ctx context.Context, creds types.Credentials, resourceID string, tags map[string]string) error {
	return types.ErrNotImplemented
}
