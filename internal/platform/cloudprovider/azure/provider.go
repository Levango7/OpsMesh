// Package azure 提供 Azure 云厂商适配器实现。
//
// 基于原生 HTTP 调用实现，不依赖 Azure 官方 SDK：
// - ValidateCredentials：本地格式校验（AK/SK、OIDC、Managed Identity）
// - ListRegions：返回内置区域列表
// - DiscoverVPCs：调用 Azure Resource Manager Microsoft.Network/virtualNetworks
// - DiscoverSubnets：调用 Azure Resource Manager Microsoft.Network/virtualNetworks/{vnet}/subnets
// - DiscoverInstances：调用 Azure Resource Manager Microsoft.Compute/virtualMachines
// - DiscoverClusters：调用 Azure Resource Manager Microsoft.ContainerService/managedClusters
// - CreateResource/DeleteResource/SyncTags：返回 ErrNotImplemented
//
// 认证使用 Azure AD OAuth2 client credentials flow 获取 Bearer token。
package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// 可被测试覆盖的端点。
var (
	azureTokenURL = "https://login.microsoftonline.com/%s/oauth2/v2.0/token"
	azureARMBase  = "https://management.azure.com"
)

// azureClient wraps HTTP calls to Azure Resource Manager.
type azureClient struct {
	clientID     string
	clientSecret string
	tenantID     string
	subscription string
	http         *http.Client
}

func newAzureClient(creds types.Credentials) *azureClient {
	return &azureClient{
		clientID:     creds.AccessKeyID,
		clientSecret: creds.AccessKeySecret,
		tenantID:     creds.Region,
		subscription: creds.Region,
		http:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *azureClient) getToken(ctx context.Context) (string, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	if c.tenantID == "" {
		return "", fmt.Errorf("azure: tenant ID (region) is required")
	}
	if c.clientID == "" {
		return "", fmt.Errorf("azure: client ID is required")
	}
	if c.clientSecret == "" {
		return "", fmt.Errorf("azure: client secret is required")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("scope", "https://management.azure.com/.default")

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf(azureTokenURL, c.tenantID), strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("azure: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("azure: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("azure: read token body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("azure: token HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("azure: parse token JSON: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("azure: empty access token")
	}
	return result.AccessToken, nil
}

func (c *azureClient) get(ctx context.Context, path string, query url.Values) (map[string]interface{}, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	u := azureARMBase + path
	if query != nil {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("azure: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("azure: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("azure: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("azure: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	if len(respBody) == 0 {
		return map[string]interface{}{}, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("azure: parse JSON: %w", err)
	}
	return result, nil
}

func azureString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// AzureProvider implements types.CloudProvider for Microsoft Azure.
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
			return fmt.Errorf("azure: ClientID (AccessKeyID) is required")
		}
		if creds.AccessKeySecret == "" {
			return fmt.Errorf("azure: ClientSecret (AccessKeySecret) is required")
		}
	case types.AuthTypeOIDC, types.AuthTypeManagedIdentity:
		// OIDC / Managed Identity: token or role ARN validated at runtime
	default:
		return fmt.Errorf("azure: unsupported auth type, expect aksk/oidc/managed_identity")
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
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if creds.Type != types.AuthTypeAKSK {
		return []types.Resource{}, nil
	}
	if region == "" {
		region = creds.Region
	}

	client := newAzureClient(creds)
	query := url.Values{}
	query.Set("api-version", "2023-09-01")
	path := "/subscriptions/" + client.subscription + "/providers/Microsoft.Network/virtualNetworks"

	result, err := client.get(ctx, path, query)
	if err != nil {
		logx.Warn(ctx, "azure DiscoverVPCs failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	if arr, ok := result["value"].([]interface{}); ok {
		for _, v := range arr {
			if vm, ok := v.(map[string]interface{}); ok {
				r := types.Resource{
					Provider:     types.ProviderTypeAzure,
					Region:       region,
					ResourceType: "vpc",
				}
				if r.Region == "" {
					r.Region = azureString(vm, "location")
				}
				id := azureString(vm, "id")
				r.ResourceID = id
				r.ID = id
				r.ResourceName = azureString(vm, "name")
				if props, ok := vm["properties"].(map[string]interface{}); ok {
					r.Status = azureString(props, "provisioningState")
				}
				resources = append(resources, r)
			}
		}
	}
	logx.Info(ctx, "azure VPCs discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *AzureProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if creds.Type != types.AuthTypeAKSK {
		return []types.Resource{}, nil
	}
	if region == "" {
		region = creds.Region
	}
	if vpcID == "" {
		return []types.Resource{}, nil
	}

	vnet := vpcID
	if idx := strings.LastIndex(vnet, "/"); idx >= 0 {
		vnet = vnet[idx+1:]
	}

	client := newAzureClient(creds)
	query := url.Values{}
	query.Set("api-version", "2023-09-01")
	path := "/subscriptions/" + client.subscription + "/providers/Microsoft.Network/virtualNetworks/" + vnet + "/subnets"

	result, err := client.get(ctx, path, query)
	if err != nil {
		logx.Warn(ctx, "azure DiscoverSubnets failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	if arr, ok := result["value"].([]interface{}); ok {
		for _, v := range arr {
			if vm, ok := v.(map[string]interface{}); ok {
				r := types.Resource{
					Provider:     types.ProviderTypeAzure,
					Region:       region,
					ResourceType: "subnet",
				}
				if r.Region == "" {
					r.Region = azureString(vm, "location")
				}
				id := azureString(vm, "id")
				r.ResourceID = id
				r.ID = id
				r.ResourceName = azureString(vm, "name")
				if props, ok := vm["properties"].(map[string]interface{}); ok {
					r.Status = azureString(props, "provisioningState")
				}
				resources = append(resources, r)
			}
		}
	}
	logx.Info(ctx, "azure subnets discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *AzureProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if creds.Type != types.AuthTypeAKSK {
		return []types.Resource{}, nil
	}
	if region == "" {
		region = creds.Region
	}

	client := newAzureClient(creds)
	query := url.Values{}
	query.Set("api-version", "2023-09-01")
	path := "/subscriptions/" + client.subscription + "/providers/Microsoft.Compute/virtualMachines"

	result, err := client.get(ctx, path, query)
	if err != nil {
		logx.Warn(ctx, "azure DiscoverInstances failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	if arr, ok := result["value"].([]interface{}); ok {
		for _, v := range arr {
			if vm, ok := v.(map[string]interface{}); ok {
				r := types.Resource{
					Provider:     types.ProviderTypeAzure,
					Region:       region,
					ResourceType: "instance",
				}
				if r.Region == "" {
					r.Region = azureString(vm, "location")
				}
				id := azureString(vm, "id")
				r.ResourceID = id
				r.ID = id
				r.ResourceName = azureString(vm, "name")
				if props, ok := vm["properties"].(map[string]interface{}); ok {
					r.Status = azureString(props, "provisioningState")
				}
				resources = append(resources, r)
			}
		}
	}
	logx.Info(ctx, "azure instances discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *AzureProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if creds.Type != types.AuthTypeAKSK {
		return []types.Resource{}, nil
	}
	if region == "" {
		region = creds.Region
	}

	client := newAzureClient(creds)
	query := url.Values{}
	query.Set("api-version", "2023-09-01")
	path := "/subscriptions/" + client.subscription + "/providers/Microsoft.ContainerService/managedClusters"

	result, err := client.get(ctx, path, query)
	if err != nil {
		logx.Warn(ctx, "azure DiscoverClusters failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	if arr, ok := result["value"].([]interface{}); ok {
		for _, v := range arr {
			if vm, ok := v.(map[string]interface{}); ok {
				r := types.Resource{
					Provider:     types.ProviderTypeAzure,
					Region:       region,
					ResourceType: "cluster",
				}
				if r.Region == "" {
					r.Region = azureString(vm, "location")
				}
				id := azureString(vm, "id")
				r.ResourceID = id
				r.ID = id
				r.ResourceName = azureString(vm, "name")
				if props, ok := vm["properties"].(map[string]interface{}); ok {
					r.Status = azureString(props, "provisioningState")
				}
				resources = append(resources, r)
			}
		}
	}
	logx.Info(ctx, "azure clusters discovered", "count", len(resources), "region", region)
	return resources, nil
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
