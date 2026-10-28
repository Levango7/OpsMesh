package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %T: %v (json=%s)", v, err, raw)
	}
	return out
}

func assertKeys(t *testing.T, obj map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("键集合 = %v, want %v", got, want)
	}
}

// roundTrip 走真实的 JSON 编解码，断言结构体等价——这些类型是适配器与
// API 之间的传输契约，字段可读性比 Go 内部表示更重要。
func roundTrip[T any](t *testing.T, in T) T {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v (json=%s)", err, raw)
	}
	return out
}

func TestConstants_LiteralValues(t *testing.T) {
	providers := map[string]ProviderType{
		"aliyun":  ProviderTypeAliyun,
		"aws":     ProviderTypeAWS,
		"azure":   ProviderTypeAzure,
		"tencent": ProviderTypeTencent,
		"huawei":  ProviderTypeHuawei,
	}
	for want, got := range providers {
		if string(got) != want {
			t.Fatalf("ProviderType 字面值 = %q, want %q（持久化/API 契约）", got, want)
		}
	}

	authTypes := map[string]AuthType{
		"aksk":             AuthTypeAKSK,
		"oidc":             AuthTypeOIDC,
		"managed_identity": AuthTypeManagedIdentity,
	}
	for want, got := range authTypes {
		if string(got) != want {
			t.Fatalf("AuthType 字面值 = %q, want %q（持久化/API 契约）", got, want)
		}
	}

	if len(providers) != 5 || len(authTypes) != 3 {
		t.Fatalf("常量数量变化（providers=%d authTypes=%d），请同步本测试与文档", len(providers), len(authTypes))
	}
}

func TestErrNotImplemented_IdentityAndMessage(t *testing.T) {
	if got := ErrNotImplemented.Error(); got != "not implemented" {
		t.Fatalf("ErrNotImplemented.Error() = %q, want %q", got, "not implemented")
	}
	if !errors.Is(ErrNotImplemented, ErrNotImplemented) {
		t.Fatal("errors.Is(ErrNotImplemented, ErrNotImplemented) = false")
	}

	wrapped := fmt.Errorf("aliyun: %w", ErrNotImplemented)
	if !errors.Is(wrapped, ErrNotImplemented) {
		t.Fatal("包装后的错误未能通过 errors.Is 匹配 ErrNotImplemented")
	}

	// 同文案的新错误不是同一个 sentinel：适配器必须返回 ErrNotImplemented 本身，
	// 否则调用方的 errors.Is 判定会失效。
	if errors.Is(errors.New("not implemented"), ErrNotImplemented) {
		t.Fatal("同文案的新错误被误判为 ErrNotImplemented（应仅按同一性匹配）")
	}
}

func TestCloudProviderMethodSet(t *testing.T) {
	iface := reflect.TypeOf((*CloudProvider)(nil)).Elem()
	got := make([]string, 0, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		got = append(got, iface.Method(i).Name)
	}
	slices.Sort(got)

	// 接口缩面时各厂商仍能编译，故用反射把方法集钉死；扩面同理。
	want := []string{
		"CreateResource",
		"DeleteResource",
		"DiscoverClusters",
		"DiscoverInstances",
		"DiscoverSubnets",
		"DiscoverVPCs",
		"ListRegions",
		"ProviderType",
		"SyncTags",
		"ValidateCredentials",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("CloudProvider 方法集 = %v, want %v", got, want)
	}
}

func TestCredentials_JSONContract(t *testing.T) {
	t.Run("all fields round-trip", func(t *testing.T) {
		in := Credentials{
			Type:            AuthTypeAKSK,
			AccessKeyID:     "ak-id",
			AccessKeySecret: "ak-secret",
			OIDCToken:       "oidc-token",
			RoleARN:         "acs:ram::123:role/ops",
			Region:          "cn-hangzhou",
		}
		obj := marshalToMap(t, in)
		assertKeys(t, obj, "type", "accessKeyId", "accessKeySecret", "oidcToken", "roleArn", "region")
		if obj["type"] != "aksk" || obj["region"] != "cn-hangzhou" {
			t.Fatalf("字段值失真: %v", obj)
		}
		if got := roundTrip(t, in); !reflect.DeepEqual(got, in) {
			t.Fatalf("往返不等价: got %+v, want %+v", got, in)
		}
	})

	t.Run("optional fields omitted but region always present", func(t *testing.T) {
		// Region 与 Type 没有 omitempty：序列化后恒在（空串也出现），
		// 下游据此可直接读取而不必判空。
		obj := marshalToMap(t, Credentials{Type: AuthTypeManagedIdentity})
		assertKeys(t, obj, "type", "region")
		if obj["region"] != "" {
			t.Fatalf("region = %#v, want 空字符串仍出现", obj["region"])
		}
	})

	t.Run("unknown auth type preserved", func(t *testing.T) {
		// 本包只承载数据，不做枚举校验；校验属于适配器职责。
		in := Credentials{Type: AuthType("future_type"), Region: "r"}
		if got := roundTrip(t, in); got.Type != in.Type {
			t.Fatalf("未知 AuthType 被改写: %q", got.Type)
		}
	})
}

func TestResource_JSONContract(t *testing.T) {
	t.Run("minimal resource omits optional fields", func(t *testing.T) {
		in := Resource{
			ID:           "res-1",
			Provider:     ProviderTypeAWS,
			Region:       "us-east-1",
			ResourceType: "instance",
			ResourceID:   "i-123",
			ResourceName: "web-01",
			Status:       "running",
		}
		obj := marshalToMap(t, in)
		assertKeys(t, obj, "id", "provider", "region", "resourceType", "resourceId", "resourceName", "status")
		if obj["provider"] != "aws" {
			t.Fatalf("provider = %v, want aws", obj["provider"])
		}
	})

	t.Run("full resource round-trip", func(t *testing.T) {
		in := Resource{
			ID:           "res-1",
			Provider:     ProviderTypeHuawei,
			Region:       "cn-north-4",
			ResourceType: "vpc",
			ResourceID:   "vpc-1",
			ResourceName: "prod-vpc",
			Status:       "available",
			PrivateIP:    "10.0.0.2",
			PublicIP:     "1.2.3.4",
			Tags:         map[string]string{"env": "prod", "team": "sre"},
			CreatedAt:    "2026-10-27T03:25:00+08:00",
			UpdatedAt:    "2026-10-28T20:00:00.123456+08:00",
		}
		obj := marshalToMap(t, in)
		assertKeys(t, obj, "id", "provider", "region", "resourceType", "resourceId",
			"resourceName", "status", "privateIp", "publicIp", "tags", "createdAt", "updatedAt")

		got := roundTrip(t, in)
		if !reflect.DeepEqual(got, in) {
			t.Fatalf("往返不等价: got %+v, want %+v", got, in)
		}
		// 时间字段是厂商返回的不透明字符串，本包不得解析/归一化。
		if got.CreatedAt != in.CreatedAt || got.UpdatedAt != in.UpdatedAt {
			t.Fatalf("时间字符串被改写: got %q/%q, want %q/%q",
				got.CreatedAt, got.UpdatedAt, in.CreatedAt, in.UpdatedAt)
		}
	})

	t.Run("empty tags omitted", func(t *testing.T) {
		obj := marshalToMap(t, Resource{ID: "r", Tags: map[string]string{}})
		if _, ok := obj["tags"]; ok {
			t.Fatalf("空 tags 不应出现: %v", obj)
		}
	})
}

func TestRegion_JSONContract(t *testing.T) {
	t.Run("minimal region", func(t *testing.T) {
		obj := marshalToMap(t, Region{ID: "cn-hangzhou", Name: "华东1（杭州）"})
		assertKeys(t, obj, "id", "name")
	})

	t.Run("region with endpoint", func(t *testing.T) {
		in := Region{ID: "cn-hangzhou", Name: "华东1（杭州）", Endpoint: "ecs.cn-hangzhou.aliyuncs.com"}
		obj := marshalToMap(t, in)
		assertKeys(t, obj, "id", "name", "endpoint")
		if got := roundTrip(t, in); got != in {
			t.Fatalf("往返不等价: got %+v, want %+v", got, in)
		}
	})
}

func TestCreateResourceRequest_JSONContract(t *testing.T) {
	t.Run("minimal request omits maps", func(t *testing.T) {
		obj := marshalToMap(t, CreateResourceRequest{
			ResourceType: "instance",
			Region:       "us-east-1",
			Name:         "web-02",
		})
		assertKeys(t, obj, "resourceType", "region", "name")
	})

	t.Run("full request round-trip", func(t *testing.T) {
		in := CreateResourceRequest{
			ResourceType: "instance",
			Region:       "us-east-1",
			Name:         "web-02",
			Properties:   map[string]string{"instanceType": "t3.large", "imageId": "ami-1"},
			Tags:         map[string]string{"env": "prod"},
		}
		obj := marshalToMap(t, in)
		assertKeys(t, obj, "resourceType", "region", "name", "properties", "tags")
		if got := roundTrip(t, in); !reflect.DeepEqual(got, in) {
			t.Fatalf("往返不等价: got %+v, want %+v", got, in)
		}
	})
}
