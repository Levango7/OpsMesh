package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

func TestProviderType(t *testing.T) {
	if got := NewAWSProvider().ProviderType(); got != types.ProviderTypeAWS {
		t.Errorf("ProviderType() = %q, want %q", got, types.ProviderTypeAWS)
	}
}

// TestValidateCredentials_AuthMatrix 钉住 stub 厂商的认证契约：
// aksk 两项必须齐全、oidc 必须带 token、其余认证方式必须显式拒绝。
// 这是控制面"填错凭据要当场报错而不是静默放行"的依据。
func TestValidateCredentials_AuthMatrix(t *testing.T) {
	p := NewAWSProvider()
	ctx := context.Background()

	cases := []struct {
		name    string
		creds   types.Credentials
		wantErr bool
	}{
		{"aksk 完整", types.Credentials{Type: types.AuthTypeAKSK, AccessKeyID: "id", AccessKeySecret: "secret"}, false},
		{"aksk 缺 AccessKeyID", types.Credentials{Type: types.AuthTypeAKSK, AccessKeySecret: "secret"}, true},
		{"aksk 缺 Secret", types.Credentials{Type: types.AuthTypeAKSK, AccessKeyID: "id"}, true},
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
	err := NewAWSProvider().ValidateCredentials(ctx, types.Credentials{
		Type: types.AuthTypeAKSK, AccessKeyID: "id", AccessKeySecret: "secret",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消 ctx 应返回 context.Canceled，得到 %v", err)
	}
}

func TestListRegions(t *testing.T) {
	regions, err := NewAWSProvider().ListRegions(context.Background(), types.Credentials{})
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
	regions, err := NewAWSProvider().ListRegions(ctx, types.Credentials{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消 ctx 应返回 context.Canceled，得到 %v", err)
	}
	if regions != nil {
		t.Errorf("出错时应返回 nil regions，得到 %d 条", len(regions))
	}
}

// TestDiscoverMethodsReturnEmptyNotError 钉住 stub 的降级契约：
// 未接 SDK 的厂商在发现类能力上返回空集而非报错，调用方据此展示"暂无资源"。
func TestDiscoverMethodsReturnEmptyNotError(t *testing.T) {
	p := NewAWSProvider()
	ctx := context.Background()
	creds := types.Credentials{}

	vpcs, err := p.DiscoverVPCs(ctx, creds, "us-east-1")
	if err != nil || len(vpcs) != 0 {
		t.Errorf("DiscoverVPCs() = (%d, %v)，期望 (0, nil)", len(vpcs), err)
	}
	subnets, err := p.DiscoverSubnets(ctx, creds, "us-east-1", "vpc-123")
	if err != nil || len(subnets) != 0 {
		t.Errorf("DiscoverSubnets() = (%d, %v)，期望 (0, nil)", len(subnets), err)
	}
	instances, err := p.DiscoverInstances(ctx, creds, "us-east-1")
	if err != nil || len(instances) != 0 {
		t.Errorf("DiscoverInstances() = (%d, %v)，期望 (0, nil)", len(instances), err)
	}
	clusters, err := p.DiscoverClusters(ctx, creds, "us-east-1")
	if err != nil || len(clusters) != 0 {
		t.Errorf("DiscoverClusters() = (%d, %v)，期望 (0, nil)", len(clusters), err)
	}
}

// TestMutatingMethodsReturnErrNotImplemented 钉住"写操作绝不能假成功"：
// stub 厂商未接 SDK，必须返回 ErrNotImplemented 让调用方区分"不支持"与"执行失败"。
func TestMutatingMethodsReturnErrNotImplemented(t *testing.T) {
	p := NewAWSProvider()
	ctx := context.Background()
	creds := types.Credentials{}

	if _, err := p.CreateResource(ctx, creds, types.CreateResourceRequest{ResourceType: "ec2"}); !errors.Is(err, types.ErrNotImplemented) {
		t.Errorf("CreateResource() err = %v，期望 ErrNotImplemented", err)
	}
	if err := p.DeleteResource(ctx, creds, "i-123"); !errors.Is(err, types.ErrNotImplemented) {
		t.Errorf("DeleteResource() err = %v，期望 ErrNotImplemented", err)
	}
	if err := p.SyncTags(ctx, creds, "i-123", map[string]string{"env": "test"}); !errors.Is(err, types.ErrNotImplemented) {
		t.Errorf("SyncTags() err = %v，期望 ErrNotImplemented", err)
	}
}
