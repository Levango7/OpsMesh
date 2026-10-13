// gateway_field_names_test.go — F6 字段命名收口守卫（板第七十九则，2026-10-13）。
//
// 规范名 = controlplane 既有契约（也是前端已在用的形态）：
//
//	· 请求 role_ids——auth_users.go:110/303 的 json tag，前端 UsersView 也按此发；
//	· 响应 roleIDs / createdAt / mustChangePassword——store.User 的 json tag，前端按此读。
//
// auth-svc 的历史拼写保留为**过渡别名**（create: roleIDs；update: roleIds；list: role_ids/
// created_at/must_change_password），双轨期零断裂；切流完成后可删——删时同步删本文几条
// 别名断言与 gateway.go 里对应注释。
//
// 为什么必须有这些守卫（裂缝的三种形态，各配一条）：
//
//	① **静默忽略**（最危险）：修复前 PUT 只认 roleIds，而控制面与前端发的是 role_ids
//	   ⇒ 切流后"改角色"返回 200 但字段没动——不看断言看不出来；
//	② 键名漂移：列表此前直出 proto 结构，role_ids 而非 roleIDs ⇒ 前端角色列空白；
//	③ 类型漂移：同一处的 created_at 是 {"seconds":…} 对象而非 RFC3339 ⇒ 前端时间列
//	   Invalid Date——所以这里按**类型**断言，不只看键名。
package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// f6Register 注册一个测试用户（默认 role-viewer）并返回其 ID。
// 复用既有 TestUpdateUser_AdminCanUpdateFields 的注册形态（本网关测试里 register 可用）。
func f6Register(t *testing.T, mux *http.ServeMux, name string) string {
	t.Helper()
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"`+name+`","password":"SomePass123!x","email":"`+name+`@x.io"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register %s: got %d, body=%s", name, rec.Code, rec.Body.String())
	}
	return name
}

// TestUpdateUser_AcceptsCanonicalRoleIds 前沿形态①：前端/控制面发 role_ids，角色必须真的改。
func TestUpdateUser_AcceptsCanonicalRoleIds(t *testing.T) {
	g, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)
	name := f6Register(t, mux, "f6put")

	before := g.svc.Store().GetUserByUsername(name).RoleIDs
	rec := doReq(t, mux, http.MethodPut, "/api/v1/users/"+g.svc.Store().GetUserByUsername(name).ID,
		`{"role_ids":["role-admin"]}`, cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT role_ids: got %d, body=%s", rec.Code, rec.Body.String())
	}
	after := g.svc.Store().GetUserByUsername(name).RoleIDs
	if len(after) != 1 || after[0] != "role-admin" {
		t.Fatalf("role_ids 未生效（静默忽略形态）：before=%v after=%v", before, after)
	}
	if len(before) == 1 && before[0] == "role-admin" {
		t.Fatal("夹具失效：before 已是 role-admin，无法证明是本次写入的")
	}
	// 响应体带规范名（前端读 roleIDs 刷新行）。
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("PUT 响应不是 JSON：%v", err)
	}
	if _, ok := body["roleIDs"]; !ok {
		t.Fatalf("PUT 响应缺规范名 roleIDs：%s", rec.Body.String())
	}
}

// TestUpdateUser_AcceptsLegacyRoleIdsAlias 过渡别名：旧拼写 roleIds 仍生效（双轨零断裂）。
func TestUpdateUser_AcceptsLegacyRoleIdsAlias(t *testing.T) {
	g, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)
	name := f6Register(t, mux, "f6alias")
	id := g.svc.Store().GetUserByUsername(name).ID

	rec := doReq(t, mux, http.MethodPut, "/api/v1/users/"+id,
		`{"roleIds":["role-admin"]}`, cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT roleIds（别名）: got %d, body=%s", rec.Code, rec.Body.String())
	}
	after := g.svc.Store().GetUserByUsername(name).RoleIDs
	if len(after) != 1 || after[0] != "role-admin" {
		t.Fatalf("旧拼写 roleIds 不再生效（过渡期断裂）：after=%v", after)
	}
}

// TestAdminCreateUser_AcceptsCanonicalRoleIds 建号侧：role_ids 必须真的指派角色。
func TestAdminCreateUser_AcceptsCanonicalRoleIds(t *testing.T) {
	g, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/users",
		`{"username":"f6create","password":"SomePass123!x","email":"f6c@x.io","role_ids":["role-admin"]}`, cookies)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST role_ids: got %d, body=%s", rec.Code, rec.Body.String())
	}
	u := g.svc.Store().GetUserByUsername("f6create")
	if u == nil || len(u.RoleIDs) != 1 || u.RoleIDs[0] != "role-admin" {
		t.Fatalf("role_ids 建号未生效（角色被丢）：%+v", u)
	}
}

// TestListUsers_EmitsControlplaneFieldNames 形态②③：列表项键名与类型都必须对齐前端读取面。
func TestListUsers_EmitsControlplaneFieldNames(t *testing.T) {
	_, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)
	f6Register(t, mux, "f6list")

	rec := doReq(t, mux, http.MethodGet, "/api/v1/users", "", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET users: got %d", rec.Code)
	}
	var body struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("列表响应不是 JSON：%v", err)
	}
	if len(body.Users) == 0 {
		t.Fatal("列表为空（夹具失效）")
	}
	item := body.Users[0]
	// 规范名：前端 UsersView 的「角色」列读 roleIDs、「创建时间」列读 createdAt。
	if _, ok := item["roleIDs"]; !ok {
		t.Fatalf("列表项缺规范名 roleIDs（前端角色列会空白）：%v", item)
	}
	created, ok := item["createdAt"]
	if !ok {
		t.Fatalf("列表项缺规范名 createdAt（前端时间列会空）：%v", item)
	}
	if _, isString := created.(string); !isString {
		// 之前是 proto 的 {"seconds":…} 对象 ⇒ 前端 Invalid Date。按类型断言，别只看键名。
		t.Fatalf("createdAt 必须是 RFC3339 字符串（收到 %T：%v）", created, created)
	}
	// 过渡别名保留（双轨期零断裂；切流完成后本段与生产代码同步删）。
	if _, ok := item["role_ids"]; !ok {
		t.Fatalf("列表项缺过渡别名 role_ids：%v", item)
	}
}

// TestLogin_EmitsCanonicalRoleIDs 登录响应（前端 auth store 的用户对象来源）带规范名。
func TestLogin_EmitsCanonicalRoleIDs(t *testing.T) {
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"admin123"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		User map[string]any `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("登录响应不是 JSON：%v", err)
	}
	roles, ok := body.User["roleIDs"]
	if !ok {
		t.Fatalf("登录响应 user 缺规范名 roleIDs：%s", rec.Body.String())
	}
	arr, ok := roles.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("roleIDs 应为非空数组，收到 %T：%v", roles, roles)
	}
	if !strings.Contains(rec.Body.String(), "permissions") {
		t.Fatalf("登录响应应含 permissions（B4 契约）：%s", rec.Body.String())
	}
}
