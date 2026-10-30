package volcengine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

func TestProviderType(t *testing.T) {
	if got := NewVolcengineProvider().ProviderType(); got != types.ProviderTypeVolcengine {
		t.Errorf("ProviderType() = %q, want %q", got, types.ProviderTypeVolcengine)
	}
}

func TestValidateCredentials_AuthMatrix(t *testing.T) {
	p := NewVolcengineProvider()
	ctx := context.Background()

	cases := []struct {
		name    string
		creds   types.Credentials
		wantErr bool
	}{
		{"aksk 完整", types.Credentials{Type: types.AuthTypeAKSK, AccessKeyID: "ak", AccessKeySecret: "sk"}, false},
		{"aksk 缺 AccessKeyID", types.Credentials{Type: types.AuthTypeAKSK, AccessKeySecret: "sk"}, true},
		{"aksk 缺 SecretKey", types.Credentials{Type: types.AuthTypeAKSK, AccessKeyID: "ak"}, true},
		{"oidc 完整", types.Credentials{Type: types.AuthTypeOIDC, OIDCToken: "token"}, false},
		{"oidc 缺 token", types.Credentials{Type: types.AuthTypeOIDC}, true},
		{"managed_identity 不受支持", types.Credentials{Type: types.AuthTypeManagedIdentity}, true},
		{"零值凭据", types.Credentials{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateCredentials(ctx, tc.creds)
			if tc.wantErr && err == nil {
				t.Fatal("期望报错，得到 nil（错误凭据被静默放行）")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("期望通过，得到错误：%v", err)
			}
		})
	}
}

func TestValidateCredentials_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewVolcengineProvider().ValidateCredentials(ctx, types.Credentials{
		Type: types.AuthTypeAKSK, AccessKeyID: "ak", AccessKeySecret: "sk",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消 ctx 应返回 context.Canceled，得到 %v", err)
	}
}

func TestListRegions(t *testing.T) {
	regions, err := NewVolcengineProvider().ListRegions(context.Background(), types.Credentials{})
	if err != nil {
		t.Fatalf("ListRegions() unexpected error: %v", err)
	}
	if len(regions) == 0 {
		t.Fatal("ListRegions() expected non-empty list, got empty")
	}
	seen := make(map[string]bool, len(regions))
	for _, r := range regions {
		if r.ID == "" || r.Name == "" || r.Endpoint == "" {
			t.Errorf("region 字段不完整：%+v", r)
		}
		if seen[r.ID] {
			t.Errorf("region ID 重复：%q", r.ID)
		}
		seen[r.ID] = true
	}
}

func TestListRegions_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	regions, err := NewVolcengineProvider().ListRegions(ctx, types.Credentials{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消 ctx 应返回 context.Canceled，得到 %v", err)
	}
	if regions != nil {
		t.Errorf("出错时应返回 nil regions，得到 %d 条", len(regions))
	}
}

func TestDiscoverMethodsReturnEmptyNotError(t *testing.T) {
	p := NewVolcengineProvider()
	ctx := context.Background()
	creds := types.Credentials{}

	vpcs, err := p.DiscoverVPCs(ctx, creds, "cn-beijing")
	if err != nil || len(vpcs) != 0 {
		t.Errorf("DiscoverVPCs() = (%d, %v)，期望 (0, nil)", len(vpcs), err)
	}
	subnets, err := p.DiscoverSubnets(ctx, creds, "cn-beijing", "vpc-123")
	if err != nil || len(subnets) != 0 {
		t.Errorf("DiscoverSubnets() = (%d, %v)，期望 (0, nil)", len(subnets), err)
	}
	instances, err := p.DiscoverInstances(ctx, creds, "cn-beijing")
	if err != nil || len(instances) != 0 {
		t.Errorf("DiscoverInstances() = (%d, %v)，期望 (0, nil)", len(instances), err)
	}
	clusters, err := p.DiscoverClusters(ctx, creds, "cn-beijing")
	if err != nil || len(clusters) != 0 {
		t.Errorf("DiscoverClusters() = (%d, %v)，期望 (0, nil)", len(clusters), err)
	}
}

func TestMutatingMethodsRequireAKSK(t *testing.T) {
	p := NewVolcengineProvider()
	ctx := context.Background()
	creds := types.Credentials{}

	if _, err := p.CreateResource(ctx, creds, types.CreateResourceRequest{ResourceType: "ecs"}); err == nil {
		t.Errorf("CreateResource() err = nil，期望非 nil（非 AKSK 凭据应被拒绝）")
	}
	if err := p.DeleteResource(ctx, creds, "ins-123"); err == nil {
		t.Errorf("DeleteResource() err = nil，期望非 nil（非 AKSK 凭据应被拒绝）")
	}
	if err := p.SyncTags(ctx, creds, "ins-123", map[string]string{"env": "test"}); err == nil {
		t.Errorf("SyncTags() err = nil，期望非 nil（非 AKSK 凭据应被拒绝）")
	}
}

func TestCreateResource_Validation(t *testing.T) {
	p := NewVolcengineProvider()
	ctx := context.Background()
	creds := types.Credentials{Type: types.AuthTypeAKSK, AccessKeyID: "ak", AccessKeySecret: "sk"}

	// 资源类型不受支持
	if _, err := p.CreateResource(ctx, creds, types.CreateResourceRequest{ResourceType: "vpc", Properties: map[string]string{"ImageId": "img-1"}}); err == nil {
		t.Errorf("CreateResource(vpc) err = nil，期望非 nil")
	}
	// 缺少 ImageId
	if _, err := p.CreateResource(ctx, creds, types.CreateResourceRequest{ResourceType: "instance"}); err == nil {
		t.Errorf("CreateResource(缺 ImageId) err = nil，期望非 nil")
	}
	// 完整参数应绕过参数校验并发起真实调用（此处无法联网，期望非 nil 且不再是 ErrNotImplemented）
	ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := p.CreateResource(ctx2, creds, types.CreateResourceRequest{
		ResourceType: "instance",
		Region:       "cn-beijing",
		Properties:   map[string]string{"ImageId": "img-1", "InstanceType": "ecs.c6.large", "ZoneId": "cn-beijing-a"},
		Name:         "test-instance",
		Tags:         map[string]string{"env": "test"},
	}); err == nil || errors.Is(err, types.ErrNotImplemented) {
		t.Errorf("CreateResource(完整参数) 应尝试真实调用而非返回 ErrNotImplemented，得到 %v", err)
	}
}

func TestDeleteResource_Validation(t *testing.T) {
	p := NewVolcengineProvider()
	ctx := context.Background()
	creds := types.Credentials{Type: types.AuthTypeAKSK, AccessKeyID: "ak", AccessKeySecret: "sk"}

	if err := p.DeleteResource(ctx, creds, ""); err == nil {
		t.Errorf("DeleteResource(空 resourceID) err = nil，期望非 nil")
	}
}

func TestSyncTags_Validation(t *testing.T) {
	p := NewVolcengineProvider()
	ctx := context.Background()
	creds := types.Credentials{Type: types.AuthTypeAKSK, AccessKeyID: "ak", AccessKeySecret: "sk"}

	if err := p.SyncTags(ctx, creds, "", map[string]string{"env": "test"}); err == nil {
		t.Errorf("SyncTags(空 resourceID) err = nil，期望非 nil")
	}
	if err := p.SyncTags(ctx, creds, "ins-123", nil); err != nil {
		t.Errorf("SyncTags(空标签) err = %v，期望 nil", err)
	}
}
