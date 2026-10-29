// Package aws 提供 AWS 云厂商适配器实现。
//
// 基于原生 HTTP 调用（AWS Signature Version 4）实现，不依赖 AWS 官方 SDK：
// - ValidateCredentials：本地格式校验（AK/SK 或 OIDC）
// - ListRegions：返回内置区域列表
// - DiscoverVPCs：调用 EC2 DescribeVpcs
// - DiscoverSubnets：调用 EC2 DescribeSubnets
// - DiscoverInstances：调用 EC2 DescribeInstances
// - DiscoverClusters：调用 EKS DescribeClusters/ListClusters + DescribeCluster
// - CreateResource/DeleteResource/SyncTags：返回 ErrNotImplemented
package aws

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
	"sort"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// awsClient wraps HTTP calls to AWS APIs.
type awsClient struct {
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	http            *http.Client
}

func newAWSClient(creds types.Credentials) *awsClient {
	return &awsClient{
		accessKeyID:     creds.AccessKeyID,
		secretAccessKey: creds.AccessKeySecret,
		sessionToken:    creds.OIDCToken,
		http:            &http.Client{Timeout: 30 * time.Second},
	}
}

// doRequest performs a SigV4-signed HTTP request and parses a JSON response.
func (c *awsClient) doRequest(ctx context.Context, method, host, path, queryString string, body []byte, service, region string, extraHeaders map[string]string) (map[string]interface{}, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if region == "" {
		region = "us-east-1"
	}
	if body == nil {
		body = []byte{}
	}

	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := time.Now().UTC().Format("20060102")

	payloadHash := sha256Hex(body)

	headerMap := map[string]string{}
	headerMap["host"] = host
	headerMap["x-amz-date"] = amzDate
	if c.sessionToken != "" {
		headerMap["x-amz-security-token"] = c.sessionToken
	}
	contentType := ""
	if len(body) > 0 {
		if v, ok := extraHeaders["Content-Type"]; ok {
			contentType = v
		} else if v, ok := extraHeaders["content-type"]; ok {
			contentType = v
		} else {
			contentType = "application/octet-stream"
		}
		headerMap["content-type"] = contentType
	}
	for k, v := range extraHeaders {
		headerMap[strings.ToLower(k)] = v
	}

	keys := make([]string, 0, len(headerMap))
	for k := range headerMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonicalHeaders strings.Builder
	for _, k := range keys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(headerMap[k]))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(keys, ";")

	canonicalRequest := method + "\n" + path + "\n" + queryString + "\n" + canonicalHeaders.String() + "\n" + signedHeaders + "\n" + payloadHash

	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))

	signingKey := getSignatureKey(c.secretAccessKey, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authorization := "AWS4-HMAC-SHA256 Credential=" + c.accessKeyID + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature

	reqURL := "https://" + host + path
	if queryString != "" {
		reqURL += "?" + queryString
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("aws: create request: %w", err)
	}
	req.Header.Set("Host", host)
	req.Header.Set("X-Amz-Date", amzDate)
	if c.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.sessionToken)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", authorization)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("aws: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("aws: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if len(respBody) == 0 {
		return map[string]interface{}{}, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("aws: parse JSON: %w", err)
	}
	return result, nil
}

func (c *awsClient) callEC2(ctx context.Context, region, action string, params map[string]string) (map[string]interface{}, error) {
	if params == nil {
		params = make(map[string]string)
	}
	values := url.Values{}
	values.Set("Action", action)
	values.Set("Version", "2016-11-15")
	for k, v := range params {
		values.Set(k, v)
	}
	query := values.Encode()
	return c.doRequest(ctx, "GET", ec2Host(region), "/", query, nil, "ec2", region, map[string]string{})
}

func (c *awsClient) callEKS(ctx context.Context, region, target string, body map[string]interface{}) (map[string]interface{}, error) {
	bodyBytes := []byte("{}")
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("aws: marshal body: %w", err)
		}
		bodyBytes = b
	}
	headers := map[string]string{
		"Content-Type": "application/x-amz-json-1.1",
		"X-Amz-Target": "eks." + target,
	}
	return c.doRequest(ctx, "POST", eksHost(region), "/", "", bodyBytes, "eks", region, headers)
}

func ec2Host(region string) string {
	suffix := "amazonaws.com"
	if strings.HasPrefix(region, "cn-") {
		suffix = "amazonaws.com.cn"
	}
	return "ec2." + region + "." + suffix
}

func eksHost(region string) string {
	suffix := "amazonaws.com"
	if strings.HasPrefix(region, "cn-") {
		suffix = "amazonaws.com.cn"
	}
	return "eks." + region + "." + suffix
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

func getSignatureKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

// --- helpers for JSON extraction ---

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
				for _, ik := range []string{"item", "Item", "member", "members"} {
					if s, ok := inner[ik].([]interface{}); ok {
						return s
					}
				}
			}
		}
	}
	return nil
}

func nameFromTags(vm map[string]interface{}) string {
	for _, tagKey := range []string{"Tags", "tags", "TagSet", "tagSet"} {
		v, ok := vm[tagKey]
		if !ok {
			continue
		}
		var arr []interface{}
		switch val := v.(type) {
		case []interface{}:
			arr = val
		case map[string]interface{}:
			for _, ik := range []string{"item", "Item", "member"} {
				if s, ok := val[ik].([]interface{}); ok {
					arr = s
					break
				}
			}
		}
		for _, t := range arr {
			tm, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			k := getString(tm, "Key", "key", "name", "Name")
			if k == "Name" || k == "name" {
				if v := getString(tm, "Value", "value"); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func collectInstances(m map[string]interface{}) []interface{} {
	var out []interface{}
	for _, key := range []string{"Instances", "instances"} {
		if s, ok := m[key].([]interface{}); ok {
			out = append(out, s...)
		}
		if inner, ok := m[key].(map[string]interface{}); ok {
			for _, ik := range []string{"item", "Item", "member"} {
				if s, ok := inner[ik].([]interface{}); ok {
					out = append(out, s...)
				}
			}
		}
	}
	for _, key := range []string{"Reservations", "reservations"} {
		if s, ok := m[key].([]interface{}); ok {
			for _, r := range s {
				if rm, ok := r.(map[string]interface{}); ok {
					out = append(out, collectInstances(rm)...)
				}
			}
		}
		if inner, ok := m[key].(map[string]interface{}); ok {
			for _, ik := range []string{"item", "Item", "member"} {
				if s, ok := inner[ik].([]interface{}); ok {
					for _, r := range s {
						if rm, ok := r.(map[string]interface{}); ok {
							out = append(out, collectInstances(rm)...)
						}
					}
				}
			}
		}
	}
	for _, w := range []string{"DescribeInstancesResponse", "DescribeInstancesResult"} {
		if inner, ok := m[w].(map[string]interface{}); ok {
			out = append(out, collectInstances(inner)...)
		}
	}
	return out
}

func getInstanceStatus(vm map[string]interface{}) string {
	if st, ok := vm["State"].(map[string]interface{}); ok {
		if s := getString(st, "Name", "name", "Code"); s != "" {
			return s
		}
	}
	if st, ok := vm["state"].(map[string]interface{}); ok {
		if s := getString(st, "Name", "name"); s != "" {
			return s
		}
	}
	return getString(vm, "State", "state", "InstanceState", "instanceState")
}

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
			return fmt.Errorf("aws: AccessKeyID is required")
		}
		if creds.AccessKeySecret == "" {
			return fmt.Errorf("aws: SecretAccessKey is required")
		}
	case types.AuthTypeOIDC:
		if creds.OIDCToken == "" {
			return fmt.Errorf("aws: OIDCToken is required")
		}
	default:
		return fmt.Errorf("aws: unsupported auth type, expect aksk or oidc")
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
		region = "us-east-1"
	}

	client := newAWSClient(creds)
	result, err := client.callEC2(ctx, region, "DescribeVpcs", nil)
	if err != nil {
		logx.Warn(ctx, "aws DiscoverVPCs failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	var vpcArr []interface{}
	if s := findSlice(result, "Vpcs", "vpcs"); s != nil {
		vpcArr = s
	} else {
		for _, w := range []string{"DescribeVpcsResponse", "DescribeVpcsResult"} {
			if inner, ok := result[w].(map[string]interface{}); ok {
				if s := findSlice(inner, "VpcSet", "vpcSet", "Vpcs", "vpcs"); s != nil {
					vpcArr = s
					break
				}
				// item wrapper
				if vs, ok := inner["vpcSet"].(map[string]interface{}); ok {
					for _, ik := range []string{"item", "Item", "member"} {
						if s, ok := vs[ik].([]interface{}); ok {
							vpcArr = s
							break
						}
					}
				}
			}
		}
	}

	for _, v := range vpcArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeAWS,
			Region:       region,
			ResourceType: "vpc",
		}
		id := getString(vm, "VpcId", "vpcId", "VpcID", "id", "Id")
		r.ResourceID = id
		r.ID = id
		if name := nameFromTags(vm); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = getString(vm, "VpcId", "vpcId")
		}
		r.Status = getString(vm, "State", "state")
		resources = append(resources, r)
	}
	logx.Info(ctx, "aws VPCs discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *AWSProvider) DiscoverSubnets(ctx context.Context, creds types.Credentials, region string, vpcID string) ([]types.Resource, error) {
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
		region = "us-east-1"
	}

	params := map[string]string{}
	if vpcID != "" {
		params["Filter.1.Name"] = "vpc-id"
		params["Filter.1.Value.1"] = vpcID
	}

	client := newAWSClient(creds)
	result, err := client.callEC2(ctx, region, "DescribeSubnets", params)
	if err != nil {
		logx.Warn(ctx, "aws DiscoverSubnets failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	var subnetArr []interface{}
	if s := findSlice(result, "Subnets", "subnets"); s != nil {
		subnetArr = s
	} else {
		for _, w := range []string{"DescribeSubnetsResponse", "DescribeSubnetsResult"} {
			if inner, ok := result[w].(map[string]interface{}); ok {
				if s := findSlice(inner, "SubnetSet", "subnetSet", "Subnets", "subnets"); s != nil {
					subnetArr = s
					break
				}
				if ss, ok := inner["subnetSet"].(map[string]interface{}); ok {
					for _, ik := range []string{"item", "Item", "member"} {
						if s, ok := ss[ik].([]interface{}); ok {
							subnetArr = s
							break
						}
					}
				}
			}
		}
	}

	for _, v := range subnetArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeAWS,
			Region:       region,
			ResourceType: "subnet",
		}
		id := getString(vm, "SubnetId", "subnetId", "id", "Id")
		r.ResourceID = id
		r.ID = id
		if name := nameFromTags(vm); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = id
		}
		r.Status = getString(vm, "State", "state")
		resources = append(resources, r)
	}
	logx.Info(ctx, "aws subnets discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *AWSProvider) DiscoverInstances(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
		region = "us-east-1"
	}

	client := newAWSClient(creds)
	result, err := client.callEC2(ctx, region, "DescribeInstances", nil)
	if err != nil {
		logx.Warn(ctx, "aws DiscoverInstances failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	resources := make([]types.Resource, 0)
	instArr := collectInstances(result)
	// unwrap response wrappers if collectInstances found nothing
	if len(instArr) == 0 {
		for _, w := range []string{"DescribeInstancesResponse", "DescribeInstancesResult"} {
			if inner, ok := result[w].(map[string]interface{}); ok {
				instArr = append(instArr, collectInstances(inner)...)
			}
		}
	}

	for _, v := range instArr {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		r := types.Resource{
			Provider:     types.ProviderTypeAWS,
			Region:       region,
			ResourceType: "instance",
		}
		id := getString(vm, "InstanceId", "instanceId", "id", "Id")
		r.ResourceID = id
		r.ID = id
		if name := nameFromTags(vm); name != "" {
			r.ResourceName = name
		} else {
			r.ResourceName = getString(vm, "InstanceId", "instanceId")
		}
		r.Status = getInstanceStatus(vm)
		r.PrivateIP = getString(vm, "PrivateIpAddress", "privateIpAddress", "PrivateIp")
		r.PublicIP = getString(vm, "PublicIpAddress", "publicIpAddress", "PublicIp")
		resources = append(resources, r)
	}
	logx.Info(ctx, "aws instances discovered", "count", len(resources), "region", region)
	return resources, nil
}

func (p *AWSProvider) DiscoverClusters(ctx context.Context, creds types.Credentials, region string) ([]types.Resource, error) {
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
		region = "us-east-1"
	}

	client := newAWSClient(creds)

	// Try DescribeClusters first (per task spec), fall back to ListClusters.
	result, err := client.callEKS(ctx, region, "DescribeClusters", map[string]interface{}{})
	if err != nil {
		result, err = client.callEKS(ctx, region, "ListClusters", map[string]interface{}{})
	}
	if err != nil {
		logx.Warn(ctx, "aws DiscoverClusters failed", "error", err, "region", region)
		return []types.Resource{}, nil
	}

	// gather cluster names / objects
	var clusterNames []string
	var clusterObjs []map[string]interface{}
	seenName := map[string]bool{}

	var gather func(m map[string]interface{})
	gather = func(m map[string]interface{}) {
		for _, key := range []string{"clusters", "Clusters", "clusterSet", "ClusterSet"} {
			v, ok := m[key]
			if !ok {
				continue
			}
			switch val := v.(type) {
			case []interface{}:
				for _, item := range val {
					switch it := item.(type) {
					case string:
						if it != "" && !seenName[it] {
							seenName[it] = true
							clusterNames = append(clusterNames, it)
						}
					case map[string]interface{}:
						clusterObjs = append(clusterObjs, it)
					}
				}
			case map[string]interface{}:
				for _, ik := range []string{"item", "Item", "member", "members"} {
					if s, ok := val[ik].([]interface{}); ok {
						for _, item := range s {
							switch it := item.(type) {
							case string:
								if it != "" && !seenName[it] {
									seenName[it] = true
									clusterNames = append(clusterNames, it)
								}
							case map[string]interface{}:
								clusterObjs = append(clusterObjs, it)
							}
						}
					}
				}
			case string:
				if val != "" && !seenName[val] {
					seenName[val] = true
					clusterNames = append(clusterNames, val)
				}
			}
		}
		for _, w := range []string{"DescribeClustersResponse", "ListClustersResponse", "DescribeClusterResult", "result"} {
			if inner, ok := m[w].(map[string]interface{}); ok {
				gather(inner)
			}
		}
	}
	gather(result)

	resources := make([]types.Resource, 0)
	objSeen := map[string]bool{}

	for _, cm := range clusterObjs {
		id := getString(cm, "name", "Name", "clusterName", "ClusterName", "id", "Id", "arn", "Arn")
		if id == "" {
			continue
		}
		if objSeen[id] {
			continue
		}
		objSeen[id] = true
		r := types.Resource{
			Provider:     types.ProviderTypeAWS,
			Region:       region,
			ResourceType: "cluster",
			ResourceID:   id,
			ID:           id,
			ResourceName: getString(cm, "name", "Name", "clusterName", "ClusterName"),
			Status:       getString(cm, "status", "Status", "clusterStatus", "ClusterStatus"),
		}
		if r.ResourceName == "" {
			r.ResourceName = id
		}
		resources = append(resources, r)
	}

	for _, name := range clusterNames {
		if objSeen[name] {
			continue
		}
		objSeen[name] = true
		status := ""
		cname := name
		res2, err := client.callEKS(ctx, region, "DescribeCluster", map[string]interface{}{"name": name})
		if err == nil {
			var cm map[string]interface{}
			if c, ok := res2["cluster"].(map[string]interface{}); ok {
				cm = c
			} else if c, ok := res2["Cluster"].(map[string]interface{}); ok {
				cm = c
			} else {
				cm = res2
			}
			if n := getString(cm, "name", "Name", "clusterName", "ClusterName"); n != "" {
				cname = n
			}
			status = getString(cm, "status", "Status", "clusterStatus", "ClusterStatus")
		}
		r := types.Resource{
			Provider:     types.ProviderTypeAWS,
			Region:       region,
			ResourceType: "cluster",
			ResourceID:   name,
			ID:           name,
			ResourceName: cname,
			Status:       status,
		}
		resources = append(resources, r)
	}

	logx.Info(ctx, "aws clusters discovered", "count", len(resources), "region", region)
	return resources, nil
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
