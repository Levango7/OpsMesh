package controlplane

// auth_me_contract_test.go — F6 守卫（控制面侧）：本地 /auth/me 的响应键集是
// **规范源**（store.User 的 JSON tag），auth-svc 镜像它（2026-10-13 裁定）。
//
// 为什么两侧各钉一根：auth-svc 的键集由 services/auth-svc/internal/http/
// gateway_me_contract_test.go 钉（另一个 Go module，常量不共享，跨模块零编译期
// 保证）；本文件钉规范源自身。任一侧增删键，两侧用例之一必红——"切流前后响应
// 形状不同"由此成为可检事件，而不是线上才发现的差异。
//
// 规范键（store.User tag，见 internal/store/model/model.go）：id / username /
// email / tenantId / status / roleIDs / createdAt / mustChangePassword /
// permissions。auth-svc 侧另回 mode（自验标识），属**补充键**，不在本表内。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/Levango7/OpsMesh/internal/store"
)

// TestAuthMe_FieldSetIsCanonicalSource 本地 /auth/me 键集恰为规范集。
// 集外键判红（防把调试/内部字段顺手塞进响应）；缺键同样判红（瘦身会静默改掉
// auth-svc 必须镜像的形状）。
func TestAuthMe_FieldSetIsCanonicalSource(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	req := doWithAuth(http.MethodGet, "/api/v1/auth/me", auth, nil)
	rec := httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应不是 JSON 对象: %v", err)
	}
	want := map[string]bool{
		"id": true, "username": true, "email": true, "tenantId": true,
		"status": true, "roleIDs": true, "createdAt": true,
		"mustChangePassword": true, "permissions": true,
	}
	for k := range m {
		if !want[k] {
			keys := make([]string, 0, len(m))
			for kk := range m {
				keys = append(keys, kk)
			}
			sort.Strings(keys)
			t.Errorf("本地 /auth/me 含规范集外字段 %q（auth-svc 镜像是全量复制形状，集外键会立刻变成两面孔差）；全集=%v", k, keys)
		}
	}
	for k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("本地 /auth/me 缺规范字段 %q（auth-svc 侧不会补它，客户端即丢字段）", k)
		}
	}

	// 形状对账：能整体 decode 成 store.User（键名与 tag 的一致性由编译器/编码器
	// 保证，这里再兜一层断言防"键对但类型漂移"）。
	var u store.User
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		t.Fatalf("响应应可 decode 成 store.User: %v", err)
	}
	if u.Username != "admin" {
		t.Fatalf("username = %q, want admin", u.Username)
	}
}
