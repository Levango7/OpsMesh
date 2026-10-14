package controlplane

// audit_tenant_scope_test.go — §3.8 守卫（2026-10-14 演练待办）：两个审计端点的
// 租户口径统一。
//
// 症状（演练人设实测）：`GET /api/v1/audits`（不带 tenant 参数）返回**全租户**
// 96 条，且 `?tenant=<他租户>` 可定向读；而 `GET /api/v1/audit/events` 严格收敛
// 调用方租户。本仓默认部署 requireAuth=false——"隔离交给前置网关"的前提在该形态
// 下不成立（控制面自身仍执行 RBAC），两个端点的差异于是成为真实的越权读面。
//
// 裁定（统一口径）：非平台租户调用者一律只看本租户（actx.TenantID，客户端传啥都
// 不看）；平台租户（default）保留跨租户查询能力（运维排障，与 handleListUsers
// 同策）。

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

func timeNowPlusHour() time.Time { return time.Now().Add(time.Hour) }

// newAuditScopeServer 造一个 vanilla Server（requireAuth=false，即本仓默认形态），
// 预置两个租户各一条审计事件。cfg.Demo=true 仅为认证放宽（未带 X-Tenant-ID 时兜底
// 默认租户），与本判据无关（判据全部显式带租户口径）。
func newAuditScopeServer(t *testing.T) (*Server, *store.MemoryStore) {
	t.Helper()
	st := store.NewMemoryStore()
	st.Audit(&proto.AuditEvent{TenantID: "t1", Action: "scope-probe", Target: "t1-event"})
	st.Audit(&proto.AuditEvent{TenantID: "t2", Action: "scope-probe", Target: "t2-event"})
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth: false,
	}, st
}

func getAuditsRec(t *testing.T, s *Server, path, tenantHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if tenantHeader != "" {
		req.Header.Set("X-Tenant-ID", tenantHeader)
	}
	rec := httptest.NewRecorder()
	s.handleAudits(rec, req)
	return rec
}

// TestAudits_NonPlatformCallerCannotCrossTenant §3.8 判据 1：非平台租户（t1）在
// requireAuth=false 下——带 ?tenant=t2 也被强制回 t1，不带参也不再看到全租户。
func TestAudits_NonPlatformCallerCannotCrossTenant(t *testing.T) {
	s, _ := newAuditScopeServer(t)

	for _, path := range []string{"/api/v1/audits?action=scope-probe", "/api/v1/audits?tenant=t2&action=scope-probe"} {
		rec := getAuditsRec(t, s, path, "t1")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status=%d want 200; body=%s", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "t1-event") {
			t.Errorf("%s: 应含本租户事件 t1-event，实得 %s", path, body)
		}
		if strings.Contains(body, "t2-event") {
			t.Errorf("%s: 不得含他租户事件 t2-event（§3.8 越权读原状），实得 %s", path, body)
		}
	}
}

// TestAudits_PlatformCallerKeepsCrossTenantQuery 平台租户（default）保留跨租户
// 查询能力（运维排障需要，与 handleListUsers 同策）。
func TestAudits_PlatformCallerKeepsCrossTenantQuery(t *testing.T) {
	s, _ := newAuditScopeServer(t)

	// 平台 + 指定 t2 ⇒ 看得到 t2（能力保留）。
	rec := getAuditsRec(t, s, "/api/v1/audits?tenant=t2&action=scope-probe", "default")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "t2-event") {
		t.Errorf("平台租户应可跨租户查询 t2，实得 %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "t1-event") {
		t.Errorf("指定 tenant=t2 时不该混入 t1，实得 %s", rec.Body.String())
	}

	// 平台 + 不指定 ⇒ 全租户视图（与修复前 default 调用方行为一致）。
	rec = getAuditsRec(t, s, "/api/v1/audits?action=scope-probe", "default")
	for _, want := range []string{"t1-event", "t2-event"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("平台租户无参应保留全租户视图（缺 %s）：%s", want, rec.Body.String())
		}
	}
}

// TestAuditEvents_PlatformTenantQueryMatchesAudits 口径对称：/api/v1/audit/events
// 也按同一规则——非平台强制本租户、平台可用 tenant=（与 /api/v1/audits 逐字同策）。
// 该端点走 requirePermission（要 Authorization），夹具与 audit_query_test.go 同款。
func TestAuditEvents_PlatformTenantQueryMatchesAudits(t *testing.T) {
	st := store.NewMemoryStore()
	st.Audit(&proto.AuditEvent{TenantID: "t1", Action: "scope-probe", Target: "t1-event"})
	st.Audit(&proto.AuditEvent{TenantID: "t2", Action: "scope-probe", Target: "t2-event"})
	s := newAuditQueryTestServer()
	s.store = st
	auth := loginAsAdmin(t, s) // admin 属 default（平台）租户

	// 平台 + tenant=t2 ⇒ t2 的事件（新增的对称能力）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=t2&action=scope-probe", nil)
	req.Header.Set("Authorization", auth)
	rec := httptest.NewRecorder()
	s.handleAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "t2-event") || strings.Contains(rec.Body.String(), "t1-event") {
		t.Errorf("平台 tenant=t2 应只见 t2：%s", rec.Body.String())
	}

	// 非平台（给一个 t1 租户的令牌）+ 试 tenant=t2 ⇒ 仍只有 t1（自称参数被忽略）。
	// 与平台用例的差别只在令牌租户——证明分支条件是租户、不是端点。
	if st.GetUser("u-t1") == nil {
		st.CreateUser(&store.User{ID: "u-t1", Username: "u-t1", TenantID: "t1", RoleIDs: []string{"role-admin"}, Status: "active"})
	}
	t1tok, err := authctx.SignJWT(authctx.JWTClaims{
		UserID: "u-t1", Username: "u-t1", TenantID: "t1",
		Roles: []string{"role-admin"}, ExpiresAt: timeNowPlusHour(),
	}, s.jwtSecret)
	if err != nil {
		t.Fatalf("签 t1 令牌: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=t2&action=scope-probe", nil)
	req.Header.Set("Authorization", "Bearer "+t1tok)
	rec = httptest.NewRecorder()
	s.handleAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "t1-event") || strings.Contains(rec.Body.String(), "t2-event") {
		t.Errorf("非平台 t1 指定他人租户也应只见 t1：%s", rec.Body.String())
	}
}
