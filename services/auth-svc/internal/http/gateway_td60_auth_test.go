// gateway_td60_auth_test.go 覆盖 TD-60 auth 切流准备项：
//   - A-3：GET /api/v1/users 的 opt-in 分页 + 租户隔离（对齐 controlplane）
//   - A-2：非 active 账号登录的 403/文案 + 口令优先顺序（防用户名枚举）
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
)

// createTD60User 建一个 active 用户（管理端建号路径同语义），返回 store 中的用户名。
func createTD60User(t *testing.T, svc *service.Service, username, password, tenant string, roles []string) {
	t.Helper()
	if _, err := svc.CreateUser(context.Background(), &authv1.CreateUserRequest{
		Username: username,
		Password: password,
		TenantId: tenant,
		RoleIds:  roles,
	}); err != nil {
		t.Fatalf("建用户 %s 失败: %v", username, err)
	}
}

// setTD60UserStatus 直接改写用户状态（审批流之外的测试夹具）。
func setTD60UserStatus(t *testing.T, svc *service.Service, username, status string) {
	t.Helper()
	u := svc.Store().GetUserByUsername(username)
	if u == nil {
		t.Fatalf("夹具失败：用户 %s 不存在", username)
	}
	u.Status = status
	if err := svc.Store().UpdateUser(u); err != nil {
		t.Fatalf("更新用户 %s 状态失败: %v", username, err)
	}
}

// loginTD60 用任意用户名/口令登录并返回 Cookie 头（成功才返回）。
func loginTD60(t *testing.T, mux *http.ServeMux, username, password string) map[string]string {
	t.Helper()
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录 %s 失败: %d body=%s", username, rec.Code, rec.Body.String())
	}
	at := findCookie(t, rec, "opsmesh_at")
	rt := findCookie(t, rec, "opsmesh_rt")
	if at == nil {
		t.Fatalf("登录 %s 未写 opsmesh_at", username)
	}
	header := "opsmesh_at=" + at.Value
	if rt != nil {
		header += "; opsmesh_rt=" + rt.Value
	}
	return map[string]string{"Cookie": header}
}

type usersPageEnvelope struct {
	Users    []map[string]any `json:"users"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
	HasMore  bool             `json:"hasMore"`
}

func decodeUsersEnvelope(t *testing.T, body []byte) usersPageEnvelope {
	t.Helper()
	var env usersPageEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("分页信封解析失败: %v body=%s", err, string(body))
	}
	return env
}

// TestListUsers_NoParamsKeepsLegacyShape：无 page/pageSize 时保持 {users:[…]}，
// 不出现分页键（byte-compatible 的键集合断言）。
func TestListUsers_NoParamsKeepsLegacyShape(t *testing.T) {
	_, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	rec := doReq(t, mux, http.MethodGet, "/api/v1/users", "", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET users: got %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("无参数响应键集合应只有 users，实际 keys=%v body=%s", keysOf(raw), rec.Body.String())
	}
	if _, ok := raw["users"]; !ok {
		t.Fatalf("无参数响应缺 users 键: %s", rec.Body.String())
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestListUsers_PaginationEnvelopeAndBoundaries：page/pageSize opt-in 信封与边界。
func TestListUsers_PaginationEnvelopeAndBoundaries(t *testing.T) {
	_, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)
	for _, name := range []string{"pg-a", "pg-b", "pg-c", "pg-d"} {
		createTD60User(t, svc, name, "SomePass123!x", "", nil)
	}
	const total = 5 // 4 个夹具 + seed admin

	t.Run("first page", func(t *testing.T) {
		rec := doReq(t, mux, http.MethodGet, "/api/v1/users?page=1&pageSize=2", "", cookies)
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d body=%s", rec.Code, rec.Body.String())
		}
		env := decodeUsersEnvelope(t, rec.Body.Bytes())
		if env.Total != total || env.Page != 1 || env.PageSize != 2 || !env.HasMore || len(env.Users) != 2 {
			t.Fatalf("首屏信封不符: %+v (want total=%d page=1 size=2 hasMore=true len=2)", env, total)
		}
	})

	t.Run("last page", func(t *testing.T) {
		rec := doReq(t, mux, http.MethodGet, "/api/v1/users?page=3&pageSize=2", "", cookies)
		env := decodeUsersEnvelope(t, rec.Body.Bytes())
		if env.Total != total || env.Page != 3 || env.PageSize != 2 || env.HasMore || len(env.Users) != 1 {
			t.Fatalf("末页信封不符: %+v", env)
		}
	})

	t.Run("out of range page is empty array not null", func(t *testing.T) {
		rec := doReq(t, mux, http.MethodGet, "/api/v1/users?page=99&pageSize=2", "", cookies)
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d body=%s", rec.Code, rec.Body.String())
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("响应不是 JSON: %v", err)
		}
		if string(raw["users"]) != "[]" {
			t.Fatalf("越界页 users 应为 []，got %s", string(raw["users"]))
		}
		env := decodeUsersEnvelope(t, rec.Body.Bytes())
		if env.HasMore {
			t.Fatal("越界页 hasMore 应为 false")
		}
	})

	t.Run("pageSize capped at 200", func(t *testing.T) {
		rec := doReq(t, mux, http.MethodGet, "/api/v1/users?pageSize=500", "", cookies)
		env := decodeUsersEnvelope(t, rec.Body.Bytes())
		if env.Page != 1 || env.PageSize != 200 {
			t.Fatalf("pageSize 上限不符: %+v", env)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		rec := doReq(t, mux, http.MethodGet, "/api/v1/users?page=2", "", cookies)
		env := decodeUsersEnvelope(t, rec.Body.Bytes())
		if env.Page != 2 || env.PageSize != 20 || len(env.Users) != 0 || env.HasMore {
			t.Fatalf("仅 page 时应默认 pageSize=20: %+v", env)
		}
		rec = doReq(t, mux, http.MethodGet, "/api/v1/users?pageSize=2", "", cookies)
		env = decodeUsersEnvelope(t, rec.Body.Bytes())
		if env.Page != 1 || env.PageSize != 2 || len(env.Users) != 2 || !env.HasMore {
			t.Fatalf("仅 pageSize 时应默认 page=1: %+v", env)
		}
		// page<1 / pageSize<1 与 controlplane 同口径：clamp 到 1 / 回退 20。
		rec = doReq(t, mux, http.MethodGet, "/api/v1/users?page=0&pageSize=0", "", cookies)
		env = decodeUsersEnvelope(t, rec.Body.Bytes())
		if env.Page != 1 || env.PageSize != 20 {
			t.Fatalf("非正参数应 clamp/回退: %+v", env)
		}
	})

	t.Run("invalid values are 400", func(t *testing.T) {
		for _, q := range []string{"page=abc", "pageSize=abc", "page=1.5&pageSize=2"} {
			rec := doReq(t, mux, http.MethodGet, "/api/v1/users?"+q, "", cookies)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s 应 400，实际 %d body=%s", q, rec.Code, rec.Body.String())
				continue
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != "invalid page/pageSize" {
				t.Errorf("%s 错误体不符: %s", q, rec.Body.String())
			}
		}
	})
}

// TestListUsers_TenantScopingPreserved：非平台租户只看到本租户用户（含分页 total），
// 平台租户保持全量视图——与 controlplane auth_users.go 的 P0-6 口径一致。
func TestListUsers_TenantScopingPreserved(t *testing.T) {
	_, mux, svc := newTestGateway()
	createTD60User(t, svc, "tenant-a-admin", "SomePass123!x", "tenant-a", []string{"role-admin"})
	createTD60User(t, svc, "tenant-a-user", "SomePass123!x", "tenant-a", nil)
	createTD60User(t, svc, "tenant-b-user", "SomePass123!x", "tenant-b", nil)

	aCookies := loginTD60(t, mux, "tenant-a-admin", "SomePass123!x")

	// 旧形态（无参数）同样受租户隔离。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/users", "", aCookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant-a 列表: got %d body=%s", rec.Code, rec.Body.String())
	}
	var legacy struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if len(legacy.Users) != 2 {
		t.Fatalf("tenant-a 应只见 2 个本租户用户，实际 %d：%v", len(legacy.Users), usernames(legacy.Users))
	}
	assertAllTenant(t, legacy.Users, "tenant-a")

	// 分页信封的 total 必须是隔离后的总数，而非全局总数。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/users?page=1&pageSize=10", "", aCookies)
	env := decodeUsersEnvelope(t, rec.Body.Bytes())
	if env.Total != 2 || len(env.Users) != 2 || env.HasMore {
		t.Fatalf("tenant-a 分页信封不符: %+v users=%v", env, usernames(env.Users))
	}
	assertAllTenant(t, env.Users, "tenant-a")

	// 平台租户（default）保持全量：seed admin + 3 个夹具。
	adminCookies := loginAsAdmin(t, mux, svc)
	rec = doReq(t, mux, http.MethodGet, "/api/v1/users?page=1&pageSize=200", "", adminCookies)
	env = decodeUsersEnvelope(t, rec.Body.Bytes())
	if env.Total != 4 {
		t.Fatalf("平台租户应见全量 4 个用户，实际 total=%d users=%v", env.Total, usernames(env.Users))
	}
}

func usernames(users []map[string]any) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		name, _ := u["username"].(string)
		out = append(out, name)
	}
	return out
}

func assertAllTenant(t *testing.T, users []map[string]any, tenant string) {
	t.Helper()
	for _, u := range users {
		if got, _ := u["tenantId"].(string); got != tenant {
			t.Errorf("用户 %v 租户 = %q, want %q", u["username"], got, tenant)
		}
	}
}

// TestLogin_NonActiveStatusForbidden：口令正确时按状态返回 403 + controlplane 原文案。
func TestLogin_NonActiveStatusForbidden(t *testing.T) {
	_, mux, svc := newTestGateway()
	cases := []struct{ status, want string }{
		{"pending", "account pending admin approval"},
		{"disabled", "account disabled"},
		{"rejected", "account registration rejected"},
		{"suspended", "account not active"},
	}
	for _, tc := range cases {
		name := "status-" + tc.status
		createTD60User(t, svc, name, "SomePass123!x", "", nil)
		setTD60UserStatus(t, svc, name, tc.status)

		rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
			`{"username":"`+name+`","password":"SomePass123!x"}`, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s 登录应 403，实际 %d body=%s", tc.status, rec.Code, rec.Body.String())
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s 响应不是 JSON: %v", tc.status, err)
			continue
		}
		if body["error"] != tc.want {
			t.Errorf("%s 文案 = %q, want %q", tc.status, body["error"], tc.want)
		}
		if findCookie(t, rec, "opsmesh_at") != nil {
			t.Errorf("%s 登录被拒后不得下发会话 Cookie", tc.status)
		}
	}
}

// TestLogin_NonActiveWrongPasswordStays401：状态只在口令正确后揭示。
// 口令错误的 disabled 账号与不存在用户必须同为 401 + 同一文案（防枚举）。
func TestLogin_NonActiveWrongPasswordStays401(t *testing.T) {
	_, mux, svc := newTestGateway()
	createTD60User(t, svc, "disabled-u", "SomePass123!x", "", nil)
	setTD60UserStatus(t, svc, "disabled-u", "disabled")

	wrong := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"disabled-u","password":"wrong-password"}`, nil)
	ghost := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"ghost-user","password":"wrong-password"}`, nil)
	if wrong.Code != http.StatusUnauthorized || ghost.Code != http.StatusUnauthorized {
		t.Fatalf("口令错误应为 401：disabled=%d ghost=%d", wrong.Code, ghost.Code)
	}
	if wrong.Body.String() != ghost.Body.String() {
		t.Fatalf("口令错误响应体必须一致（防枚举）：disabled=%s ghost=%s", wrong.Body.String(), ghost.Body.String())
	}
	if wrong.Body.String() != `{"error":"invalid credentials"}`+"\n" {
		t.Fatalf("口令错误文案不符: %s", wrong.Body.String())
	}

	// 正确口令才看到状态（403 disabled）。
	correct := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"disabled-u","password":"SomePass123!x"}`, nil)
	if correct.Code != http.StatusForbidden {
		t.Fatalf("正确口令 + disabled 应 403，实际 %d body=%s", correct.Code, correct.Body.String())
	}
}
