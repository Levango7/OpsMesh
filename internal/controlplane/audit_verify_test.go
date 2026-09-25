// audit_verify_test.go 测试 P1-3 审计链校验端点（GET /api/v1/audit/verify）。
//
// 覆盖状态码契约：
//   - 200：校验完成且无异常；
//   - 409：检出不一致（ok=false）；
//   - 501：后端不提供链式校验（内存态/未迁移）；
//   - 500：校验本身失败（DB 故障）；
//   - 401：无凭据；limit 越界时按上限收敛。
package controlplane

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/store"
)

// chainStubStore 包装真实 Store，仅覆写 VerifyAuditChain，用于端点分支测试
// （其余方法由内嵌的 Store 实现提供，避免为测试实现整个接口）。
type chainStubStore struct {
	store.Store
	res    *store.AuditChainVerifyResult
	err    error
	gotTen string
	gotLim int
}

func (c *chainStubStore) VerifyAuditChain(tenant string, limit int) (*store.AuditChainVerifyResult, error) {
	c.gotTen, c.gotLim = tenant, limit
	return c.res, c.err
}

// newAuditVerifyTestServer 构造带 stub 的测试 Server。
func newAuditVerifyTestServer(st store.Store) *Server {
	s := newAuditQueryTestServer()
	s.store = st
	return s
}

func TestHandleAuditVerify_MemoryUnsupported(t *testing.T) {
	st := store.NewMemoryStore()
	s := newAuditVerifyTestServer(st)
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditVerify(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("内存后端应返回 501；got %d body=%s", w.Code, w.Body.String())
	}
	var res store.AuditChainVerifyResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Supported {
		t.Fatal("内存后端必须如实回报 supported=false（不得伪称校验通过）")
	}
}

func TestHandleAuditVerify_OK(t *testing.T) {
	stub := &chainStubStore{
		Store: store.NewMemoryStore(),
		res:   &store.AuditChainVerifyResult{Supported: true, OK: true, Scope: "tenant", Checked: 3, FromID: 1, ToID: 3, TailCovered: true, HeadConsistent: true},
	}
	s := newAuditVerifyTestServer(stub)
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify?limit=50", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditVerify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if stub.gotLim != 50 {
		t.Fatalf("limit 应透传 50；got %d", stub.gotLim)
	}
	var res store.AuditChainVerifyResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK || res.Checked != 3 || !res.HeadConsistent {
		t.Fatalf("响应体错误: %+v", res)
	}
}

// TestHandleAuditVerify_Tampered 验证检出不一致时用 409（便于 curl -f / 网关直接告警），
// 且响应体仍带完整定位信息。
func TestHandleAuditVerify_Tampered(t *testing.T) {
	stub := &chainStubStore{
		Store: store.NewMemoryStore(),
		res: &store.AuditChainVerifyResult{
			Supported: true, OK: false, Checked: 10, FromID: 1, ToID: 10,
			FirstBadID: 7, Reason: "id=7 的 entry_hash 与内容重算结果不一致（行内容被改写）",
		},
	}
	s := newAuditVerifyTestServer(stub)
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditVerify(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("检出不一致应返回 409；got %d body=%s", w.Code, w.Body.String())
	}
	var res store.AuditChainVerifyResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.OK || res.FirstBadID != 7 || res.Reason == "" {
		t.Fatalf("响应体应带首个坏行与原因: %+v", res)
	}
}

func TestHandleAuditVerify_StoreError(t *testing.T) {
	stub := &chainStubStore{Store: store.NewMemoryStore(), err: errors.New("db down")}
	s := newAuditVerifyTestServer(stub)
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditVerify(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("校验失败应返回 500；got %d body=%s", w.Code, w.Body.String())
	}
}

// TestHandleAuditVerify_LimitClamp 验证 limit 越界收敛到上限（防把校验端点变成全表扫描放大器）。
func TestHandleAuditVerify_LimitClamp(t *testing.T) {
	stub := &chainStubStore{
		Store: store.NewMemoryStore(),
		res:   &store.AuditChainVerifyResult{Supported: true, OK: true},
	}
	s := newAuditVerifyTestServer(stub)
	auth := loginAsAdmin(t, s)

	for _, c := range []struct {
		query string
		want  int
	}{
		{"", verifyDefaultLimit},
		{"?limit=10", 10},
		{"?limit=999999", verifyMaxLimit},
		{"?limit=abc", verifyDefaultLimit},
		{"?limit=-5", verifyDefaultLimit},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify"+c.query, nil)
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.handleAuditVerify(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("query=%q status=%d body=%s", c.query, w.Code, w.Body.String())
		}
		if stub.gotLim != c.want {
			t.Fatalf("query=%q limit=%d want=%d", c.query, stub.gotLim, c.want)
		}
	}
}

func TestHandleAuditVerify_NoToken(t *testing.T) {
	s := newAuditQueryTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify", nil)
	w := httptest.NewRecorder()
	s.handleAuditVerify(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

func TestHandleAuditVerify_MethodNotAllowed(t *testing.T) {
	stub := &chainStubStore{
		Store: store.NewMemoryStore(),
		res:   &store.AuditChainVerifyResult{Supported: true, OK: true},
	}
	s := newAuditVerifyTestServer(stub)
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/verify", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleAuditVerify(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}
