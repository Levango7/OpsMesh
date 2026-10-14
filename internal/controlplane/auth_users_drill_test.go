package controlplane

// auth_users_drill_test.go — 演练移交/待办项的守卫（2026-10-14，sim-team 报告
// §3.2/§3.3/§3.4）：
//   - §3.2 建号透传 mustChangePassword（单体 HTTP 面此前造不出首登强制改密账号，
//     而 auth-svc 侧支持——切流交接口令的关键机制两侧不对称）；
//   - §3.4 建号响应回读**落库对象**（此前回请求体，SQL 后端实测
//     createdAt:"0001-01-01T00:00:00Z" + permissions:null）；
//   - §3.3 PUT /users/{id} 未识别键 400（此前静默 200 no-op——"HTTP 面无法
//     授予角色"误报的根因）。
//
// 夹具沿用 auth_test.go 的 newAuthTestServer（预置 admin/admin123）。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newDrillUser 建一个普通账号并返回其 ID（供 §3.3 更新类用例使用）。
func newDrillUser(t *testing.T, s *Server, auth string, username string) string {
	t.Helper()
	body := `{"username":"` + username + `","password":"SomePass123!x","email":"` + username + `@x.io","role_ids":["role-viewer"]}`
	rec := doWithAuthRec(t, s, http.MethodPost, "/api/v1/users", auth, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("建号 %s: status=%d want 201; body=%s", username, rec.Code, rec.Body.String())
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析建号响应: %v; body=%s", err, rec.Body.String())
	}
	if out.ID == "" {
		t.Fatalf("建号响应缺 id：%s", rec.Body.String())
	}
	return out.ID
}

// doWithAuthRec 带认证发请求并返回 recorder（auth_test.go 的 loginAsAdmin 返回
// Authorization 头值；users 端点按 server_lifecycle 的注册形态直调 handler——
// 与既有 auth_test.go/tenant_users_test.go 同款，不造第二个入口）。
func doWithAuthRec(t *testing.T, s *Server, method, path string, auth string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	if strings.HasPrefix(path, "/api/v1/users/") {
		s.handleUserRouting(rec, req)
	} else {
		s.handleUsers(rec, req)
	}
	return rec
}

// TestCreateUser_MustChangePasswordPassthrough §3.2：请求体 mustChangePassword:true
// 必须落库（SQL/内存两条路径都由 store 承载，这里断言库内事实 + 响应回显）。
// camel 与 snake 两个拼写都收（双名策略）；snake 单写也要生效。
func TestCreateUser_MustChangePasswordPassthrough(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
	}{
		{"camel", `{"username":"mc-camel","password":"SomePass123!x","email":"a@x.io","role_ids":["role-viewer"],"mustChangePassword":true}`},
		{"snake", `{"username":"mc-snake","password":"SomePass123!x","email":"b@x.io","role_ids":["role-viewer"],"must_change_password":true}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newAuthTestServer(t)
			auth := loginAsAdmin(t, s)
			rec := doWithAuthRec(t, s, http.MethodPost, "/api/v1/users", auth, c.body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status=%d want 201; body=%s", rec.Code, rec.Body.String())
			}
			var out struct {
				ID                 string `json:"id"`
				MustChangePassword bool   `json:"mustChangePassword"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("解析响应: %v", err)
			}
			u := s.store.GetUser(out.ID)
			if u == nil {
				t.Fatalf("建号后库内查不到：%s", rec.Body.String())
			}
			if !u.MustChangePassword {
				t.Errorf("库内 MustChangePassword=false——**透传丢失**（§3.2 原状）；body=%s", rec.Body.String())
			}
			if !out.MustChangePassword {
				t.Errorf("响应未回显 mustChangePassword:true")
			}
		})
	}
}

// TestCreateUser_ResponseIsStoredObject §3.4：响应必须是**落库对象**——
// createdAt 非零（SQL 后端此前回请求体给出 0001-01-01）、permissions 非空
// （与 login 同规则展开；此前为 null）。
func TestCreateUser_ResponseIsStoredObject(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	rec := doWithAuthRec(t, s, http.MethodPost, "/api/v1/users", auth,
		`{"username":"shape-check","password":"SomePass123!x","email":"s@x.io","role_ids":["role-admin"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d want 201; body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID          string   `json:"id"`
		CreatedAt   string   `json:"createdAt"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if strings.HasPrefix(out.CreatedAt, "0001-01-01") {
		t.Errorf("createdAt 是零值——响应回的是请求体而非落库对象（§3.4 原状）")
	}
	if len(out.Permissions) == 0 {
		t.Errorf("permissions 为空——建号响应应按 authUserWithPerms 展开（此前 null）")
	}
	// 与库内一致（回读而非另造）。
	if u := s.store.GetUser(out.ID); u == nil || u.CreatedAt.IsZero() {
		t.Errorf("库内对象 createdAt 异常（回读口径失效）")
	}
}

// TestUpdateUser_UnknownFieldsRejected §3.3：未识别键 400（点名键 + 认可集），
// 而规范名与其过渡别名照常 200 生效。这条同时钉住"误报的根因"：拼错字段名
// （roles）绝不能再拿到 200 no-op。
func TestUpdateUser_UnknownFieldsRejected(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	uid := newDrillUser(t, s, auth, "upd-target")

	// 未识别键：400 且错误体点名该键与认可集。
	rec := doWithAuthRec(t, s, http.MethodPut, "/api/v1/users/"+uid, auth, `{"roles":["role-admin"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段 roles：status=%d want 400（静默 200 no-op 是 §3.3 原状）; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "roles") || !strings.Contains(rec.Body.String(), "role_ids") {
		t.Errorf("400 响应应点名未知键与认可集，实得 %s", rec.Body.String())
	}
	// 库内确实没被动（拒绝发生在写库之前）。
	if u := s.store.GetUser(uid); u == nil || len(u.RoleIDs) != 1 || u.RoleIDs[0] != "role-viewer" {
		t.Errorf("400 后库内角色不该被动，实得 %v", u.RoleIDs)
	}

	// 规范名 role_ids：200 且真的生效。
	rec = doWithAuthRec(t, s, http.MethodPut, "/api/v1/users/"+uid, auth, `{"role_ids":["role-operator"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("role_ids 更新：status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if u := s.store.GetUser(uid); u == nil || len(u.RoleIDs) != 1 || u.RoleIDs[0] != "role-operator" {
		t.Fatalf("role_ids 应生效为 role-operator，实得 %v", u.RoleIDs)
	}

	// 过渡别名 roleIDs（F6 双名）：200 且生效。
	rec = doWithAuthRec(t, s, http.MethodPut, "/api/v1/users/"+uid, auth, `{"roleIDs":["role-admin"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("roleIDs 别名：status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if u := s.store.GetUser(uid); u == nil || len(u.RoleIDs) != 1 || u.RoleIDs[0] != "role-admin" {
		t.Fatalf("roleIDs 别名应生效为 role-admin，实得 %v", u.RoleIDs)
	}

	// 多个未知键：报错里排序稳定（纯函数契约的可见面）。
	rec = doWithAuthRec(t, s, http.MethodPut, "/api/v1/users/"+uid, auth, `{"zzz":1,"aaa":2}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("多未知键：status=%d want 400", rec.Code)
	}
	iA := strings.Index(rec.Body.String(), "aaa")
	iZ := strings.Index(rec.Body.String(), "zzz")
	if iA < 0 || iZ < 0 || iA > iZ {
		t.Errorf("未知键应按字典序点名（aaa 在 zzz 前），实得 %s", rec.Body.String())
	}
}

// TestUnknownUserUpdateKeys 纯函数判据：空体/空对象⇒空；非法 JSON⇒error；
// 混合键⇒只返未知项且排序。
func TestUnknownUserUpdateKeys(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{"空体", "", nil, false},
		{"空白体", "   ", nil, false},
		{"空对象", "{}", nil, false},
		{"全认可", `{"email":"a@x.io","role_ids":["role-viewer"],"roleIDs":["role-viewer"],"status":"active","tenantId":"t1"}`, nil, false},
		{"一个未知", `{"roles":["role-admin"]}`, []string{"roles"}, false},
		{"多个未知排序", `{"zzz":1,"aaa":2,"mid":3}`, []string{"aaa", "mid", "zzz"}, false},
		{"混合", `{"email":"a@x.io","bogus":1}`, []string{"bogus"}, false},
		{"非法 JSON", `{"email":`, nil, true},
		{"JSON 数组", `[1,2]`, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := unknownUserUpdateKeys([]byte(c.raw))
			if c.wantErr {
				if err == nil {
					t.Fatalf("应返回 error，实得 got=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应出错: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("got=%v want=%v", got, c.want)
			}
		})
	}
}
