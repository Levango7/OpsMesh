package cloudprovider

import (
	"context"
	"errors"
	"testing"

	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/types"
)

// TestErrNotImplemented_IsSameSentinel 钉住别名关系：调用方常用
// errors.Is(err, cloudprovider.ErrNotImplemented) 判断"能力未实现"，
// 若这里被改成独立 errors.New，所有这样的判断都会静默失配。
func TestErrNotImplemented_IsSameSentinel(t *testing.T) {
	// 这里要断言的正是"同一个 sentinel 实例"，用身份比较而非 errors.Is（后者会归一化包装链）。
	if ErrNotImplemented != types.ErrNotImplemented {
		t.Fatal("cloudprovider.ErrNotImplemented 与 types.ErrNotImplemented 必须是同一 sentinel")
	}
}

// TestTypeAliases 钉住向后兼容别名与 types 包常量/类型同源：
// 别名被换成独立定义时，跨包赋值与比较会悄悄失效。
func TestTypeAliases(t *testing.T) {
	consts := map[ProviderType]types.ProviderType{
		ProviderTypeAliyun:  types.ProviderTypeAliyun,
		ProviderTypeAWS:     types.ProviderTypeAWS,
		ProviderTypeAzure:   types.ProviderTypeAzure,
		ProviderTypeTencent: types.ProviderTypeTencent,
		ProviderTypeHuawei:     types.ProviderTypeHuawei,
		ProviderTypeVolcengine: types.ProviderTypeVolcengine,
	}
	for alias, want := range consts {
		if alias != want {
			t.Errorf("ProviderType 别名 %q 与 types 包常量 %q 不一致", alias, want)
		}
	}
	if AuthTypeAKSK != types.AuthTypeAKSK || AuthTypeOIDC != types.AuthTypeOIDC || AuthTypeManagedIdentity != types.AuthTypeManagedIdentity {
		t.Error("AuthType 别名与 types 包常量不一致")
	}
}

// TestNotImplementedProvider_AllMethodsReturnSentinel 逐个走完 9 个接口方法：
// 基线实现必须在每个能力上都返回 ErrNotImplemented，且不返回半成品数据
// （返回零资源 + nil error 会让调用方把"没实现"当成"没有资源"）。
func TestNotImplementedProvider_AllMethodsReturnSentinel(t *testing.T) {
	p := &NotImplementedProvider{}
	ctx := context.Background()
	creds := Credentials{}

	if err := p.ValidateCredentials(ctx, creds); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("ValidateCredentials() err = %v，期望 ErrNotImplemented", err)
	}
	if regions, err := p.ListRegions(ctx, creds); !errors.Is(err, ErrNotImplemented) || regions != nil {
		t.Errorf("ListRegions() = (%v, %v)，期望 (nil, ErrNotImplemented)", regions, err)
	}
	if rs, err := p.DiscoverVPCs(ctx, creds, "r"); !errors.Is(err, ErrNotImplemented) || rs != nil {
		t.Errorf("DiscoverVPCs() = (%v, %v)，期望 (nil, ErrNotImplemented)", rs, err)
	}
	if rs, err := p.DiscoverSubnets(ctx, creds, "r", "vpc"); !errors.Is(err, ErrNotImplemented) || rs != nil {
		t.Errorf("DiscoverSubnets() = (%v, %v)，期望 (nil, ErrNotImplemented)", rs, err)
	}
	if rs, err := p.DiscoverInstances(ctx, creds, "r"); !errors.Is(err, ErrNotImplemented) || rs != nil {
		t.Errorf("DiscoverInstances() = (%v, %v)，期望 (nil, ErrNotImplemented)", rs, err)
	}
	if rs, err := p.DiscoverClusters(ctx, creds, "r"); !errors.Is(err, ErrNotImplemented) || rs != nil {
		t.Errorf("DiscoverClusters() = (%v, %v)，期望 (nil, ErrNotImplemented)", rs, err)
	}
	if _, err := p.CreateResource(ctx, creds, CreateResourceRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("CreateResource() err = %v，期望 ErrNotImplemented", err)
	}
	if err := p.DeleteResource(ctx, creds, "id"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("DeleteResource() err = %v，期望 ErrNotImplemented", err)
	}
	if err := p.SyncTags(ctx, creds, "id", map[string]string{"k": "v"}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("SyncTags() err = %v，期望 ErrNotImplemented", err)
	}
}
