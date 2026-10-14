package controlplane

// authz_audit_forgery_test.go — §3.5 守卫（2026-10-14 演练移交/待办两项）：
//   - **actor 不采信自称头**：JWT 模式下 X-User-Id 是客户端可自设的，演练人设实测
//     两条伪造署名入库（`user mismatch` 那类 403 的 actor 竟是被拒的头值）⇒ 按
//     actor 追责会被带偏；
//   - **UserID 语义统一为存储 ID**：此前 authz_denied 记用户名（drill-u04）、
//     user_login 记存储 ID（user-admin），同字段两种含义 ⇒ 单一键对账必漏；
//   - **身份不适用记 unknown**：不编造、也不退回自称值。
//
// 注入模式（--trust-gateway-headers）下头部即身份的行为不变，由既有
// TestAuthzDeniedLeavesAuditTrail 覆盖（本文件不动它）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// TestAuthzDenied_NeverTrustsSelfClaimedActor JWT 模式：自称 X-User-Id 不得成为
// actor；actor=JWT 存储 ID，用户名另置 actor_name=，自称值降级为 claimed_user= 证据。
func TestAuthzDenied_NeverTrustsSelfClaimedActor(t *testing.T) {
	st := store.NewMemoryStore()
	st.Register(&proto.AgentInfo{AgentID: "dev-x", TenantID: "t1", Status: "online", Segment: "seg-a"})
	secret := []byte("test-jwt-secret-for-authz-forgery-32b!")
	s := &Server{
		store:       st,
		jwtSecret:   secret,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: false},
		requireAuth: false,
	}
	tok, err := authctx.SignJWT(authctx.JWTClaims{
		UserID: "user-real", Username: "realname", Roles: []string{"role-viewer"},
		TenantID: "t1", ExpiresAt: time.Now().Add(time.Hour),
	}, secret)
	if err != nil {
		t.Fatalf("签 token: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks",
		strings.NewReader(`{"agentID":"dev-x","type":"shell","command":"echo hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "spoofed-victim") // 自称值：演练里正是它入库
	req.Header.Set("X-User-Roles", "viewer")      // viewer 无 task:write ⇒ 403
	rec := httptest.NewRecorder()
	s.handleCreateTask(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer 下发任务应 403，实得 %d：%s", rec.Code, rec.Body.String())
	}

	evs := authzDeniedEvents(st)
	if len(evs) == 0 {
		t.Fatal("拒绝未留痕（A3 回归）")
	}
	last := evs[len(evs)-1]
	if last.UserID == "spoofed-victim" {
		t.Fatalf("自称头被采信为 actor——伪造署名入库（§3.5 原状）；userID=%q", last.UserID)
	}
	if last.UserID != "user-real" {
		t.Errorf("actor 应为 JWT 存储 ID user-real，实得 %q", last.UserID)
	}
	if !strings.Contains(last.Detail, "actor_name=realname") {
		t.Errorf("用户名应另置 actor_name=（UserID 统一存储 ID后的可读性来源）：detail=%q", last.Detail)
	}
	if !strings.Contains(last.Detail, "claimed_user=spoofed-victim") {
		t.Errorf("自称值应降级为证据 claimed_user=：detail=%q", last.Detail)
	}
}

// TestAuditAuthzDenied_NoIdentityRecordsUnknown 无凭证且非注入模式：actor=unknown，
// 自称值只作证据——"身份不适用就不编"。
func TestAuditAuthzDenied_NoIdentityRecordsUnknown(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{
		store:       st,
		jwtSecret:   []byte("test-jwt-secret-for-authz-unknown-32bytes!"),
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: false},
		requireAuth: false,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks",
		strings.NewReader(`{"agentID":"dev-x"}`))
	req.Header.Set("X-User-Id", "ghost")
	s.auditAuthzDenied(req, "task:write", "permission denied")

	evs := authzDeniedEvents(st)
	if len(evs) != 1 {
		t.Fatalf("应恰好 1 条 authz_denied，实得 %d", len(evs))
	}
	if evs[0].UserID != "unknown" {
		t.Errorf("无身份可用时应记 unknown，实得 %q（采信自称头即 §3.5 原状）", evs[0].UserID)
	}
	if !strings.Contains(evs[0].Detail, "claimed_user=ghost") {
		t.Errorf("自称值应作为证据进 detail：%q", evs[0].Detail)
	}
	if evs[0].TenantID != "default" {
		t.Errorf("租户全空应归 default，实得 %q", evs[0].TenantID)
	}
}
