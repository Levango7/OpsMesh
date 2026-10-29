// Package volcengine 提供火山引擎（Volcengine）云厂商适配器实现。
//
// 基于原生 HTTP 调用（HMAC-SHA256 签名）实现，不依赖火山引擎官方 SDK：
// - ValidateCredentials：本地格式校验（AK/SK 或 OIDC）
// - ListRegions：返回内置区域列表
// - DiscoverVPCs：调用 VPC DescribeVpcs
// - DiscoverSubnets：调用 VPC DescribeSubnets
// - DiscoverInstances：调用 ECS DescribeInstances
// - DiscoverClusters：调用 VKE DescribeClusters
// - CreateResource/DeleteResource/SyncTags：返回 ErrNotImplemented
package volcengine

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// volcengineClient wraps HTTP calls to Volcengine OpenAPI.
type volcengineClient struct {
	accessKeyID     string
	secretAccessKey string
	http            *http.Client
}

func newVolcengineClient(creds types.Credentials) *volcengineClient {
	return &volcengineClient{
		accessKeyID:     creds.AccessKeyID,
		secretAccessKey: creds.AccessKeySecret,
		http:            &http.Client{Timeout: 30 * time.Second},
	}
}

// call makes a signed POST request to Volcengine OpenAPI.
func (c *volcengineClient) call(ctx context.Context, service, action, version, region string, body map[string]interface{}) (map[string]interface{}, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if body == nil {
		body = make(map[string]interface{})
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("volcengine: marshal body: %w", err)
	}
	if region == "" {
		region = "cn-beijing"
	}

	host := "open.volcengineapi.com"

	values := url.Values{}
	values.Set("Action", action)
	values.Set("Version", version)
	queryString := values.Encode()

	timestamp := time.Now().UTC()
	xDate := timestamp.Format("20060102T150405Z")
	dateStamp := timestamp.Format("20060102")

	authorization, err := volcengineAuthorization(c.accessKeyID, c.secretAccessKey, host, service, region, dateStamp, xDate, queryString, bodyBytes)
	if err != nil {
		return nil, err
	}

	reqURL := "https://" + host + "/?" + queryString
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("volcengine: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", host)
	req.Header.Set("X-Date", xDate)
	req.Header.Set("Authorization", authorization)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("volcengine: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("volcengine: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("volcengine: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("volcengine: parse JSON: %w", err)
	}

	// Volcengine error envelope.
	if rm, ok := result["ResponseMetadata"].(map[string]interface{}); ok {
		if errObj, ok := rm["Error"].(map[string]interface{}); ok {
			code := getString(errObj, "Code", "code")
			msg := getString(errObj, "Message", "message")
			if code != "" || msg != "" {
				return nil, fmt.Errorf("volcengine: API error %s: %s", code, msg)
			}
		}
	}

	return result, nil
}

func volcengineAuthorization(accessKeyID, secretAccessKey, host, service, region, dateStamp, xDate, queryString string, body []byte) (string, error) {
	payloadHash := sha256Hex(body)

	canonicalHeaders := "content-type:application/json\n" + "host:" + host + "\n" + "x-date:" + xDate + "\n"
	signedHeaders := "content-type;host;x-date"

	canonicalRequest := "POST\n/\n" + queryString + "\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash

	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + dateStamp + "/" + region + "/" + service + "/request\n" + sha256Hex([]byte(canonicalRequest))

	kDate := hmacSHA256([]byte(secretAccessKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	return fmt.Sprintf("HMAC-SHA256 Credential=%s/%s/%s/%s/request, SignedHeaders=%s, Signature=%s",
		accessKeyID, dateStamp, region, service, signedHeaders, signature), nil
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

// unwrapResult extracts the "Result" object when present.
func unwrapResult(result map[string]interface{}) map[string]interface{} {
	if r, ok := result["Result"].(map[string]interface{}); ok {
		return r
	}
	return result
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func findSlice(m map[string]interface{}, keys ...string) []interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.([]interface{}); ok {
				return s
			}
			if inner, ok := v.(map[string]interface{}); ok {
				for _, ik := range []string{"item", "Item", "member", "members", "list", "List"} {
					if s, ok := inner[ik].([]interface{}); ok {
						return s
					}
				}
			}
		}
	}
	return nil
}

// VolcengineProvider implements types.CloudProvider for Volcengine.
type VolcengineProvider struct{}

func NewVolcengineProvider() *VolcengineProvider { return &VolcengineProvider{} }

func (p *VolcengineProvider) ProviderType() types.ProviderType { return types.ProviderTypeVolcengine }

func (p *VolcengineProvider) ValidateCredentials(ctx context.Context, creds types.Credentials) error {
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
			return fmt.Errorf("volcengine: AccessKeyID is required")
		}
		if creds.AccessKeySecret == "" {
			return fmt.Errorf("volcengine: SecretAccessKey is required")
		}
	case types.AuthTypeOIDC:
		if creds.OIDCToken == "" {
			return fmt.Errorf("volcengine: OIDCToken is required")
		}
	default:
		return fmt.Errorf("volcengine: unsupported auth type, expect aksk or oidc")
	}
	logx.Info(ctx, "volcengine credentials validated", "authType", creds.Type, "region", creds.Region)
	return nil
}

func (p *VolcengineProvider) ListRegions(ctx context.Context, creds types.Credentials) ([]types.Region, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return []types.Region{
		{ID: "cn-beijing", Name: "华北2（北京）", Endpoint: "open.volcengineapi.com"},
		{ID: "cn-shanghai", Name: "华东2（上海）", Endpoint: "open.volcengineapi.com"},
		{ID: "cn-guangzhou", Name: "华南1（广州）", Endpoint: "open.volcengineapi.com"},
		{ID: "cn-hongkong", Name: "中国香港", Endpoint: "open.volcengineapi.com"},
		{ID: "ap-singapore", Name: "新加坡", Endpoint: "open.volcengineapi.com"},
		{ID: "us-east-1", Name: "美国东部（弗吉尼亚）", Endpoint: "open.volcengineapi.com"},
	}, nil
}

func (p *VolcengineProvider) DiscoverVPCs(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
	if region == "" {
		region = "cn-beijing"
	}

	client := newVolcengineClient(creds)
	result, err := client.call(ctx, "vpc", "DescribeVpcs", "2020-04-01", region, map[string]interface{}{
		"PageNumber": 1,
		"PageSize":   100,
	})
	if err != nil {
		logx.Warn(ctx, "volcengine DiscoverVPCs failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	data := unwrapResult(result)
	var vpcArr []interface{}
	if s := findSlice(data, "Vpcs", "VpcSet", "vpcs", "vpcSet"); s != nil {
		vpcArr = s
	}
	for _, v := range vpcArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeVolcengine,
			Region:       region,
			ResourceType: "vpc",
		}
		id := getString(vm, "VpcId", "VpcID", "Id", "id")
		r.ResourceID = id
		r.ID = id
		if name := getString(vm, "VpcName", "Name", "name"); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = id
		}
		r.Status = getString(vm, "Status", "status", "State", "state")
		resources = append(resources, r)
	}
	logx.Info(ctx, "volcengine VPCs discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *VolcengineProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
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
	if region == "" {
		region = "cn-beijing"
	}

	body := map[string]interface{}{
		"PageNumber": 1,
		"PageSize":   100,
	}
	if vpcID != "" {
		body["VpcId"] = vpcID
	}

	client := newVolcengineClient(creds)
	result, err := client.call(ctx, "vpc", "DescribeSubnets", "2020-04-01", region, body)
	if err != nil {
		logx.Warn(ctx, "volcengine DiscoverSubnets failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	data := unwrapResult(result)
	var subnetArr []interface{}
	if s := findSlice(data, "Subnets", "SubnetSet", "subnets", "subnetSet"); s != nil {
		subnetArr = s
	}
	for _, v := range subnetArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeVolcengine,
			Region:       region,
			ResourceType: "subnet",
		}
		id := getString(vm, "SubnetId", "SubnetID", "Id", "id")
		r.ResourceID = id
		r.ID = id
		if name := getString(vm, "SubnetName", "Name", "name"); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = id
		}
		r.Status = getString(vm, "Status", "status", "State", "state")
		resources = append(resources, r)
	}
	logx.Info(ctx, "volcengine subnets discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *VolcengineProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
	if region == "" {
		region = "cn-beijing"
	}

	client := newVolcengineClient(creds)
	result, err := client.call(ctx, "ecs", "DescribeInstances", "2020-06-01", region, map[string]interface{}{
		"PageNumber": 1,
		"PageSize":   100,
	})
	if err != nil {
		logx.Warn(ctx, "volcengine DiscoverInstances failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	data := unwrapResult(result)
	var instArr []interface{}
	if s := findSlice(data, "Instances", "InstanceSet", "instances", "instanceSet"); s != nil {
		instArr = s
	}
	for _, v := range instArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeVolcengine,
			Region:       region,
			ResourceType: "instance",
		}
		id := getString(vm, "InstanceId", "InstanceID", "Id", "id")
		r.ResourceID = id
		r.ID = id
		if name := getString(vm, "InstanceName", "Name", "name"); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = id
		}
		r.Status = getString(vm, "Status", "status", "InstanceState", "instanceState")
		r.PrivateIP = getString(vm, "PrimaryIpAddress", "PrivateIpAddress", "privateIpAddress", "PrivateIp")
		r.PublicIP = getString(vm, "PublicIpAddress", "publicIpAddress", "PublicIp")
		resources = append(resources, r)
	}
	logx.Info(ctx, "volcengine instances discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *VolcengineProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
	if region == "" {
		region = "cn-beijing"
	}

	client := newVolcengineClient(creds)
	result, err := client.call(ctx, "vke", "DescribeClusters", "2022-05-12", region, map[string]interface{}{
		"PageNumber": 1,
		"PageSize":   100,
	})
	if err != nil {
		logx.Warn(ctx, "volcengine DiscoverClusters failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	data := unwrapResult(result)
	var clusterArr []interface{}
	if s := findSlice(data, "Clusters", "ClusterSet", "clusters", "clusterSet"); s != nil {
		clusterArr = s
	}
	for _, v := range clusterArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeVolcengine,
			Region:       region,
			ResourceType: "cluster",
		}
		for _, idKey := range []string{"Id", "ID", "ClusterId", "ClusterID", "id"} {
			if id, ok := vm[idKey].(string); ok && id != "" {
				r.ResourceID = id
				r.ID = id
				break
			}
		}
		for _, nameKey := range []string{"Name", "ClusterName", "name", "clusterName"} {
			if name, ok := vm[nameKey].(string); ok && name != "" {
				r.ResourceName = name
				break
			}
		}
		if r.ResourceName == "" {
			r.ResourceName = r.ResourceID
		}
		for _, statusKey := range []string{"Status", "ClusterStatus", "status", "clusterStatus"} {
			if status, ok := vm[statusKey].(string); ok && status != "" {
				r.Status = status
				break
			}
		}
		resources = append(resources, r)
	}
	logx.Info(ctx, "volcengine clusters discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *VolcengineProvider) CreateResource(ctx context.Context, creds types.Credentials, req types.CreateResourceRequest) (types.Resource, error) {
	return types.Resource{}, types.ErrNotImplemented
}

func (p *VolcengineProvider) DeleteResource(ctx context.Context, creds types.Credentials, resourceID string) error {
	return types.ErrNotImplemented
}

func (p *VolcengineProvider) SyncTags(ctx context.Context, creds types.Credentials, resourceID string, tags map[string]string) error {
	return types.ErrNotImplemented
}
