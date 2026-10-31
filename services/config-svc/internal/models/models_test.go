package models

import (
	"reflect"
	"testing"
	"time"
)

// SecretMeta 是对外结构（不含明文），SecretEntry 是内部结构（含明文）。
// 两者字段集一旦混淆，明文密钥会随响应体泄露。
func TestSecretValueIsInternalOnly(t *testing.T) {
	if _, ok := reflect.TypeOf(SecretMeta{}).FieldByName("Value"); ok {
		t.Fatal("SecretMeta 不应含 Value 字段：它是对外结构，带明文即泄露")
	}
	if _, ok := reflect.TypeOf(SecretEntry{}).FieldByName("Value"); !ok {
		t.Fatal("SecretEntry 必须保留 Value 字段：内部解密路径依赖它")
	}
}

// 配置项/通知渠道/模板的基础字段契约（服务层按这些字段名读写）。
func TestEntryFieldContracts(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		want []string
	}{
		{"ConfigEntry", reflect.TypeOf(ConfigEntry{}), []string{"ID", "TenantID", "Key", "Value", "Format", "Version", "UpdatedBy", "CreatedAt", "UpdatedAt"}},
		{"ChannelEntry", reflect.TypeOf(ChannelEntry{}), []string{"ID", "TenantID", "Name", "Type", "Config", "Enabled", "CreatedAt", "UpdatedAt"}},
		{"TemplateEntry", reflect.TypeOf(TemplateEntry{}), []string{"ID", "TenantID", "Name", "Content", "Variables", "CreatedAt", "UpdatedAt"}},
	}
	for _, tc := range cases {
		for _, field := range tc.want {
			if _, ok := tc.typ.FieldByName(field); !ok {
				t.Fatalf("%s 缺少字段 %s", tc.name, field)
			}
		}
	}
}

// Variables 的默认值必须是可变映射（模板渲染会读取多个变量）。
func TestTemplateVariablesType(t *testing.T) {
	f, ok := reflect.TypeOf(TemplateEntry{}).FieldByName("Variables")
	if !ok {
		t.Fatal("TemplateEntry 缺少 Variables 字段")
	}
	if f.Type != reflect.TypeOf(map[string]string{}) {
		t.Fatalf("Variables 类型 = %v, want map[string]string", f.Type)
	}
}

func TestTimestampsAreTime(t *testing.T) {
	f, _ := reflect.TypeOf(ConfigEntry{}).FieldByName("CreatedAt")
	if f.Type != reflect.TypeOf(time.Time{}) {
		t.Fatalf("CreatedAt 类型 = %v, want time.Time", f.Type)
	}
}
