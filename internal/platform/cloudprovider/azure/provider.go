package azure

import (
	"context"
	"errors"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

type AzureProvider struct{}

func NewAzureProvider() *AzureProvider { return &AzureProvider{} }

func (p *AzureProvider) ProviderType() types.ProviderType { return types.ProviderTypeAzure }

func (p *AzureProvider) ValidateCredentials(ctx context.Context, creds types.Credentials) error {
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
			return errors.New("azure: ClientID (AccessKeyID) is required")
		}
		if creds.AccessKeySecret == "" {
			return errors.New("azure: ClientSecret (AccessKeySecret) is required")
		}
	case types.AuthTypeOIDC, types.AuthTypeManagedIdentity:
		// OIDC / Managed Identity: token or role ARN validated at runtime
	default:
		return errors.New("azure: unsupported auth type, expect aksk/oidc/managed_identity")
	}
	logx.Info(ctx, "azure credentials validated", "authType", creds.Type, "region", creds.Region)
	return nil
}

func (p *AzureProvider) ListRegions(ctx context.Context, creds types.Credentials) ([]types.Region, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return []types.Region{
		{ID: "eastus", Name: "East US", Endpoint: "management.azure.com"},
		{ID: "eastus2", Name: "East US 2", Endpoint: "management.azure.com"},
		{ID: "westus", Name: "West US", Endpoint: "management.azure.com"},
		{ID: "westus2", Name: "West US 2", Endpoint: "management.azure.com"},
		{ID: "centralus", Name: "Central US", Endpoint: "management.azure.com"},
		{ID: "northeurope", Name: "North Europe", Endpoint: "management.azure.com"},
		{ID: "westeurope", Name: "West Europe", Endpoint: "management.azure.com"},
		{ID: "southeastasia", Name: "Southeast Asia", Endpoint: "management.azure.com"},
		{ID: "eastasia", Name: "East Asia", Endpoint: "management.azure.com"},
		{ID: "japaneast", Name: "Japan East", Endpoint: "management.azure.com"},
	}, nil
}

func (p *AzureProvider) DiscoverVPCs(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *AzureProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *AzureProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *AzureProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	return []types.Resource{}, nil
}

func (p *AzureProvider) CreateResource(ctx context.Context, creds types.Credentials, req types.CreateResourceRequest) (types.Resource, error) {
	return types.Resource{}, types.ErrNotImplemented
}

func (p *AzureProvider) DeleteResource(ctx context.Context, creds types.Credentials, resourceID string) error {
	return types.ErrNotImplemented
}

func (p *AzureProvider) SyncTags(ctx context.Context, creds types.Credentials, resourceID string, tags map[string]string) error {
	return types.ErrNotImplemented
}
