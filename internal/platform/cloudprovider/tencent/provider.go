// Package tencent 提供腾讯云（Tencent Cloud）云厂商适配器实现。
//
// 基于原生 HTTP 调用（TC3-HMAC-SHA256 签名，API v3）实现，不依赖腾讯云官方 SDK。
package tencent

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
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// tencentClient wraps HTTP calls to Tencent Cloud API v3.
type tencentClient struct {
	secretID  string
	secretKey string
	http      *http.Client
}

func newTencentClient(creds types.Credentials) *tencentClient {
	return &tencentClient{
		secretID:  creds.AccessKeyID,
		secretKey: creds.AccessKeySecret,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// call makes a signed POST request to a Tencent Cloud API v3 endpoint.
func (c *tencentClient) call(ctx context.Context, host, service, action, version, region string, body map[string]interface{}) (map[string]interface{}, error) {
	if body == nil {
		body = make(map[string]interface{})
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("tencent: marshal body: %w", err)
	}

	if region == "" {
		region = "ap-guangzhou"
	}

	timestamp := time.Now().Unix()

	authorization := tc3Authorization(c.secretID, c.secretKey, host, service, timestamp, bodyBytes)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://"+host, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("tencent: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", host)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", version)
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-TC-Region", region)
	req.Header.Set("Authorization", authorization)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tencent: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tencent: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tencent: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("tencent: parse JSON: %w", err)
	}

	if respObj, ok := result["Response"].(map[string]interface{}); ok {
		if errObj, ok := respObj["Error"].(map[string]interface{}); ok {
			code := ""
			if v, ok := errObj["Code"].(string); ok {
				code = v
			}
			msg := ""
			if v, ok := errObj["Message"].(string); ok {
				msg = v
			}
			return nil, fmt.Errorf("tencent: API error %s: %s", code, msg)
		}
	}

	return result, nil
}

func tc3Authorization(secretID, secretKey, host, service string, timestamp int64, body []byte) string {
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	canonicalHeaders := "content-type:application/json; charset=utf-8\n" + "host:" + host + "\n"
	signedHeaders := "content-type;host"
	hashedPayload := sha256Hex(body)

	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + hashedPayload

	stringToSign := "TC3-HMAC-SHA256\n" + fmt.Sprintf("%d", timestamp) + "\n" + date + "/" + service + "/tc3_request\n" + sha256Hex([]byte(canonicalRequest))

	secretDate := hmacSHA256([]byte("TC3"+secretKey), []byte(date))
	secretService := hmacSHA256(secretDate, []byte(service))
	secretSigning := hmacSHA256(secretService, []byte("tc3_request"))
	signature := hex.EncodeToString(hmacSHA256(secretSigning, []byte(stringToSign)))

	return fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s/%s/tc3_request, SignedHeaders=%s, Signature=%s",
		secretID, date, service, signedHeaders, signature)
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

// unwrapResponse extracts the inner "Response" object when present.
func unwrapResponse(result map[string]interface{}) map[string]interface{} {
	if resp, ok := result["Response"].(map[string]interface{}); ok {
		return resp
	}
	return result
}

// TencentProvider implements types.CloudProvider for Tencent Cloud.
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
			return fmt.Errorf("tencent: SecretId (AccessKeyID) is required")
		}
		if creds.AccessKeySecret == "" {
			return fmt.Errorf("tencent: SecretKey (AccessKeySecret) is required")
		}
	case types.AuthTypeOIDC:
		if creds.OIDCToken == "" {
			return fmt.Errorf("tencent: OIDCToken is required")
		}
	default:
		return fmt.Errorf("tencent: unsupported auth type, expect aksk or oidc")
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
		region = "ap-guangzhou"
	}

	client := newTencentClient(creds)
	result, err := client.call(ctx, "vpc.tencentcloudapi.com", "vpc", "DescribeVpcs", "2017-03-12", region, map[string]interface{}{
		"Offset": 0,
		"Limit":  100,
	})
	if err != nil {
		logx.Warn(ctx, "tencent DiscoverVPCs failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	data := unwrapResponse(result)
	if vpcArr, ok := data["VpcSet"].([]interface{}); ok {
		for _, v := range vpcArr {
			if vm, ok := v.(map[string]interface{}); ok {
				r := types.Resource{
					Provider:     types.ProviderTypeTencent,
					Region:       region,
					ResourceType: "vpc",
				}
				if id, ok := vm["VpcId"].(string); ok {
					r.ResourceID = id
					r.ID = id
				}
				if name, ok := vm["VpcName"].(string); ok {
					r.ResourceName = name
				}
				if status, ok := vm["State"].(string); ok {
					r.Status = status
				}
				resources = append(resources, r)
			}
		}
	}
	logx.Info(ctx, "tencent VPCs discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *TencentProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
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
		region = "ap-guangzhou"
	}

	body := map[string]interface{}{
		"Offset": 0,
		"Limit":  100,
	}
	if vpcID != "" {
		body["Filters"] = []map[string]interface{}{
			{"Name": "vpc-id", "Values": []string{vpcID}},
		}
	}

	client := newTencentClient(creds)
	result, err := client.call(ctx, "vpc.tencentcloudapi.com", "vpc", "DescribeSubnets", "2017-03-12", region, body)
	if err != nil {
		logx.Warn(ctx, "tencent DiscoverSubnets failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	data := unwrapResponse(result)
	if swArr, ok := data["SubnetSet"].([]interface{}); ok {
		for _, v := range swArr {
			if vm, ok := v.(map[string]interface{}); ok {
				r := types.Resource{
					Provider:     types.ProviderTypeTencent,
					Region:       region,
					ResourceType: "subnet",
				}
				if id, ok := vm["SubnetId"].(string); ok {
					r.ResourceID = id
					r.ID = id
				}
				if name, ok := vm["SubnetName"].(string); ok {
					r.ResourceName = name
				}
				if status, ok := vm["State"].(string); ok {
					r.Status = status
				}
				resources = append(resources, r)
			}
		}
	}
	logx.Info(ctx, "tencent subnets discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *TencentProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
		region = "ap-guangzhou"
	}

	client := newTencentClient(creds)
	result, err := client.call(ctx, "cvm.tencentcloudapi.com", "cvm", "DescribeInstances", "2017-03-12", region, map[string]interface{}{
		"Offset": 0,
		"Limit":  100,
	})
	if err != nil {
		logx.Warn(ctx, "tencent DiscoverInstances failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	data := unwrapResponse(result)
	if instArr, ok := data["InstanceSet"].([]interface{}); ok {
		for _, v := range instArr {
			if vm, ok := v.(map[string]interface{}); ok {
				r := types.Resource{
					Provider:     types.ProviderTypeTencent,
					Region:       region,
					ResourceType: "instance",
				}
				if id, ok := vm["InstanceId"].(string); ok {
					r.ResourceID = id
					r.ID = id
				}
				if name, ok := vm["InstanceName"].(string); ok {
					r.ResourceName = name
				}
				if status, ok := vm["InstanceState"].(string); ok {
					r.Status = status
				}
				if ips, ok := vm["PrivateIpAddresses"].([]interface{}); ok && len(ips) > 0 {
					if s, ok := ips[0].(string); ok {
						r.PrivateIP = s
					}
				}
				if ips, ok := vm["PublicIpAddresses"].([]interface{}); ok && len(ips) > 0 {
					if s, ok := ips[0].(string); ok {
						r.PublicIP = s
					}
				}
				resources = append(resources, r)
			}
		}
	}
	logx.Info(ctx, "tencent instances discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *TencentProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
		region = "ap-guangzhou"
	}

	client := newTencentClient(creds)
	result, err := client.call(ctx, "tke.tencentcloudapi.com", "tke", "DescribeClusters", "2018-05-25", region, map[string]interface{}{
		"Offset": 0,
		"Limit":  100,
	})
	if err != nil {
		logx.Warn(ctx, "tencent DiscoverClusters failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	data := unwrapResponse(result)
	arr := []interface{}{}
	if v, ok := data["Clusters"].([]interface{}); ok {
		arr = v
	} else if v, ok := data["ClusterSet"].([]interface{}); ok {
		arr = v
	}
	for _, v := range arr {
		if vm, ok := v.(map[string]interface{}); ok {
			r := types.Resource{
				Provider:     types.ProviderTypeTencent,
				Region:       region,
				ResourceType: "cluster",
			}
			for _, idKey := range []string{"ClusterId", "ClusterID", "Id"} {
				if id, ok := vm[idKey].(string); ok && id != "" {
					r.ResourceID = id
					r.ID = id
					break
				}
			}
			for _, nameKey := range []string{"ClusterName", "Name"} {
				if name, ok := vm[nameKey].(string); ok && name != "" {
					r.ResourceName = name
					break
				}
			}
			for _, statusKey := range []string{"ClusterStatus", "Status", "ClusterState"} {
				if status, ok := vm[statusKey].(string); ok && status != "" {
					r.Status = status
					break
				}
			}
			resources = append(resources, r)
		}
	}
	logx.Info(ctx, "tencent clusters discovered", "count", len(resources), "region", region)
	return resources, nil
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
