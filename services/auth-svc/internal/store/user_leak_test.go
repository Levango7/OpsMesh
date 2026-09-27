// user_leak_test.go P0 安全回归：PasswordHash 不得出现在任何 JSON 输出中。
//
// 背景：store.User 的 PasswordHash 字段此前缺少 `json:"-"` 序列化屏蔽，而
// handleUserDetail(GET) 与 handleUpdateUser 直接把该结构体交给 writeJSON，
// 导致任何持有 user:read 的调用方都能批量拉取 cost-12 bcrypt 哈希用于离线爆破。
package store

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 底层结构体直接序列化时不得带出密码哈希。
func TestUser_JSONNeverExposesPasswordHash(t *testing.T) {
	u := &User{
		ID:                 "u-1",
		Username:           "admin",
		Email:              "admin@example.com",
		PasswordHash:       "$2a$12$abcdefghijklmnopqrstuv",
		Status:             "active",
		RoleIDs:            []string{"r-1"},
		CreatedAt:          time.Unix(1700000000, 0).UTC(),
		MustChangePassword: true,
	}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	if strings.Contains(got, "PasswordHash") || strings.Contains(got, "passwordHash") {
		t.Fatalf("JSON 中出现 PasswordHash 字段名: %s", got)
	}
	// 连哈希值本身（$2a$ 前缀）也不能出现。
	if strings.Contains(got, "$2a$") || strings.Contains(got, "$2b$") {
		t.Fatalf("JSON 中出现 bcrypt 哈希值: %s", got)
	}
	// 合法字段仍需保留，避免"修过头"把接口打坏。
	// 注意：store.User 只有 PasswordHash 带了 json tag，其余字段按 Go 字段名序列化
	// （网关侧的对外响应统一走 toPublicStoreUser，那里才是 camelCase）。
	for _, want := range []string{`"ID"`, `"Username"`, `"Email"`, `"Status"`, `"RoleIDs"`, `"MustChangePassword"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON 缺少合法字段 %s: %s", want, got)
		}
	}
}

// 反射层面的结构性保证：字段必须带 json:"-" 标签。
// 这样即使将来有人改了序列化路径，测试也会先失败。
func TestUser_PasswordHashCarriesJSONDashTag(t *testing.T) {
	tp := reflect.TypeOf(User{})
	f, ok := tp.FieldByName("PasswordHash")
	if !ok {
		t.Fatal("store.User 上找不到 PasswordHash 字段")
	}
	if got := f.Tag.Get("json"); got != "-" {
		t.Fatalf(`PasswordHash 的 json 标签 = %q，期望 "-"，否则直接序列化会泄露哈希`, got)
	}
}
