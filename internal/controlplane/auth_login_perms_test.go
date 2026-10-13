package controlplane

// auth_login_perms_test.go — B4 回归守卫（2026-10-12 模拟用户演练）。
//
// 症状：登录响应里的 user.permissions 是 null（只有 /auth/me 填）⇒ 用户从登录那一刻起不知道
// 自己能做什么；演练里新人据此误判"viewer 什么都不能读"。store.User.EffectivePermissions 的
// 字段注释明写其用途是"供前端侧栏按权限过滤功能入口，与后端 RBAC 闸同源"——登录响应本该也填。
//
// 两条判据：
//  1. 登录成功响应必须带**非空**权限（种一个 viewer 用户 ⇒ 至少有 device:read 之类的只读权限）；
//  2. 填权限必须走**浅拷贝**：不得原地改 store 里的用户对象（共享指针，否则并发下污染库内数据）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/controlplane/credentials"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newLoginPermsServer 复用包内既有的 auth 夹具（它带了登录路径必需的 sessionStore/loginGuard；
// 裸 &Server{} 会在登录里 nil 解引用而 panic——本测试第一版即此坑）。
func newLoginPermsServer(t *testing.T) (*Server, *store.MemoryStore) {
	t.Helper()
	s := newAuthTestServer(t)
	st, _ := s.store.(*store.MemoryStore)
	if st == nil {
		t.Fatal("夹具 store 类型不符")
	}
	return s, st
}

func TestLoginResponseCarriesPermissions(t *testing.T) {
	s, st := newLoginPermsServer(t)
	// 种一个带可见角色的用户（复用种子角色 id，权限由 RBAC 目录展开）。
	if st.CreateUser(&store.User{
		ID: "u-viewer", Username: "looker", Email: "looker@x.io", TenantID: "default",
		Status: "active", RoleIDs: []string{"role-viewer"},
		PasswordHash: hashOrFatal(t, "Looker-Pass-2026x!"),
	}) == nil {
		t.Fatal("种用户失败")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"looker","password":"Looker-Pass-2026x!"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录应 200，实得 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"permissions":[`) {
		t.Errorf("登录响应必须带权限（否则用户不知道能做什么——B4 原形状）：%s", truncate(rec.Body.String(), 200))
	}

	// 判据 2：store 里的对象不得被原地改
	stored := st.GetUserByUsername("looker")
	if stored != nil && len(stored.EffectivePermissions) != 0 {
		t.Errorf("填权限必须走浅拷贝，不得原地改 store 对象（实得 %d 条写回库内）", len(stored.EffectivePermissions))
	}
}

// hashOrFatal 用与生产同一套哈希（credentials.HashPassword）造测试口令。
func hashOrFatal(t *testing.T, pw string) string {
	t.Helper()
	h, err := credentials.HashPassword(pw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return h
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
