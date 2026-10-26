package aliyun

import (
	"context"
	"errors"
	"testing"

	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

func TestProviderType(t *testing.T) {
	p := NewAliyunProvider()
	if got := p.ProviderType(); got != types.ProviderTypeAliyun {
		t.Errorf("ProviderType() = %q, want %q", got, types.ProviderTypeAliyun)
	}
}

func TestValidateCredentialsRejectsEmpty(t *testing.T) {
	p := NewAliyunProvider()
	ctx := context.Background()

	// 完全空凭据
	if err := p.ValidateCredentials(ctx, types.Credentials{}); err == nil {
		t.Error("ValidateCredentials(empty) expected error, got nil")
	}

	// AK/SK 缺少 AccessKeyID
	if err := p.ValidateCredentials(ctx, types.Credentials{
		Type:            types.AuthTypeAKSK,
		AccessKeySecret: "secret",
	}); err == nil {
		t.Error("ValidateCredentials(missing AccessKeyID) expected error, got nil")
	}

	// AK/SK 缺少 AccessKeySecret
	if err := p.ValidateCredentials(ctx, types.Credentials{
		Type:         types.AuthTypeAKSK,
		AccessKeyID:  "key",
	}); err == nil {
		t.Error("ValidateCredentials(missing AccessKeySecret) expected error, got nil")
	}

	// OIDC 缺少 token
	if err := p.ValidateCredentials(ctx, types.Credentials{
		Type: types.AuthTypeOIDC,
	}); err == nil {
		t.Error("ValidateCredentials(missing OIDCToken) expected error, got nil")
	}

	// 不支持的 auth 类型
	if err := p.ValidateCredentials(ctx, types.Credentials{
		Type: "unsupported",
	}); err == nil {
		t.Error("ValidateCredentials(unsupported type) expected error, got nil")
	}

	// ctx 被取消
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := p.ValidateCredentials(canceledCtx, types.Credentials{
		Type:            types.AuthTypeAKSK,
		AccessKeyID:     "key",
		AccessKeySecret: "secret",
	}); !errors.Is(err, context.Canceled) {
		t.Errorf("ValidateCredentials(canceled ctx) expected context.Canceled, got %v", err)
	}
}

func TestValidateCredentialsAcceptsValid(t *testing.T) {
	p := NewAliyunProvider()
	ctx := context.Background()

	validCases := []types.Credentials{
		{Type: types.AuthTypeAKSK, AccessKeyID: "key", AccessKeySecret: "secret"},
		{Type: types.AuthTypeAKSK, AccessKeyID: "key", AccessKeySecret: "secret", Region: "cn-hangzhou"},
		{Type: types.AuthTypeOIDC, OIDCToken: "token"},
		{Type: types.AuthTypeOIDC, OIDCToken: "token", RoleARN: "arn"},
	}
	for i, c := range validCases {
		if err := p.ValidateCredentials(ctx, c); err != nil {
			t.Errorf("case %d: ValidateCredentials(valid) unexpected error: %v", i, err)
		}
	}
}

func TestListRegionsNonEmpty(t *testing.T) {
	p := NewAliyunProvider()
	ctx := context.Background()

	regions, err := p.ListRegions(ctx, types.Credentials{})
	if err != nil {
		t.Fatalf("ListRegions() unexpected error: %v", err)
	}
	if len(regions) == 0 {
		t.Error("ListRegions() expected non-empty list, got empty")
	}
	for _, r := range regions {
		if r.ID == "" {
			t.Error("ListRegions() region with empty ID")
		}
	}
}

func TestDiscoverMethodsReturnEmptyNotError(t *testing.T) {
	p := NewAliyunProvider()
	ctx := context.Background()

	vpcs, err := p.DiscoverVPCs(ctx, types.Credentials{}, "cn-hangzhou")
	if err != nil {
		t.Errorf("DiscoverVPCs() unexpected error: %v", err)
	}
	if len(vpcs) != 0 {
		t.Errorf("DiscoverVPCs() expected empty slice, got %d", len(vpcs))
	}

	subnets, err := p.DiscoverSubnets(ctx, types.Credentials{}, "cn-hangzhou", "vpc-123")
	if err != nil {
		t.Errorf("DiscoverSubnets() unexpected error: %v", err)
	}
	if len(subnets) != 0 {
		t.Errorf("DiscoverSubnets() expected empty slice, got %d", len(subnets))
	}

	instances, err := p.DiscoverInstances(ctx, types.Credentials{}, "cn-hangzhou")
	if err != nil {
		t.Errorf("DiscoverInstances() unexpected error: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("DiscoverInstances() expected empty slice, got %d", len(instances))
	}

	clusters, err := p.DiscoverClusters(ctx, types.Credentials{}, "cn-hangzhou")
	if err != nil {
		t.Errorf("DiscoverClusters() unexpected error: %v", err)
	}
	if len(clusters) != 0 {
		t.Errorf("DiscoverClusters() expected empty slice, got %d", len(clusters))
	}
}

func TestCreateResourceReturnsErrNotImplemented(t *testing.T) {
	p := NewAliyunProvider()
	ctx := context.Background()

	_, err := p.CreateResource(ctx, types.Credentials{}, types.CreateResourceRequest{})
	if !errors.Is(err, types.ErrNotImplemented) {
		t.Errorf("CreateResource() expected ErrNotImplemented, got %v", err)
	}
}

func TestDeleteResourceReturnsErrNotImplemented(t *testing.T) {
	p := NewAliyunProvider()
	ctx := context.Background()

	err := p.DeleteResource(ctx, types.Credentials{}, "resource-123")
	if !errors.Is(err, types.ErrNotImplemented) {
		t.Errorf("DeleteResource() expected ErrNotImplemented, got %v", err)
	}
}

func TestSyncTagsReturnsErrNotImplemented(t *testing.T) {
	p := NewAliyunProvider()
	ctx := context.Background()

	err := p.SyncTags(ctx, types.Credentials{}, "resource-123", map[string]string{"env": "test"})
	if !errors.Is(err, types.ErrNotImplemented) {
		t.Errorf("SyncTags() expected ErrNotImplemented, got %v", err)
	}
}
