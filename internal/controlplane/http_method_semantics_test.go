package controlplane

// http_method_semantics_test.go — 路径分派器的 HTTP 方法语义守卫。
//
// 契约：路径存在但动词用错 ⇒ 405 Method Not Allowed + Allow 头（列出该子资源
// 支持的方法）；路径真正不存在 ⇒ 404。修复前四个分派器（tasks/devices/alerts/
// users）一律回 404，调用方无法区分"URL 拼错"与"方法用错"，运维脚本据 404
// 判资源不存在而做出错误处置。
//
// 另覆盖 GET /api/v1/users/{id}：200 单用户 JSON + 跨租户/不存在 404。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/store"
)

// TestSubResourceDispatchers_MethodSemantics 表驱动：四个分派器的 Allow 集与 404/405 分界。
func TestSubResourceDispatchers_MethodSemantics(t *testing.T) {
	type dispatchFunc func(s *Server, w http.ResponseWriter, r *http.Request)
	taskDispatch := func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleTaskRouting(w, r) }
	deviceDispatch := func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleDeviceRouting(w, r) }
	alertDispatch := func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleAlertRouting(w, r) }
	userDispatch := func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleUserRouting(w, r) }

	cases := []struct {
		name      string
		dispatch  dispatchFunc
		method    string
		path      string
		wantAllow string // 非空：期望 405 + 该 Allow；空：期望 404
	}{
		// tasks：{id} => GET；cancel/approve/reject/retry => POST；result => GET；dead-letter => DELETE
		{"tasks/bare-wrong-method", taskDispatch, http.MethodPut, "/api/v1/tasks/tk-1", "GET"},
		{"tasks/cancel-wrong-method", taskDispatch, http.MethodGet, "/api/v1/tasks/tk-1/cancel", "POST"},
		{"tasks/result-wrong-method", taskDispatch, http.MethodPost, "/api/v1/tasks/tk-1/result", "GET"},
		{"tasks/approve-wrong-method", taskDispatch, http.MethodDelete, "/api/v1/tasks/tk-1/approve", "POST"},
		{"tasks/reject-wrong-method", taskDispatch, http.MethodGet, "/api/v1/tasks/tk-1/reject", "POST"},
		{"tasks/retry-wrong-method", taskDispatch, http.MethodGet, "/api/v1/tasks/tk-1/retry", "POST"},
		{"tasks/dead-letter-wrong-method", taskDispatch, http.MethodPost, "/api/v1/tasks/tk-1/dead-letter", "DELETE"},
		{"tasks/unknown-subpath", taskDispatch, http.MethodPost, "/api/v1/tasks/tk-1/nope", ""},

		// devices：{id} => GET, DELETE；provision => POST；metrics/system => GET
		{"devices/bare-wrong-method", deviceDispatch, http.MethodPost, "/api/v1/devices/dev-1", "GET, DELETE"},
		{"devices/provision-wrong-method", deviceDispatch, http.MethodGet, "/api/v1/devices/dev-1/provision", "POST"},
		{"devices/metrics-wrong-method", deviceDispatch, http.MethodPost, "/api/v1/devices/dev-1/metrics", "GET"},
		{"devices/system-wrong-method", deviceDispatch, http.MethodDelete, "/api/v1/devices/dev-1/system", "GET"},
		{"devices/unknown-subpath", deviceDispatch, http.MethodGet, "/api/v1/devices/dev-1/nope", ""},

		// alerts：ack/silence => POST；裸 {id} 无详情端点，属未知路径
		{"alerts/ack-wrong-method", alertDispatch, http.MethodGet, "/api/v1/alerts/al-1/ack", "POST"},
		{"alerts/silence-wrong-method", alertDispatch, http.MethodPut, "/api/v1/alerts/al-1/silence", "POST"},
		{"alerts/bare-id-not-found", alertDispatch, http.MethodGet, "/api/v1/alerts/al-1", ""},
		{"alerts/unknown-subpath", alertDispatch, http.MethodPost, "/api/v1/alerts/al-1/nope", ""},

		// users：{id} => GET, PUT, DELETE；approve/reject => POST
		{"users/bare-wrong-method", userDispatch, http.MethodPost, "/api/v1/users/user-x", "GET, PUT, DELETE"},
		{"users/approve-wrong-method", userDispatch, http.MethodGet, "/api/v1/users/user-x/approve", "POST"},
		{"users/reject-wrong-method", userDispatch, http.MethodDelete, "/api/v1/users/user-x/reject", "POST"},
		{"users/unknown-subpath", userDispatch, http.MethodPost, "/api/v1/users/user-x/nope", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newExtraTestServer()
			req := httptest.NewRequest(c.method, c.path, nil)
			req.Header.Set("X-Tenant-ID", "t1")
			rec := httptest.NewRecorder()
			c.dispatch(s, rec, req)

			if c.wantAllow == "" {
				if rec.Code != http.StatusNotFound {
					t.Fatalf("%s %s = %d, want 404（未知子路径）; body=%s", c.method, c.path, rec.Code, rec.Body.String())
				}
				if got := rec.Header().Get("Allow"); got != "" {
					t.Fatalf("404 响应不应带 Allow 头，got %q", got)
				}
				return
			}
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s = %d, want 405（已知子资源 + 错误动词）; body=%s", c.method, c.path, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Allow"); got != c.wantAllow {
				t.Fatalf("%s %s Allow = %q, want %q", c.method, c.path, got, c.wantAllow)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("405 响应体非 JSON: %v; body=%s", err, rec.Body.String())
			}
			if body["error"] == "" {
				t.Fatalf("405 响应缺 error 字段: %s", rec.Body.String())
			}
		})
	}
}

// TestGetUserDetail_ShapeAndTenantScope GET /api/v1/users/{id}：单用户 JSON 字段形态
// 与列表用户项一致；跨租户或不存在一律 404。
func TestGetUserDetail_ShapeAndTenantScope(t *testing.T) {
	s := newExtraTestServer()
	if got := s.store.CreateUser(&store.User{
		ID: "user-t1", Username: "alice", Email: "alice@t1.io", TenantID: "t1",
		Status: "active", RoleIDs: []string{"role-viewer"},
	}); got == nil {
		t.Fatal("seed user-t1 失败")
	}
	if got := s.store.CreateUser(&store.User{
		ID: "user-t2", Username: "bob", Email: "bob@t2.io", TenantID: "t2", Status: "active",
	}); got == nil {
		t.Fatal("seed user-t2 失败")
	}

	do := func(id, tenant string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+id, nil)
		req.Header.Set("X-Tenant-ID", tenant)
		rec := httptest.NewRecorder()
		s.handleUserRouting(rec, req)
		return rec
	}

	// 同租户：200 + 与列表用户项同字段形态（passwordHash 恒不出现）。
	rec := do("user-t1", "t1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /users/user-t1 = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		ID       string   `json:"id"`
		Username string   `json:"username"`
		Email    string   `json:"email"`
		TenantID string   `json:"tenantId"`
		Status   string   `json:"status"`
		RoleIDs  []string `json:"roleIDs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode user: %v; body=%s", err, rec.Body.String())
	}
	if got.ID != "user-t1" || got.Username != "alice" || got.TenantID != "t1" || got.Status != "active" {
		t.Fatalf("user JSON = %+v, want id=user-t1 username=alice tenantId=t1 status=active", got)
	}
	if got.Email != "alice@t1.io" || len(got.RoleIDs) != 1 || got.RoleIDs[0] != "role-viewer" {
		t.Fatalf("user JSON 字段不完整: %+v", got)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if _, leaked := raw["passwordHash"]; leaked {
		t.Fatal("GET /users/{id} 泄漏 passwordHash 字段")
	}

	// 跨租户 / 不存在：404（不泄露他租户账号是否存在）。
	if rec := do("user-t2", "t1"); rec.Code != http.StatusNotFound {
		t.Fatalf("跨租户 GET = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if rec := do("user-nope", "t1"); rec.Code != http.StatusNotFound {
		t.Fatalf("不存在用户 GET = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}
