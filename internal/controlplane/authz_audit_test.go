package controlplane

// authz_audit_test.go — A3 回归守卫（2026-10-12 模拟用户演练）。
//
// 症状（两个"模拟用户"独立复现）：被拒的越权尝试**零留痕**——6 项越权写操作 + 跨租户读之后，
// 审计链里仍只有 user_login / user_create / user_change_password，count 前后不动。
// 对值班复盘与等保，越权尝试恰恰是最该留痕的一类。
//
// 三条判据（缺一条会放过不同坏法）：
//  1. **拒绝必留痕**：403 之后必须出现 authz_denied，且 actor/tenant/所需权限都在场；
//  2. **放行不留痕**：有权限的同类请求不得产生 authz_denied（否则审计被噪音淹没，等于没记）；
//  3. **query 不入台账**：路径里的查询串不得进 Target/Detail（台账不留参数值）。

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

// newAuthzAuditServer 造一个"非 demo + 信任网关注入头"的 Server：这条组合让
// authorizeByRoles 成为权限判定路径（无需 token），且 demo 的宽松放行不生效。
func newAuthzAuditServer() (*Server, *store.MemoryStore) {
	st := store.NewMemoryStore()
	// 种一个 agent：否则"有权限"的那条会在权限关之后被业务校验拦成
	// "agent not found or tenant mismatch"（那是业务 403，不是授权拒绝，会把判据弄浑）。
	st.Register(&proto.AgentInfo{AgentID: "dev-x", TenantID: "t1", Status: "online", Segment: "seg-a"})
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}, st
}

func authzDeniedEvents(st *store.MemoryStore) []*proto.AuditEvent {
	var out []*proto.AuditEvent
	for _, e := range st.Audits() {
		if e != nil && e.Action == "authz_denied" {
			out = append(out, e)
		}
	}
	return out
}

func postTaskWithRoles(s *Server, url, role string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, url,
		strings.NewReader(`{"agentID":"dev-x","type":"shell","command":"echo hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u-7")
	req.Header.Set("X-User-Roles", role)
	rec := httptest.NewRecorder()
	s.handleCreateTask(rec, req)
	return rec
}

// TestAuthzDeniedLeavesAuditTrail 判据 1+3：越权拒绝必须留痕，且台账里不含 query。
func TestAuthzDeniedLeavesAuditTrail(t *testing.T) {
	s, st := newAuthzAuditServer()
	before := len(authzDeniedEvents(st))

	rec := postTaskWithRoles(s, "/api/v1/tasks?probe=secret", "viewer") // viewer 无 task:write
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer 下发任务应 403，实得 %d：%s", rec.Code, rec.Body.String())
	}
	evs := authzDeniedEvents(st)
	if len(evs) != before+1 {
		t.Fatalf("拒绝后应恰好新增 1 条 authz_denied（%d ⇒ %d）——零留痕正是 A3 的形状", before, len(evs))
	}
	last := evs[len(evs)-1]
	if last.UserID != "u-7" || last.TenantID != "t1" {
		t.Errorf("留痕要能回答「谁在哪个租户」：userID=%q tenantID=%q", last.UserID, last.TenantID)
	}
	if !strings.Contains(last.Detail, "task:write") {
		t.Errorf("留痕要能回答「想干什么」（缺所需权限名）：detail=%q", last.Detail)
	}
	if strings.Contains(last.Target, "probe=secret") || strings.Contains(last.Detail, "probe=secret") {
		t.Errorf("query 不得进台账：target=%q detail=%q", last.Target, last.Detail)
	}
}

// TestAuthzAllowedLeavesNoDenialTrail 判据 2：有权限的请求不得产生 authz_denied。
func TestAuthzAllowedLeavesNoDenialTrail(t *testing.T) {
	s, st := newAuthzAuditServer()
	before := len(authzDeniedEvents(st))

	rec := postTaskWithRoles(s, "/api/v1/tasks", "admin") // admin 有 task:write
	if rec.Code == http.StatusForbidden {
		t.Fatalf("admin 不应被拒：%s", rec.Body.String())
	}
	if after := len(authzDeniedEvents(st)); after != before {
		t.Errorf("放行的请求不得产生 authz_denied（%d ⇒ %d）——否则审计被噪音淹没", before, after)
	}
}

// TestAuthzTenantMismatchLeavesTrail 租户不符（另一类 403）同样要留痕：跨租户尝试是审计最关心的。
func TestAuthzTenantMismatchLeavesTrail(t *testing.T) {
	st := store.NewMemoryStore()
	secret := []byte("test-jwt-secret-for-authz-audit-32b!")
	s := &Server{
		store:       st,
		jwtSecret:   secret,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: false},
		requireAuth: false,
	}
	tok, err := authctx.SignJWT(authctx.JWTClaims{
		UserID: "u-9", Username: "nine", Roles: []string{"role-admin"},
		TenantID: "t9", ExpiresAt: time.Now().Add(time.Hour),
	}, secret)
	if err != nil {
		t.Fatalf("签 token: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Tenant-ID", "t-other") // 与令牌的 t9 不符
	rec := httptest.NewRecorder()
	if _, ok := s.requireTenantContext(rec, req); ok {
		t.Fatalf("租户不符应被拒，实得 %d", rec.Code)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("应为 403，实得 %d", rec.Code)
	}
	found := false
	for _, e := range authzDeniedEvents(st) {
		if strings.Contains(e.Detail, "tenant mismatch") {
			found = true
		}
	}
	if !found {
		t.Error("跨租户尝试必须留痕（A3：正是最该记的一类）")
	}
}
