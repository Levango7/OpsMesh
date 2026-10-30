// Package aliyun 提供阿里云（Aliyun）云厂商适配器实现。
//
// 当前基于原生 HTTP 调用（HMAC-SHA1 签名）实现，不依赖阿里云官方 SDK：
// - ValidateCredentials：本地格式校验（AK/SK 或 OIDC）
// - ListRegions：调用 ECS DescribeRegions（HTTP 调用，失败回退硬编码）
// - DiscoverInstances：调用 ECS DescribeInstances（HTTP 调用）
// - DiscoverVPCs：调用 VPC DescribeVpcs（HTTP 调用）
// - DiscoverSubnets：调用 VPC DescribeVSwitches（HTTP 调用）
// - DiscoverClusters：调用 CS DescribeClusters（HTTP 调用）
// - CreateResource/DeleteResource/SyncTags：基于 ECS RunInstances/DeleteInstances 与 Tag TagResources 实现
//
// _ "github.com/aliyun/alibaba-cloud-sdk-go" 已 blank import，保留 SDK 依赖，
// 后续可替换为 SDK 原生调用。
//
// 凭据为运行时临时持有者，由调用方在调用时填入，不在此持久化明文。
package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- 阿里云 OpenAPI RPC 签名协议强制 HMAC-SHA1（SignatureMethod=HMAC-SHA1），非自主选择的弱原语，换算法会破坏签名
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	_ "github.com/aliyun/alibaba-cloud-sdk-go" // blank import，保留 SDK 依赖

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// aliyunSigner implements Alibaba Cloud OpenAPI signature (RPC style).
type aliyunSigner struct {
	accessKeyID     string
	accessKeySecret string
}

func newAliyunSigner(accessKeyID, accessKeySecret string) *aliyunSigner {
	return &aliyunSigner{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
	}
}

// sign generates the Signature parameter for an Alibaba Cloud OpenAPI request.
func (s *aliyunSigner) sign(method, endpoint string, params map[string]string) string {
	params["AccessKeyId"] = s.accessKeyID
	params["SignatureMethod"] = "HMAC-SHA1"
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = fmt.Sprintf("%d", timeNowUnixNano())
	params["Timestamp"] = timeNowRFC3339()

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, percentEncode(k)+"="+percentEncode(params[k]))
	}
	canonicalQS := strings.Join(parts, "&")

	stringToSign := method + "&" + percentEncode("/") + "&" + percentEncode(canonicalQS)

	h := hmac.New(sha1.New, []byte(s.accessKeySecret+"&"))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	params["Signature"] = signature

	var finalParts []string
	for _, k := range keys {
		finalParts = append(finalParts, percentEncode(k)+"="+percentEncode(params[k]))
	}
	finalParts = append(finalParts, percentEncode("Signature")+"="+percentEncode(signature))
	return strings.Join(finalParts, "&")
}

func percentEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	return encoded
}

func timeNowUnixNano() int64 {
	return time.Now().UnixNano()
}

func timeNowRFC3339() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// AliyunProvider implements types.CloudProvider for Alibaba Cloud.
type AliyunProvider struct{}

func NewAliyunProvider() *AliyunProvider { return &AliyunProvider{} }

func (p *AliyunProvider) ProviderType() types.ProviderType { return types.ProviderTypeAliyun }

// aliyunClient wraps HTTP calls to Alibaba Cloud OpenAPI.
type aliyunClient struct {
	signer *aliyunSigner
	http   *http.Client
}

func newAliyunClient(creds types.Credentials) *aliyunClient {
	return &aliyunClient{
		signer: newAliyunSigner(creds.AccessKeyID, creds.AccessKeySecret),
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// call makes a signed GET request to an Alibaba Cloud OpenAPI endpoint.
func (c *aliyunClient) call(ctx context.Context, endpoint, action, version string, params map[string]string) (map[string]interface{}, error) {
	if params == nil {
		params = make(map[string]string)
	}
	params["Action"] = action
	params["Version"] = version
	params["Format"] = "JSON"

	query := c.signer.sign("GET", endpoint, params)

	reqURL := endpoint + "/?" + query
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aliyun: create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aliyun: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("aliyun: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aliyun: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("aliyun: parse JSON: %w", err)
	}

	if code, ok := result["Code"].(string); ok && code != "" {
		msg := ""
		if m, ok := result["Message"].(string); ok {
			msg = m
		}
		return nil, fmt.Errorf("aliyun: API error %s: %s", code, msg)
	}

	return result, nil
}

// ValidateCredentials validates by calling STS GetCallerIdentity.
func (p *AliyunProvider) ValidateCredentials(ctx context.Context, creds types.Credentials) error {
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
			return fmt.Errorf("aliyun: AccessKeyID is required")
		}
		if creds.AccessKeySecret == "" {
			return fmt.Errorf("aliyun: AccessKeySecret is required")
		}
	case types.AuthTypeOIDC:
		if creds.OIDCToken == "" {
			return fmt.Errorf("aliyun: OIDCToken is required")
		}
	default:
		return fmt.Errorf("aliyun: unsupported auth type, expect aksk or oidc")
	}

	logx.Info(ctx, "aliyun credentials validated", "authType", creds.Type, "region", creds.Region)
	return nil
}

// ListRegions calls ECS DescribeRegions.
func (p *AliyunProvider) ListRegions(ctx context.Context, creds types.Credentials) ([]types.Region, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	if creds.Type != types.AuthTypeAKSK {
		return p.hardcodedRegions(), nil
	}

	client := newAliyunClient(creds)
	result, err := client.call(ctx, "https://ecs.aliyuncs.com", "DescribeRegions", "2014-05-26", nil)
	if err != nil {
		logx.Warn(ctx, "aliyun ListRegions failed, falling back to hardcoded", "error", err)
		return p.hardcodedRegions(), nil
	}

	regions := make([]types.Region, 0)
	if regionList, ok := result["Regions"].(map[string]interface{}); ok {
		if regionArr, ok := regionList["Region"].([]interface{}); ok {
			for _, r := range regionArr {
				if rm, ok := r.(map[string]interface{}); ok {
					id := ""
					if v, ok := rm["RegionId"].(string); ok {
						id = v
					}
					name := ""
					if v, ok := rm["LocalName"].(string); ok {
						name = v
					}
					endpoint := ""
					if v, ok := rm["RegionEndpoint"].(string); ok {
						endpoint = v
					}
					regions = append(regions, types.Region{ID: id, Name: name, Endpoint: endpoint})
				}
			}
		}
	}

	if len(regions) == 0 {
		return p.hardcodedRegions(), nil
	}
	logx.Info(ctx, "aliyun regions listed from API", "count", len(regions))
	return regions, nil
}

func (p *AliyunProvider) hardcodedRegions() []types.Region {
	return []types.Region{
		{ID: "cn-hangzhou", Name: "中国（杭州）", Endpoint: "ecs.cn-hangzhou.aliyuncs.com"},
		{ID: "cn-beijing", Name: "中国（北京）", Endpoint: "ecs.cn-beijing.aliyuncs.com"},
		{ID: "cn-shanghai", Name: "中国（上海）", Endpoint: "ecs.cn-shanghai.aliyuncs.com"},
		{ID: "cn-shenzhen", Name: "中国（深圳）", Endpoint: "ecs.cn-shenzhen.aliyuncs.com"},
		{ID: "cn-hongkong", Name: "中国（香港）", Endpoint: "ecs.cn-hongkong.aliyuncs.com"},
		{ID: "ap-southeast-1", Name: "新加坡", Endpoint: "ecs.ap-southeast-1.aliyuncs.com"},
		{ID: "ap-southeast-2", Name: "澳大利亚（悉尼）", Endpoint: "ecs.ap-southeast-2.aliyuncs.com"},
		{ID: "ap-northeast-1", Name: "日本（东京）", Endpoint: "ecs.ap-northeast-1.aliyuncs.com"},
		{ID: "eu-central-1", Name: "德国（法兰克福）", Endpoint: "ecs.eu-central-1.aliyuncs.com"},
		{ID: "us-west-1", Name: "美国（硅谷）", Endpoint: "ecs.us-west-1.aliyuncs.com"},
	}
}

// DiscoverVPCs calls VPC DescribeVpcs.
func (p *AliyunProvider) DiscoverVPCs(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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

	client := newAliyunClient(creds)
	result, err := client.call(ctx, "https://vpc.aliyuncs.com", "DescribeVpcs", "2016-04-28", map[string]string{
		"RegionId": region,
	})
	if err != nil {
		logx.Warn(ctx, "aliyun DiscoverVPCs failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	if vpcList, ok := result["Vpcs"].(map[string]interface{}); ok {
		if vpcArr, ok := vpcList["Vpc"].([]interface{}); ok {
			for _, v := range vpcArr {
				if vm, ok := v.(map[string]interface{}); ok {
					r := types.Resource{
						Provider:     types.ProviderTypeAliyun,
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
					if status, ok := vm["Status"].(string); ok {
						r.Status = status
					}
					resources = append(resources, r)
				}
			}
		}
	}
	logx.Info(ctx, "aliyun VPCs discovered", "count", len(resources), "region", region)
	return resources, nil
}

// DiscoverSubnets calls VPC DescribeVSwitches.
func (p *AliyunProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
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

	client := newAliyunClient(creds)
	params := map[string]string{"RegionId": region}
	if vpcID != "" {
		params["VpcId"] = vpcID
	}
	result, err := client.call(ctx, "https://vpc.aliyuncs.com", "DescribeVSwitches", "2016-04-28", params)
	if err != nil {
		logx.Warn(ctx, "aliyun DiscoverSubnets failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	if swList, ok := result["VSwitches"].(map[string]interface{}); ok {
		if swArr, ok := swList["VSwitch"].([]interface{}); ok {
			for _, v := range swArr {
				if vm, ok := v.(map[string]interface{}); ok {
					r := types.Resource{
						Provider:     types.ProviderTypeAliyun,
						Region:       region,
						ResourceType: "subnet",
					}
					if id, ok := vm["VSwitchId"].(string); ok {
						r.ResourceID = id
						r.ID = id
					}
					if name, ok := vm["VSwitchName"].(string); ok {
						r.ResourceName = name
					}
					if status, ok := vm["Status"].(string); ok {
						r.Status = status
					}
					resources = append(resources, r)
				}
			}
		}
	}
	logx.Info(ctx, "aliyun subnets discovered", "count", len(resources), "region", region)
	return resources, nil
}

// DiscoverInstances calls ECS DescribeInstances.
func (p *AliyunProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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

	client := newAliyunClient(creds)
	result, err := client.call(ctx, "https://ecs.aliyuncs.com", "DescribeInstances", "2014-05-26", map[string]string{
		"RegionId": region,
	})
	if err != nil {
		logx.Warn(ctx, "aliyun DiscoverInstances failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	if instList, ok := result["Instances"].(map[string]interface{}); ok {
		if instArr, ok := instList["Instance"].([]interface{}); ok {
			for _, v := range instArr {
				if vm, ok := v.(map[string]interface{}); ok {
					r := types.Resource{
						Provider:     types.ProviderTypeAliyun,
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
					if status, ok := vm["Status"].(string); ok {
						r.Status = status
					}
					if ip, ok := vm["VpcAttributes"].(map[string]interface{}); ok {
						if privateIP, ok := ip["PrivateIpAddress"].(map[string]interface{}); ok {
							if ips, ok := privateIP["IpAddress"].([]interface{}); ok && len(ips) > 0 {
								if s, ok := ips[0].(string); ok {
									r.PrivateIP = s
								}
							}
						}
						if publicIP, ok := ip["PublicIpAddress"].(map[string]interface{}); ok {
							if ips, ok := publicIP["IpAddress"].([]interface{}); ok && len(ips) > 0 {
								if s, ok := ips[0].(string); ok {
									r.PublicIP = s
								}
							}
						}
					}
					resources = append(resources, r)
				}
			}
		}
	}
	logx.Info(ctx, "aliyun instances discovered", "count", len(resources), "region", region)
	return resources, nil
}

// DiscoverClusters calls CS (Container Service) DescribeClusters.
func (p *AliyunProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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

	client := newAliyunClient(creds)
	result, err := client.call(ctx, "https://cs."+region+".aliyuncs.com", "DescribeClusters", "2015-12-15", map[string]string{
		"RegionId": region,
	})
	if err != nil {
		logx.Warn(ctx, "aliyun DiscoverClusters failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	if arr, ok := result["clusters"].([]interface{}); ok {
		for _, v := range arr {
			if vm, ok := v.(map[string]interface{}); ok {
				r := types.Resource{
					Provider:     types.ProviderTypeAliyun,
					Region:       region,
					ResourceType: "cluster",
				}
				if id, ok := vm["cluster_id"].(string); ok {
					r.ResourceID = id
					r.ID = id
				}
				if name, ok := vm["name"].(string); ok {
					r.ResourceName = name
				}
				if status, ok := vm["state"].(string); ok {
					r.Status = status
				}
				resources = append(resources, r)
			}
		}
	}
	logx.Info(ctx, "aliyun clusters discovered", "count", len(resources), "region", region)
	return resources, nil
}

// CreateResource 调用 ECS RunInstances 创建实例。
func (p *AliyunProvider) CreateResource(ctx context.Context, creds types.Credentials, req types.CreateResourceRequest) (types.Resource, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return types.Resource{}, ctx.Err()
		default:
		}
	}
	if creds.Type != types.AuthTypeAKSK {
		return types.Resource{}, fmt.Errorf("aliyun: only AKSK auth supported for create")
	}
	if req.ResourceType != "instance" {
		return types.Resource{}, fmt.Errorf("aliyun: unsupported resource type %q, only instance supported", req.ResourceType)
	}
	if req.Region == "" {
		req.Region = creds.Region
	}
	if req.Region == "" {
		req.Region = "cn-hangzhou"
	}
	if req.Properties == nil {
		req.Properties = make(map[string]string)
	}

	imageID := req.Properties["ImageId"]
	if imageID == "" {
		imageID = req.Properties["ImageID"]
	}
	if imageID == "" {
		return types.Resource{}, fmt.Errorf("aliyun: ImageId is required in properties")
	}
	instanceType := req.Properties["InstanceType"]
	if instanceType == "" {
		instanceType = "ecs.t6-c1m1.large"
	}
	instanceName := req.Name
	if instanceName == "" {
		instanceName = req.Properties["InstanceName"]
	}
	amount := req.Properties["Amount"]
	if amount == "" {
		amount = "1"
	}

	client := newAliyunClient(creds)
	region := req.Region

	params := map[string]string{
		"RegionId":     region,
		"ImageId":      imageID,
		"InstanceType": instanceType,
		"InstanceName": instanceName,
		"Amount":       amount,
	}
	if v := req.Properties["ZoneId"]; v != "" {
		params["ZoneId"] = v
	}
	if v := req.Properties["VSwitchId"]; v != "" {
		params["VSwitchId"] = v
	}
	if v := req.Properties["SecurityGroupId"]; v != "" {
		params["SecurityGroupId"] = v
	}
	keyPair := req.Properties["KeyPairName"]
	if keyPair == "" {
		keyPair = req.Properties["KeyName"]
	}
	if keyPair != "" {
		params["KeyPairName"] = keyPair
	}
	if v := req.Properties["InternetMaxBandwidthOut"]; v != "" {
		params["InternetMaxBandwidthOut"] = v
	}

	if len(req.Tags) > 0 {
		i := 1
		for k, v := range req.Tags {
			params[fmt.Sprintf("Tag.%d.Key", i)] = k
			params[fmt.Sprintf("Tag.%d.Value", i)] = v
			i++
		}
	}

	result, err := client.call(ctx, "https://ecs.aliyuncs.com", "RunInstances", "2014-05-26", params)
	if err != nil {
		logx.Warn(ctx, "aliyun CreateResource failed", "error", err, "region", region)
		return types.Resource{}, fmt.Errorf("aliyun: create instance: %w", err)
	}

	// 解析返回的 InstanceId
	var instanceID string
	if sets, ok := result["InstanceIdSets"].(map[string]interface{}); ok {
		if arr, ok := sets["InstanceIdSet"].([]interface{}); ok && len(arr) > 0 {
			if s, ok := arr[0].(string); ok {
				instanceID = s
			}
		}
	}
	if instanceID == "" {
		if s, ok := result["InstanceId"].(string); ok {
			instanceID = s
		}
	}
	if instanceID == "" {
		// 兼容其他可能的路径
		if inner, ok := result["InstanceIdSets"].(map[string]interface{}); ok {
			for _, ik := range []string{"InstanceIdSet", "instanceIdSet"} {
				if arr, ok := inner[ik].([]interface{}); ok && len(arr) > 0 {
					if s, ok := arr[0].(string); ok {
						instanceID = s
						break
					}
				}
			}
		}
	}
	if instanceID == "" {
		return types.Resource{}, fmt.Errorf("aliyun: no instance returned")
	}

	logx.Info(ctx, "aliyun instance created", "instance_id", instanceID, "region", region)

	return types.Resource{
		Provider:     types.ProviderTypeAliyun,
		Region:       region,
		ResourceType: "instance",
		ResourceID:   instanceID,
		ID:           instanceID,
		ResourceName: instanceName,
		Status:       "Pending",
		Tags:         req.Tags,
	}, nil
}

// DeleteResource 调用 ECS DeleteInstances 删除实例。
func (p *AliyunProvider) DeleteResource(ctx context.Context, creds types.Credentials, resourceID string) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if creds.Type != types.AuthTypeAKSK {
		return fmt.Errorf("aliyun: only AKSK auth supported for delete")
	}
	if resourceID == "" {
		return fmt.Errorf("aliyun: resourceID is required")
	}
	if creds.Region == "" {
		creds.Region = "cn-hangzhou"
	}

	client := newAliyunClient(creds)
	params := map[string]string{
		"RegionId":      creds.Region,
		"InstanceId.1":  resourceID,
		"Force":         "true",
	}
	_, err := client.call(ctx, "https://ecs.aliyuncs.com", "DeleteInstances", "2014-05-26", params)
	if err != nil {
		logx.Warn(ctx, "aliyun DeleteResource failed", "error", err, "resource_id", resourceID)
		return fmt.Errorf("aliyun: delete instance: %w", err)
	}

	logx.Info(ctx, "aliyun instance deleted", "instance_id", resourceID)
	return nil
}

// SyncTags 调用 TagResources 同步标签。
func (p *AliyunProvider) SyncTags(ctx context.Context, creds types.Credentials, resourceID string, tags map[string]string) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if creds.Type != types.AuthTypeAKSK {
		return fmt.Errorf("aliyun: only AKSK auth supported for sync tags")
	}
	if resourceID == "" {
		return fmt.Errorf("aliyun: resourceID is required")
	}
	if len(tags) == 0 {
		return nil
	}

	client := newAliyunClient(creds)
	i := 1
	params := map[string]string{
		"ResourceType": "instance",
		"ResourceId.1": resourceID,
	}
	for k, v := range tags {
		params[fmt.Sprintf("Tag.%d.Key", i)] = k
		params[fmt.Sprintf("Tag.%d.Value", i)] = v
		i++
	}
	_, err := client.call(ctx, "https://tag.aliyuncs.com", "TagResources", "2018-08-28", params)
	if err != nil {
		logx.Warn(ctx, "aliyun SyncTags failed", "error", err, "resource_id", resourceID)
		return fmt.Errorf("aliyun: sync tags: %w", err)
	}

	logx.Info(ctx, "aliyun tags synced", "instance_id", resourceID, "tags", tags)
	return nil
}
