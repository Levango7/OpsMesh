// Package huawei 提供华为云（Huawei Cloud）云厂商适配器实现。
//
// 基于原生 HTTP 调用（HMAC-SHA256 / SDK-HMAC-SHA256 签名）实现，不依赖华为云官方 SDK。
package huawei

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// huaweiClient wraps HTTP calls to Huawei Cloud APIs.
type huaweiClient struct {
	accessKeyID string
	secretKey   string
	http        *http.Client
}

func newHuaweiClient(creds types.Credentials) *huaweiClient {
	return &huaweiClient{
		accessKeyID: creds.AccessKeyID,
		secretKey:   creds.AccessKeySecret,
		http:        &http.Client{Timeout: 30 * time.Second},
	}
}

// get performs a signed GET request and parses a JSON response.
func (c *huaweiClient) get(ctx context.Context, host, path string) (map[string]interface{}, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	reqURL := "https://" + host + path
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("huawei: create request: %w", err)
	}

	xSdkDate := time.Now().UTC().Format("20060102T150405Z")
	payloadHash := sha256Hex([]byte(""))

	signedHeaders := "host;x-sdk-date"
	canonicalHeaders := "host:" + host + "\n" + "x-sdk-date:" + xSdkDate + "\n"

	canonicalRequest := "GET\n" + path + "\n" + "\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash
	stringToSign := "SDK-HMAC-SHA256\n" + xSdkDate + "\n" + sha256Hex([]byte(canonicalRequest))

	signature := hex.EncodeToString(hmacSHA256([]byte(c.secretKey), []byte(stringToSign)))
	authorization := fmt.Sprintf("SDK-HMAC-SHA256 Access=%s, SignedHeaders=%s, Signature=%s",
		c.accessKeyID, signedHeaders, signature)

	req.Header.Set("Host", host)
	req.Header.Set("X-Sdk-Date", xSdkDate)
	req.Header.Set("Authorization", authorization)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huawei: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("huawei: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("huawei: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if len(respBody) == 0 {
		return map[string]interface{}{}, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("huawei: parse JSON: %w", err)
	}
	return result, nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch val := v.(type) {
			case string:
				if val != "" {
					return val
				}
			case float64:
				return fmt.Sprintf("%v", val)
			}
		}
	}
	return ""
}

func findSlice(m map[string]interface{}, keys ...string) []interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch val := v.(type) {
			case []interface{}:
				return val
			case map[string]interface{}:
				for _, ik := range []string{"item", "Item", "member", "members", "servers", "vpcs", "subnets", "clusters", "items"} {
					if s, ok := val[ik].([]interface{}); ok {
						return s
					}
				}
			}
		}
	}
	return nil
}

// HuaweiProvider implements types.CloudProvider for Huawei Cloud.
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
			return fmt.Errorf("huawei: AccessKey (AccessKeyID) is required")
		}
		if creds.AccessKeySecret == "" {
			return fmt.Errorf("huawei: SecretKey (AccessKeySecret) is required")
		}
	case types.AuthTypeOIDC:
		if creds.OIDCToken == "" {
			return fmt.Errorf("huawei: OIDCToken is required")
		}
	default:
		return fmt.Errorf("huawei: unsupported auth type, expect aksk or oidc")
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

func huaweiRegion(creds types.Credentials, region string) string {
	if region == "" {
		region = creds.Region
	}
	if region == "" {
		region = "cn-north-4"
	}
	return region
}

func huaweiProjectID(creds types.Credentials, region string) string {
	if creds.Region != "" {
		return creds.Region
	}
	if region != "" {
		return region
	}
	return "cn-north-4"
}

func (p *HuaweiProvider) DiscoverVPCs(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
	region = huaweiRegion(creds, region)
	projectID := huaweiProjectID(creds, region)

	client := newHuaweiClient(creds)
	host := "vpc." + region + ".myhuaweicloud.com"
	path := "/v1/" + projectID + "/vpcs"

	result, err := client.get(ctx, host, path)
	if err != nil {
		logx.Warn(ctx, "huawei DiscoverVPCs failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	var vpcArr []interface{}
	if s := findSlice(result, "vpcs"); s != nil {
		vpcArr = s
	}
	for _, v := range vpcArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeHuawei,
			Region:       region,
			ResourceType: "vpc",
		}
		id := getString(vm, "id", "ID", "vpc_id", "vpcId")
		r.ResourceID = id
		r.ID = id
		if name := getString(vm, "name", "Name", "vpc_name"); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = id
		}
		r.Status = getString(vm, "status", "Status", "state")
		resources = append(resources, r)
	}
	logx.Info(ctx, "huawei VPCs discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *HuaweiProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
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
	region = huaweiRegion(creds, region)
	projectID := huaweiProjectID(creds, region)

	client := newHuaweiClient(creds)
	host := "vpc." + region + ".myhuaweicloud.com"
	path := "/v1/" + projectID + "/subnets"

	result, err := client.get(ctx, host, path)
	if err != nil {
		logx.Warn(ctx, "huawei DiscoverSubnets failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	var subnetArr []interface{}
	if s := findSlice(result, "subnets"); s != nil {
		subnetArr = s
	}
	for _, v := range subnetArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if vpcID != "" {
			if vid := getString(vm, "vpc_id", "vpcId", "VpcId"); vid != "" && vid != vpcID {
				continue
			}
		}
		r := types.Resource{
			Provider:     types.ProviderTypeHuawei,
			Region:       region,
			ResourceType: "subnet",
		}
		id := getString(vm, "id", "ID", "subnet_id", "subnetId")
		r.ResourceID = id
		r.ID = id
		if name := getString(vm, "name", "Name", "subnet_name"); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = id
		}
		r.Status = getString(vm, "status", "Status", "state")
		resources = append(resources, r)
	}
	logx.Info(ctx, "huawei subnets discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *HuaweiProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
	region = huaweiRegion(creds, region)
	projectID := huaweiProjectID(creds, region)

	client := newHuaweiClient(creds)
	host := "ecs." + region + ".myhuaweicloud.com"
	path := "/v2/" + projectID + "/servers/detail"

	result, err := client.get(ctx, host, path)
	if err != nil {
		logx.Warn(ctx, "huawei DiscoverInstances failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	var instArr []interface{}
	if s := findSlice(result, "servers"); s != nil {
		instArr = s
	}
	for _, v := range instArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeHuawei,
			Region:       region,
			ResourceType: "instance",
		}
		id := getString(vm, "id", "ID", "server_id", "serverId")
		r.ResourceID = id
		r.ID = id
		if name := getString(vm, "name", "Name", "server_name"); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = id
		}
		r.Status = getString(vm, "status", "Status")
		if addrMap, ok := vm["addresses"].(map[string]interface{}); ok {
			for _, v := range addrMap {
				if arr, ok := v.([]interface{}); ok {
					for _, a := range arr {
						if am, ok := a.(map[string]interface{}); ok {
							if ip := getString(am, "addr"); ip != "" {
								if t, ok := am["OS-EXT-IPS:type"].(string); ok && t == "floating" {
									if r.PublicIP == "" {
										r.PublicIP = ip
									}
								} else {
									if r.PrivateIP == "" {
										r.PrivateIP = ip
									}
								}
							}
						}
					}
				}
			}
		}
		resources = append(resources, r)
	}
	logx.Info(ctx, "huawei instances discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *HuaweiProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
	region = huaweiRegion(creds, region)
	projectID := huaweiProjectID(creds, region)

	client := newHuaweiClient(creds)
	host := "cce." + region + ".myhuaweicloud.com"
	path := "/api/v3/projects/" + projectID + "/clusters"

	result, err := client.get(ctx, host, path)
	if err != nil {
		logx.Warn(ctx, "huawei DiscoverClusters failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	var clusterArr []interface{}
	if s := findSlice(result, "items", "clusters"); s != nil {
		clusterArr = s
	}
	if len(clusterArr) == 0 {
		if body, ok := result["body"].(map[string]interface{}); ok {
			if s := findSlice(body, "items", "clusters"); s != nil {
				clusterArr = s
			}
		}
	}

	for _, v := range clusterArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeHuawei,
			Region:       region,
			ResourceType: "cluster",
		}
		meta := vm
		if m, ok := vm["metadata"].(map[string]interface{}); ok {
			meta = m
		}
		id := getString(meta, "uid", "id", "ID", "cluster_id", "clusterId")
		if id == "" {
			id = getString(vm, "uid", "id", "ID", "cluster_id", "clusterId")
		}
		r.ResourceID = id
		r.ID = id
		name := getString(meta, "name", "Name", "cluster_name")
		if name == "" {
			name = getString(vm, "name", "Name", "cluster_name")
		}
		if name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = id
		}
		status := ""
		if st, ok := vm["status"].(map[string]interface{}); ok {
			status = getString(st, "phase", "status", "state")
		}
		if status == "" {
			status = getString(vm, "status", "phase", "state")
		}
		r.Status = status
		resources = append(resources, r)
	}
	logx.Info(ctx, "huawei clusters discovered", "count", len(resources), "region", region)
	return resources, nil
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
